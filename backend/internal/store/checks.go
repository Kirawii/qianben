package store

import (
	"context"
	"encoding/json"
	"github.com/Kirawii/qianben/backend/internal/domain"
	"github.com/jackc/pgx/v5"
	"strconv"
	"time"
)

type BalanceCheck struct {
	ID            string        `json:"id"`
	AccountID     string        `json:"account_id"`
	AsOf          time.Time     `json:"as_of"`
	Actual        domain.Amount `json:"actual_minor"`
	Book          domain.Amount `json:"book_minor"`
	Difference    domain.Amount `json:"difference_minor"`
	SourceVersion int64         `json:"source_version"`
}
type CheckRequest struct {
	CommandID string        `json:"command_id"`
	AccountID string        `json:"account_id"`
	AsOf      time.Time     `json:"as_of"`
	Actual    domain.Amount `json:"actual_minor"`
}

func (d *DB) CheckBalance(ctx context.Context, actor, ledger string, r CheckRequest) (json.RawMessage, error) {
	r.AsOf = r.AsOf.Truncate(time.Microsecond)
	if !domain.IsUUID(r.AccountID) || r.AsOf.IsZero() || r.AsOf.After(time.Now()) || r.Actual < -9000000000000000000 || r.Actual > 9000000000000000000 {
		return nil, domain.Invalid("账户、余额或检查时间无效")
	}
	return command(ctx, d, actor, ledger, r.CommandID, r, "BALANCE_CHECKED", func(tx pgx.Tx, l domain.Ledger) (any, error) {
		if r.AsOf.Before(l.Cutover) {
			return nil, domain.Invalid("检查时间须不早于账本启用时点")
		}
		var typ string
		var initialized, system bool
		e := tx.QueryRow(ctx, `SELECT type,initialized,system FROM qb.accounts WHERE ledger_id=$1 AND id=$2`, ledger, r.AccountID).Scan(&typ, &initialized, &system)
		if e == pgx.ErrNoRows {
			return nil, domain.Missing("账户不存在")
		}
		if e != nil {
			return nil, e
		}
		if system || !initialized || (typ != "ASSET" && typ != "LIABILITY") {
			return nil, domain.Invalid("请选择已设置期初的实际账户")
		}
		var value string
		e = tx.QueryRow(ctx, `SELECT COALESCE(sum(CASE WHEN $3='LIABILITY' THEN e.credit::numeric-e.debit ELSE e.debit::numeric-e.credit END),0)::text FROM qb.entries e JOIN qb.journals j ON j.id=e.journal_id WHERE e.ledger_id=$1 AND e.account_id=$2 AND j.effective_at<=$4`, ledger, r.AccountID, typ, r.AsOf).Scan(&value)
		if e != nil {
			return nil, e
		}
		n, e := strconv.ParseInt(value, 10, 64)
		if e != nil {
			return nil, e
		}
		c := BalanceCheck{ID: domain.ID(), AccountID: r.AccountID, AsOf: r.AsOf, Actual: r.Actual, Book: domain.Amount(n), SourceVersion: l.Version + 1}
		c.Difference, e = domain.Subtract(c.Actual, c.Book)
		if e != nil {
			return nil, domain.Invalid("余额差额超出支持范围")
		}
		_, e = tx.Exec(ctx, `INSERT INTO qb.balance_checks(id,ledger_id,account_id,as_of,actual_minor,book_minor,difference_minor,source_version) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, c.ID, ledger, c.AccountID, c.AsOf, int64(c.Actual), int64(c.Book), int64(c.Difference), c.SourceVersion)
		return c, e
	})
}

func (d *DB) BalanceChecks(ctx context.Context, actor, ledger string) ([]BalanceCheck, error) {
	if e := d.Authorized(ctx, actor, ledger); e != nil {
		return nil, e
	}
	rows, e := d.Pool.Query(ctx, `SELECT id::text,account_id::text,as_of,actual_minor,book_minor,difference_minor,source_version FROM qb.balance_checks WHERE ledger_id=$1 ORDER BY created_at DESC,id DESC LIMIT 100`, ledger)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []BalanceCheck{}
	for rows.Next() {
		var c BalanceCheck
		if e = rows.Scan(&c.ID, &c.AccountID, &c.AsOf, &c.Actual, &c.Book, &c.Difference, &c.SourceVersion); e != nil {
			return nil, e
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
