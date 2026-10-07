package store

import (
	"context"
	"encoding/json"
	"github.com/Kirawii/qianben/backend/internal/domain"
	"github.com/jackc/pgx/v5"
	"time"
)

type Rule struct {
	ID            string    `json:"id"`
	Merchant      string    `json:"merchant"`
	Category      string    `json:"category"`
	Version       int64     `json:"version"`
	EffectiveFrom time.Time `json:"effective_from"`
	Active        bool      `json:"active"`
}

func (d *DB) Rules(ctx context.Context, actor, ledger string) ([]Rule, error) {
	tx, e := d.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	if _, e = loadLedger(ctx, tx, actor, ledger, ""); e != nil {
		return nil, e
	}
	rows, e := tx.Query(ctx, `SELECT id::text,merchant,category,version,effective_from,active FROM qb.rules WHERE ledger_id=$1 ORDER BY version DESC,id`, ledger)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Rule{}
	for rows.Next() {
		var r Rule
		if e = rows.Scan(&r.ID, &r.Merchant, &r.Category, &r.Version, &r.EffectiveFrom, &r.Active); e != nil {
			return nil, e
		}
		out = append(out, r)
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	rows.Close()
	return out, tx.Commit(ctx)
}

type RevokeRuleRequest struct {
	CommandID       string `json:"command_id"`
	RuleID          string `json:"rule_id"`
	ExpectedVersion int64  `json:"expected_version"`
}

func (d *DB) RevokeRule(ctx context.Context, actor, ledger string, r RevokeRuleRequest) (json.RawMessage, error) {
	if !domain.IsUUID(r.RuleID) || r.ExpectedVersion <= 0 {
		return nil, domain.Invalid("规则 ID 或版本无效")
	}
	return command(ctx, d, actor, ledger, r.CommandID, r, "RULE_REVOKED", func(tx pgx.Tx, l domain.Ledger) (any, error) {
		var version int64
		var active bool
		e := tx.QueryRow(ctx, `SELECT version,active FROM qb.rules WHERE ledger_id=$1 AND id=$2`, ledger, r.RuleID).Scan(&version, &active)
		if e == pgx.ErrNoRows {
			return nil, domain.Missing("规则不存在")
		}
		if e != nil {
			return nil, e
		}
		if version != r.ExpectedVersion || !active {
			return nil, domain.Conflict("规则已改变或已撤销，请刷新")
		}
		_, e = tx.Exec(ctx, `UPDATE qb.rules SET active=false WHERE ledger_id=$1 AND id=$2`, ledger, r.RuleID)
		return map[string]any{"rule_id": r.RuleID, "active": false, "source_version": l.Version + 1}, e
	})
}
