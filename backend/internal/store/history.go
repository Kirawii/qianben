package store

import (
	"context"
	"encoding/json"
	"github.com/Kirawii/qianben/backend/internal/domain"
)

// History preserves the facts and posting effects of each accepted revision.
func (d *DB) History(ctx context.Context, actor, ledger, event string) ([]json.RawMessage, error) {
	if !domain.IsUUID(event) {
		return nil, domain.Invalid("事件 ID 无效")
	}
	if e := d.Authorized(ctx, actor, ledger); e != nil {
		return nil, e
	}
	var exists bool
	if e := d.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM qb.events WHERE ledger_id=$1 AND id=$2)`, ledger, event).Scan(&exists); e != nil {
		return nil, e
	}
	if !exists {
		return nil, domain.Missing("事件不存在")
	}
	rows, e := d.Pool.Query(ctx, `SELECT jsonb_build_object(
 'revision',v.number,'facts',v.facts,'source_version',v.committed_version,'created_at',v.created_at,'policy_version',v.policy_version,'evidence_ids',v.evidence_ids,
 'accounting_revision',(SELECT max(pr.number) FROM qb.event_revisions pr JOIN qb.posting_revisions pp ON pp.event_revision_id=pr.id WHERE pr.event_id=v.event_id AND pr.number<=v.number AND EXISTS(SELECT 1 FROM qb.journals pj WHERE pj.posting_revision_id=pp.id AND pj.kind='NORMAL')),
 'journals',COALESCE((SELECT jsonb_agg(jsonb_build_object('id',j.id,'kind',j.kind,'effective_at',j.effective_at,'reversal_of',j.reversal_of,'reversed_by',(SELECT rev.id FROM qb.journals rev WHERE rev.reversal_of=j.id),
 'entries',COALESCE((SELECT jsonb_agg(jsonb_build_object('account',a.name,'debit_minor',en.debit::text,'credit_minor',en.credit::text) ORDER BY a.code,en.id) FROM qb.entries en JOIN qb.accounts a ON a.id=en.account_id WHERE en.journal_id=j.id),'[]'::jsonb)) ORDER BY CASE WHEN j.kind='REVERSAL' THEN 0 ELSE 1 END,j.created_at,j.id)
 FROM qb.posting_revisions p JOIN qb.journals j ON j.posting_revision_id=p.id WHERE p.event_revision_id=v.id),'[]'::jsonb))
 FROM qb.event_revisions v WHERE v.ledger_id=$1 AND v.event_id=$2 ORDER BY v.number DESC LIMIT 100`, ledger, event)
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
