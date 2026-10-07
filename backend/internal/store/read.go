package store

import (
	"context"
	"encoding/json"
	"github.com/Kirawii/qianben/backend/internal/domain"
)

func (d *DB) Evidence(ctx context.Context, actor, ledger, event string) ([]json.RawMessage, error) {
	if !domain.IsUUID(event) {
		return nil, domain.Invalid("事件 ID 无效")
	}
	if e := d.Authorized(ctx, actor, ledger); e != nil {
		return nil, e
	}
	rows, e := d.Pool.Query(ctx, `SELECT jsonb_build_object('id',o.id,'source_identity',s.source_identity,'source_object_key',s.object_key,'version',o.version,'payload',o.payload,'received_at',o.created_at) FROM qb.evidence_links el JOIN qb.observations o ON o.id=el.observation_id JOIN qb.source_events s ON s.id=el.source_event_id WHERE el.ledger_id=$1 AND el.event_id=$2 ORDER BY o.created_at`, ledger, event)
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
func (d *DB) Assets(ctx context.Context, actor, ledger string) ([]json.RawMessage, error) {
	if e := d.Authorized(ctx, actor, ledger); e != nil {
		return nil, e
	}
	rows, e := d.Pool.Query(ctx, `SELECT jsonb_build_object('id',a.id,'event_id',a.event_id,'title',a.title,'purchase_minor',a.original_minor::text,'original_minor',(a.original_minor-COALESCE((SELECT sum(r.allocation) FROM qb.relations r WHERE r.ledger_id=a.ledger_id AND r.to_event=a.event_id AND r.type='REFUND_OF' AND r.active),0))::text) FROM qb.assets a WHERE ledger_id=$1 ORDER BY title`, ledger)
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
