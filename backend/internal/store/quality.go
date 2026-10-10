package store

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
)

func (d *DB) Quality(ctx context.Context, actor, ledger string) (json.RawMessage, error) {
	tx, e := d.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	l, e := loadLedger(ctx, tx, actor, ledger, "")
	if e != nil {
		return nil, e
	}
	var value []byte
	e = tx.QueryRow(ctx, `WITH current_events AS (
 SELECT ev.id,ev.status,ev.review_reason,ev.created_at,
 EXISTS(SELECT 1 FROM qb.posting_intents p WHERE p.ledger_id=ev.ledger_id AND p.lineage_id=ev.id AND p.purpose='ECONOMIC' AND p.status='ACTIVE') posted,
 EXISTS(SELECT 1 FROM jsonb_array_elements_text(v.evidence_ids) ref JOIN qb.observations o ON o.id=ref.value::uuid JOIN qb.source_events s ON s.id=o.source_event_id WHERE s.source_identity='manual.confirmation') manual,
 EXISTS(SELECT 1 FROM jsonb_array_elements_text(v.evidence_ids) ref JOIN qb.observations o ON o.id=ref.value::uuid JOIN qb.source_events s ON s.id=o.source_event_id WHERE s.source_identity LIKE 'csv.account:%' OR s.source_identity LIKE 'csv.native:%') imported
 FROM qb.events ev JOIN qb.event_revisions v ON v.id=ev.current_revision_id WHERE ev.ledger_id=$1 AND ev.status NOT IN ('MERGED','SPLIT')
 ) SELECT jsonb_build_object('source_version',$2::bigint,'algorithm_version','quality-v1','generated_at',now(),'age_basis','event_created_at',
 'posted_count',count(*) FILTER(WHERE posted),
 'automatic_posted_count',count(*) FILTER(WHERE posted AND NOT manual AND NOT imported),
 'manual_evidence_count',count(*) FILTER(WHERE manual),
 'csv_evidence_count',count(*) FILTER(WHERE imported),
 'review_count',count(*) FILTER(WHERE status='REVIEW_REQUIRED'),
 'review_older_than_7d_count',count(*) FILTER(WHERE status='REVIEW_REQUIRED' AND created_at<now()-interval '7 days'),
 'oldest_review_event_created_at',min(created_at) FILTER(WHERE status='REVIEW_REQUIRED'),
 'review_reasons',COALESCE((SELECT jsonb_agg(jsonb_build_object('reason',reason,'count',n) ORDER BY reason) FROM (SELECT review_reason reason,count(*) n FROM current_events WHERE status='REVIEW_REQUIRED' GROUP BY review_reason) reasons),'[]'::jsonb)) FROM current_events`, ledger, l.Version).Scan(&value)
	if e != nil {
		return nil, e
	}
	return json.RawMessage(value), tx.Commit(ctx)
}
