package store

import (
	"context"
	"encoding/json"
	"github.com/Kirawii/qianben/backend/internal/domain"
	"github.com/Kirawii/qianben/backend/internal/evidence"
	"github.com/jackc/pgx/v5"
	"strconv"
	"time"
)

type DuplicateCandidate struct {
	Event   domain.Event `json:"event"`
	Reasons []string     `json:"reasons"`
}
type CandidateResult struct {
	Version        int64                `json:"source_version"`
	Algorithm      string               `json:"algorithm_version"`
	TargetRevision int64                `json:"target_revision"`
	Truncated      bool                 `json:"search_truncated"`
	Ambiguous      bool                 `json:"ambiguous"`
	Candidates     []DuplicateCandidate `json:"candidates"`
}

func (d *DB) DuplicateCandidates(ctx context.Context, actor, ledger, event string) (CandidateResult, error) {
	result := CandidateResult{Algorithm: "duplicate-review-v1", Candidates: []DuplicateCandidate{}}
	if !domain.IsUUID(event) {
		return result, domain.Invalid("事件 ID 无效")
	}
	tx, e := d.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return result, e
	}
	defer tx.Rollback(ctx)
	l, e := loadLedger(ctx, tx, actor, ledger, "")
	if e != nil {
		return result, e
	}
	result.Version = l.Version
	target, e := eventTx(ctx, tx, ledger, event)
	if e != nil {
		return result, e
	}
	if target.Status == "MERGED" || target.Status == "SPLIT" {
		return result, domain.Conflict("终结事件不能生成合并候选")
	}
	result.TargetRevision = target.Revision
	rows, e := tx.Query(ctx, `SELECT ev.id::text,ev.revision,ev.status,ev.review_reason,v.facts,v.evidence_ids,EXISTS(SELECT 1 FROM qb.posting_intents p WHERE p.ledger_id=ev.ledger_id AND p.lineage_id=ev.id AND p.purpose='ECONOMIC' AND p.status='ACTIVE') FROM qb.events ev JOIN qb.event_revisions v ON v.id=ev.current_revision_id WHERE ev.ledger_id=$1 AND ev.id<>$2 AND ev.status NOT IN ('MERGED','SPLIT') AND v.facts->>'amount_minor'=$3 ORDER BY ev.created_at DESC,ev.id DESC LIMIT 501`, ledger, event, strconv.FormatInt(int64(target.Facts.Amount), 10))
	if e != nil {
		return result, e
	}
	events := map[string]domain.Event{target.ID: target}
	ids := []string{target.ID}
	for rows.Next() {
		var ev domain.Event
		var facts, evidenceIDs []byte
		if e = rows.Scan(&ev.ID, &ev.Revision, &ev.Status, &ev.Reason, &facts, &evidenceIDs, &ev.Posted); e != nil {
			rows.Close()
			return result, e
		}
		if len(ids) > 500 {
			result.Truncated = true
			break
		}
		if e = json.Unmarshal(facts, &ev.Facts); e != nil {
			rows.Close()
			return result, e
		}
		if e = json.Unmarshal(evidenceIDs, &ev.EvidenceIDs); e != nil {
			rows.Close()
			return result, e
		}
		events[ev.ID] = ev
		ids = append(ids, ev.ID)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return result, e
	}
	clusters := map[string]evidence.MatchEvent{}
	for id, ev := range events {
		clusters[id] = evidence.MatchEvent{Facts: ev.Facts, Sources: map[string][]string{}}
	}
	rows, e = tx.Query(ctx, `SELECT ev.id::text,s.source_identity,s.object_key,min(NULLIF(o.payload->>'observed_at','')::timestamptz) FROM qb.events ev JOIN qb.event_revisions v ON v.id=ev.current_revision_id CROSS JOIN LATERAL jsonb_array_elements_text(v.evidence_ids) ref JOIN qb.observations o ON o.id=ref.value::uuid AND o.ledger_id=ev.ledger_id JOIN qb.source_events s ON s.id=o.source_event_id WHERE ev.ledger_id=$1 AND ev.id=ANY($2::uuid[]) AND s.source_identity LIKE 'android.notification:%' GROUP BY ev.id,s.source_identity,s.object_key`, ledger, ids)
	if e != nil {
		return result, e
	}
	for rows.Next() {
		var id, provider, object string
		var observed *time.Time
		if e = rows.Scan(&id, &provider, &object, &observed); e != nil {
			rows.Close()
			return result, e
		}
		cluster := clusters[id]
		cluster.Sources[provider] = append(cluster.Sources[provider], object)
		if observed != nil && (cluster.ObservedAt.IsZero() || observed.Before(cluster.ObservedAt)) {
			cluster.ObservedAt = *observed
		}
		clusters[id] = cluster
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return result, e
	}
	for _, id := range ids[1:] {
		if reasons := evidence.DuplicateReasons(clusters[event], clusters[id]); len(reasons) > 0 {
			result.Candidates = append(result.Candidates, DuplicateCandidate{events[id], reasons})
		}
	}
	result.Ambiguous = len(result.Candidates) > 1
	return result, tx.Commit(ctx)
}
