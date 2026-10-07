package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Kirawii/qianben/backend/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"strings"
	"time"
)

type DB struct{ Pool *pgxpool.Pool }

func Open(ctx context.Context, url string) (*DB, error) {
	p, e := pgxpool.New(ctx, url)
	if e != nil {
		return nil, e
	}
	if e = p.Ping(ctx); e != nil {
		p.Close()
		return nil, e
	}
	return &DB{p}, nil
}
func Hash(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }
func (d *DB) Actor(ctx context.Context, token string) (string, error) {
	var id string
	if len(token) < 32 {
		return "", domain.Unauthorized()
	}
	e := d.Pool.QueryRow(ctx, `SELECT user_id::text FROM qb.tokens WHERE token_hash=$1 AND NOT revoked`, Hash([]byte(token))).Scan(&id)
	if errors.Is(e, pgx.ErrNoRows) {
		return "", domain.Unauthorized()
	}
	return id, e
}
func actorTx(ctx context.Context, tx pgx.Tx, actor string) error {
	_, e := tx.Exec(ctx, `SELECT set_config('qianben.actor',$1,true)`, actor)
	return e
}
func loadLedger(ctx context.Context, tx pgx.Tx, actor, id, lock string) (domain.Ledger, error) {
	var l domain.Ledger
	q := `SELECT id::text,name,currency,cutover_time,version,status FROM qb.ledgers WHERE id=$1 AND user_id=$2 ` + lock
	e := tx.QueryRow(ctx, q, id, actor).Scan(&l.ID, &l.Name, &l.Currency, &l.Cutover, &l.Version, &l.Status)
	if errors.Is(e, pgx.ErrNoRows) {
		return l, domain.Missing("账本不存在")
	}
	if e != nil {
		return l, e
	}
	if l.Status != "ACTIVE" {
		return l, domain.Conflict("账本已停止写入")
	}
	return l, nil
}
func (d *DB) Authorized(ctx context.Context, actor, id string) error {
	if !domain.IsUUID(id) {
		return domain.Invalid("无效账本 ID")
	}
	var ok bool
	e := d.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM qb.ledgers WHERE id=$1 AND user_id=$2 AND status='ACTIVE')`, id, actor).Scan(&ok)
	if e != nil {
		return e
	}
	if !ok {
		return domain.Missing("账本不存在")
	}
	return nil
}
func command(ctx context.Context, d *DB, actor, ledger, id string, payload any, kind string, fn func(pgx.Tx, domain.Ledger) (any, error)) (json.RawMessage, error) {
	if !domain.IsUUID(ledger) || len(id) < 8 || len(id) > 128 {
		return nil, domain.Invalid("账本或 command_id 无效")
	}
	tx, e := d.Pool.Begin(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	if e = actorTx(ctx, tx, actor); e != nil {
		return nil, e
	}
	l, e := loadLedger(ctx, tx, actor, ledger, "FOR UPDATE")
	if e != nil {
		return nil, e
	}
	hash := Hash(domain.Hashable(payload))
	var oldHash string
	var result []byte
	e = tx.QueryRow(ctx, `SELECT payload_hash,result FROM qb.command_receipts WHERE ledger_id=$1 AND command_id=$2`, ledger, id).Scan(&oldHash, &result)
	if e == nil {
		if oldHash != hash {
			return nil, domain.Conflict("同一 command_id 的请求内容不同")
		}
		return result, nil
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return nil, e
	}
	out, e := fn(tx, l)
	if e != nil {
		return nil, e
	}
	result, e = json.Marshal(out)
	if e != nil {
		return nil, e
	}
	next := l.Version + 1
	_, e = tx.Exec(ctx, `UPDATE qb.ledgers SET version=$2 WHERE id=$1`, ledger, next)
	if e != nil {
		return nil, e
	}
	change, _ := json.Marshal(map[string]any{"result": json.RawMessage(result), "version": next})
	_, e = tx.Exec(ctx, `INSERT INTO qb.changes(ledger_id,version,command_id,kind,data) VALUES($1,$2,$3,$4,$5)`, ledger, next, id, kind, change)
	if e != nil {
		return nil, e
	}
	_, e = tx.Exec(ctx, `INSERT INTO qb.outbox(ledger_id,kind,payload) VALUES($1,'PROJECT',$2)`, ledger, fmt.Sprintf(`{"version":%d}`, next))
	if e != nil {
		return nil, e
	}
	_, e = tx.Exec(ctx, `INSERT INTO qb.command_receipts(ledger_id,command_id,payload_hash,result) VALUES($1,$2,$3,$4)`, ledger, id, hash, result)
	if e != nil {
		return nil, e
	}
	if e = tx.Commit(ctx); e != nil {
		return nil, e
	}
	return result, nil
}
func accountsTx(ctx context.Context, tx pgx.Tx, ledger string) (map[string]domain.Account, map[string]string, error) {
	rows, e := tx.Query(ctx, `SELECT id::text,code,name,type,cash,initialized,revision,provider,masked_ref FROM qb.accounts WHERE ledger_id=$1 ORDER BY code`, ledger)
	if e != nil {
		return nil, nil, e
	}
	defer rows.Close()
	all := map[string]domain.Account{}
	sys := map[string]string{}
	for rows.Next() {
		var a domain.Account
		if e = rows.Scan(&a.ID, &a.Code, &a.Name, &a.Type, &a.Cash, &a.Initialized, &a.Revision, &a.Provider, &a.MaskedRef); e != nil {
			return nil, nil, e
		}
		all[a.ID] = a
		sys[a.Code] = a.ID
	}
	return all, sys, rows.Err()
}
func textLimit(s string, n int) bool { return len(strings.TrimSpace(s)) > 0 && len(s) <= n }
func expires(t time.Time) bool       { return time.Since(t) > 30*24*time.Hour }
