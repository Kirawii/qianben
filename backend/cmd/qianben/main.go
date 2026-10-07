package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"github.com/Kirawii/qianben/backend/internal/domain"
	"github.com/Kirawii/qianben/backend/internal/httpapi"
	"github.com/Kirawii/qianben/backend/internal/store"
	"net/http"
	"os"
	"os/signal"
	"time"
)

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, "钱本启动失败：", e)
		os.Exit(1)
	}
}
func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		return fmt.Errorf("请设置 DATABASE_URL")
	}
	db, e := store.Open(ctx, url)
	if e != nil {
		return e
	}
	defer db.Pool.Close()
	mode := "serve"
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}
	if mode == "serve" || mode == "worker" {
		var role string
		var super bool
		e = db.Pool.QueryRow(ctx, `SELECT current_user,usesuper FROM pg_user WHERE usename=current_user`).Scan(&role, &super)
		if e != nil {
			return e
		}
		if super || role != "qianben_app" {
			return fmt.Errorf("API/worker 必须使用 qianben_app 数据库角色")
		}
	}
	switch mode {
	case "worker":
		for ctx.Err() == nil {
			worked, err := db.Work(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return nil
				}
				fmt.Fprintln(os.Stderr, "worker 操作失败，将重试（不记录证据正文）")
				select {
				case <-ctx.Done():
				case <-time.After(5 * time.Second):
				}
				continue
			}
			if !worked {
				select {
				case <-ctx.Done():
				case <-time.After(time.Second):
				}
			}
		}
		return nil
	case "migrate":
		return db.Migrate(ctx, "migrations")
	case "revoke":
		token := os.Getenv("QIANBEN_TOKEN")
		if len(token) < 32 {
			return fmt.Errorf("请设置待撤销令牌 QIANBEN_TOKEN")
		}
		tag, e := db.Pool.Exec(ctx, `UPDATE qb.tokens SET revoked=true WHERE token_hash=$1`, store.Hash([]byte(token)))
		if e == nil {
			fmt.Printf("已撤销 %d 个令牌\n", tag.RowsAffected())
		}
		return e
	case "user", "token":
		b := make([]byte, 32)
		if _, e = rand.Read(b); e != nil {
			return e
		}
		token := hex.EncodeToString(b)
		id := domain.ID()
		if mode == "token" {
			if len(os.Args) != 3 || !domain.IsUUID(os.Args[2]) {
				return fmt.Errorf("用法：qianben token USER_ID")
			}
			id = os.Args[2]
		}
		tx, e := db.Pool.Begin(ctx)
		if e != nil {
			return e
		}
		defer tx.Rollback(ctx)
		if mode == "user" {
			_, e = tx.Exec(ctx, `INSERT INTO qb.users(id,name) VALUES($1,'本地用户')`, id)
			if e != nil {
				return e
			}
		}
		_, e = tx.Exec(ctx, `INSERT INTO qb.tokens(user_id,token_hash) VALUES($1,$2)`, id, store.Hash([]byte(token)))
		if e != nil {
			return e
		}
		if e = tx.Commit(ctx); e != nil {
			return e
		}
		fmt.Println("访问令牌（仅显示一次，请妥善保存）：", token)
		fmt.Println("用户 ID：", id)
		return nil
	case "serve":
		addr := os.Getenv("LISTEN_ADDR")
		if addr == "" {
			addr = "127.0.0.1:8080"
		}
		server := &http.Server{Addr: addr, Handler: httpapi.Server{DB: db}.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
		go func() {
			<-ctx.Done()
			shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			server.Shutdown(shutdown)
		}()
		fmt.Println("钱本 API：", addr)
		e = server.ListenAndServe()
		if e == http.ErrServerClosed {
			return nil
		}
		return e
	default:
		return fmt.Errorf("用法：qianben [serve|worker|migrate|user|token USER_ID|revoke]")
	}
}
