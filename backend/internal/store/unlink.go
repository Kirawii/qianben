package store

import (
	"context"
	"encoding/json"
	"github.com/Kirawii/qianben/backend/internal/domain"
	"github.com/jackc/pgx/v5"
)

type UnlinkRequest struct {
	CommandID  string `json:"command_id"`
	RelationID string `json:"relation_id"`
}

func (d *DB) Relations(ctx context.Context, actor, ledger, event string) ([]json.RawMessage, error) {
	if !domain.IsUUID(event) {
		return nil, domain.Invalid("事件 ID 无效")
	}
	if e := d.Authorized(ctx, actor, ledger); e != nil {
		return nil, e
	}
	rows, e := d.Pool.Query(ctx, `SELECT to_jsonb(r) FROM qb.relations r WHERE ledger_id=$1 AND active AND (from_event=$2 OR to_event=$2)`, ledger, event)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	result := []json.RawMessage{}
	for rows.Next() {
		var b []byte
		if e = rows.Scan(&b); e != nil {
			return nil, e
		}
		result = append(result, json.RawMessage(b))
	}
	return result, rows.Err()
}
func (d *DB) UnlinkTransfer(ctx context.Context, actor, ledger string, r UnlinkRequest) (json.RawMessage, error) {
	if !domain.IsUUID(r.RelationID) {
		return nil, domain.Invalid("关系 ID 无效")
	}
	return command(ctx, d, actor, ledger, r.CommandID, r, "TRANSFER_UNLINKED", func(tx pgx.Tx, l domain.Ledger) (any, error) {
		var prior []byte
		e := tx.QueryRow(ctx, `SELECT to_jsonb(r) FROM qb.relations r WHERE ledger_id=$1 AND id=$2 AND active AND type='TRANSFER_LEG_OF' FOR UPDATE`, ledger, r.RelationID).Scan(&prior)
		if e == pgx.ErrNoRows {
			return nil, domain.Missing("转账关联不存在")
		}
		if e != nil {
			return nil, e
		}
		_, e = tx.Exec(ctx, `INSERT INTO qb.relation_history(ledger_id,relation_id,prior_data,result_data,committed_version) VALUES($1,$2,$3,'{"active":false}',$4)`, ledger, r.RelationID, prior, l.Version+1)
		if e != nil {
			return nil, e
		}
		_, e = tx.Exec(ctx, `UPDATE qb.relations SET active=false WHERE ledger_id=$1 AND id=$2`, ledger, r.RelationID)
		return map[string]bool{"unlinked": true}, e
	})
}
