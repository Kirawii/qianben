package store

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Kirawii/qianben/backend/internal/accounting"
	"github.com/Kirawii/qianben/backend/internal/domain"
	"github.com/jackc/pgx/v5"
	"reflect"
	"strings"
	"time"
)

type EventRequest struct {
	CommandID        string       `json:"command_id"`
	EventID          string       `json:"event_id"`
	ExpectedRevision int64        `json:"expected_revision"`
	Facts            domain.Facts `json:"facts"`
	LearnCategory    bool         `json:"learn_category"`
}

func eventTx(ctx context.Context, tx pgx.Tx, ledger, id string) (domain.Event, error) {
	var ev domain.Event
	var facts, ids []byte
	e := tx.QueryRow(ctx, `SELECT e.id::text,e.status,e.revision,r.facts,r.evidence_ids,e.review_reason,COALESCE(e.canonical_id::text,''),EXISTS(SELECT 1 FROM qb.posting_intents p WHERE p.ledger_id=e.ledger_id AND p.lineage_id=e.id AND p.purpose='ECONOMIC' AND p.status='ACTIVE') FROM qb.events e JOIN qb.event_revisions r ON r.id=e.current_revision_id WHERE e.ledger_id=$1 AND e.id=$2`, ledger, id).Scan(&ev.ID, &ev.Status, &ev.Revision, &facts, &ids, &ev.Reason, &ev.CanonicalID, &ev.Posted)
	if errors.Is(e, pgx.ErrNoRows) {
		return ev, domain.Missing("事件不存在")
	}
	if e != nil {
		return ev, e
	}
	if e = json.Unmarshal(facts, &ev.Facts); e != nil {
		return ev, e
	}
	e = json.Unmarshal(ids, &ev.EvidenceIDs)
	return ev, e
}
func writeEvent(ctx context.Context, tx pgx.Tx, l domain.Ledger, id string, revision int64, f domain.Facts, evidence []string, policy ...string) (domain.Event, error) {
	ev := domain.Event{ID: id, Revision: revision + 1, Facts: f, EvidenceIDs: evidence, Status: "ACTIVE"}
	if evidence == nil {
		ev.EvidenceIDs = []string{}
	}
	all, sys, e := accountsTx(ctx, tx, l.ID)
	if e != nil {
		return ev, e
	}
	for _, accountID := range []string{f.FundingAccount, f.RepaymentAccount} {
		if a, ok := all[accountID]; ok && !strings.HasPrefix(a.Code, "user.") {
			return ev, domain.Invalid("实际账户不能使用系统会计科目")
		}
	}
	p, e := accounting.Plan(l, f, all, sys)
	if f.Kind == "CARD_REPAYMENT" && f.RepaymentAccount != "" {
		p, e = accounting.Repayment(l, f, all[f.FundingAccount], all[f.RepaymentAccount])
	}
	if f.Amount == 0 && f.Kind == "UNKNOWN" {
		e = &accounting.Blocked{Reason: "AMOUNT_REQUIRED"}
	}
	if e != nil {
		var blocked *accounting.Blocked
		if !errors.As(e, &blocked) {
			return ev, e
		}
		ev.Status = "REVIEW_REQUIRED"
		ev.Reason = blocked.Reason
		if blocked.Reason == "HISTORICAL_ONLY" {
			ev.Status = "HISTORICAL_ONLY"
		}
		p.Entries = []domain.Entry{}
	}
	if e == nil && (f.Kind == "CASH_OUT" || f.Kind == "CASH_IN") {
		ev.Status = "REVIEW_REQUIRED"
		ev.Reason = "PURPOSE_REQUIRED"
	}
	if ev.Status == "ACTIVE" && (f.Kind == "REFUND" || f.Kind == "ASSET_REFUND") && f.OriginalEvent != "" {
		if !domain.IsUUID(f.OriginalEvent) || f.OriginalEvent == id {
			return ev, domain.Invalid("原消费事件无效")
		}
		original, err := eventTx(ctx, tx, l.ID, f.OriginalEvent)
		if err != nil {
			return ev, err
		}
		expected := "EXPENSE"
		if f.Kind == "ASSET_REFUND" {
			expected = "ASSET_PURCHASE"
		}
		if !original.Posted || original.Facts.Kind != expected {
			return ev, domain.Invalid("退款须关联已入账的同类原消费")
		}
		if original.Facts.OccurredAt != nil && f.OccurredAt.Before(*original.Facts.OccurredAt) {
			return ev, domain.Invalid("退款时间早于原消费")
		}
		var used int64
		err = tx.QueryRow(ctx, `SELECT COALESCE(sum(allocation),0)::bigint FROM qb.relations WHERE ledger_id=$1 AND to_event=$2 AND from_event<>$3 AND type='REFUND_OF' AND active`, l.ID, f.OriginalEvent, id).Scan(&used)
		if err != nil {
			return ev, err
		}
		total, err := domain.Add(domain.Amount(used), f.Amount)
		if err != nil || total > original.Facts.Amount {
			return ev, domain.Invalid("累计退款超过原消费")
		}
	}
	unchangedAccounting := false
	if revision > 0 && len(p.Entries) > 0 {
		old, err := eventTx(ctx, tx, l.ID, id)
		if err != nil {
			return ev, err
		}
		normalize := func(f domain.Facts) domain.Facts {
			f.Category, f.Merchant, f.Note, f.AssetTitle = "", "", "", ""
			if f.OccurredAt != nil {
				at := f.OccurredAt.UTC()
				f.OccurredAt = &at
			}
			return f
		}
		unchangedAccounting = old.Posted && reflect.DeepEqual(normalize(old.Facts), normalize(f))
	}
	rid := domain.ID()
	policyVersion := "core-v1"
	if len(policy) > 0 {
		policyVersion = policy[0]
	}
	_, e = tx.Exec(ctx, `INSERT INTO qb.event_revisions(id,ledger_id,event_id,number,facts,evidence_ids,committed_version,policy_version) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, rid, l.ID, id, ev.Revision, domain.Hashable(f), domain.Hashable(ev.EvidenceIDs), l.Version+1, policyVersion)
	if e != nil {
		return ev, e
	}
	if unchangedAccounting {
		ev.Posted = true
		_, e = tx.Exec(ctx, `UPDATE qb.assets SET title=$3 WHERE ledger_id=$1 AND event_id=$2`, l.ID, id, f.AssetTitle)
		if e != nil {
			return ev, e
		}
		_, e = tx.Exec(ctx, `UPDATE qb.events SET revision=$3,current_revision_id=$4,status=$5,review_reason=$6 WHERE ledger_id=$1 AND id=$2`, l.ID, id, ev.Revision, rid, ev.Status, ev.Reason)
		return ev, e
	}
	if p.Entries == nil {
		p.Entries = []domain.Entry{}
	}
	effective := l.Cutover
	if f.OccurredAt != nil {
		effective = *f.OccurredAt
	}
	var journal *string
	e = tx.QueryRow(ctx, `SELECT qb.post($1,$2,'ECONOMIC',$3,$4,$5,$6)::text`, l.ID, id, rid, domain.Hashable(p.Entries), effective, l.Version+1).Scan(&journal)
	if e != nil {
		return ev, e
	}
	ev.Posted = journal != nil
	_, e = tx.Exec(ctx, `INSERT INTO qb.relation_history(ledger_id,relation_id,prior_data,result_data,committed_version) SELECT ledger_id,id,to_jsonb(r),'{"active":false}'::jsonb,$3 FROM qb.relations r WHERE ledger_id=$1 AND from_event=$2 AND type='REFUND_OF' AND active`, l.ID, id, l.Version+1)
	if e != nil {
		return ev, e
	}
	_, e = tx.Exec(ctx, `UPDATE qb.relations SET active=false WHERE ledger_id=$1 AND from_event=$2 AND type='REFUND_OF' AND active`, l.ID, id)
	if e != nil {
		return ev, e
	}
	if ev.Posted && f.OriginalEvent != "" && (f.Kind == "REFUND" || f.Kind == "ASSET_REFUND") {
		_, e = tx.Exec(ctx, `INSERT INTO qb.relations(id,ledger_id,from_event,to_event,type,allocation,committed_version) VALUES($1,$2,$3,$4,'REFUND_OF',$5,$6)`, domain.ID(), l.ID, id, f.OriginalEvent, int64(f.Amount), l.Version+1)
		if e != nil {
			return ev, e
		}
	}
	_, e = tx.Exec(ctx, `DELETE FROM qb.assets WHERE ledger_id=$1 AND event_id=$2`, l.ID, id)
	if e != nil {
		return ev, e
	}
	if ev.Posted && f.Kind == "ASSET_PURCHASE" {
		_, e = tx.Exec(ctx, `INSERT INTO qb.assets(id,ledger_id,event_id,title,original_minor) VALUES($1,$2,$3,$4,$5)`, domain.ID(), l.ID, id, f.AssetTitle, int64(f.Amount))
		if e != nil {
			return ev, e
		}
	}
	_, e = tx.Exec(ctx, `UPDATE qb.events SET revision=$3,current_revision_id=$4,status=$5,review_reason=$6 WHERE ledger_id=$1 AND id=$2`, l.ID, id, ev.Revision, rid, ev.Status, ev.Reason)
	return ev, e
}
func (d *DB) SaveEvent(ctx context.Context, actor, ledger string, r EventRequest) (json.RawMessage, error) {
	if r.Facts.OccurredAt != nil {
		at := r.Facts.OccurredAt.Truncate(time.Microsecond)
		r.Facts.OccurredAt = &at
	}
	if !domain.IsUUID(r.EventID) || r.Facts.Amount <= 0 || len(r.Facts.Merchant) > 200 || len(r.Facts.Note) > 2000 || len(r.Facts.Category) > 100 {
		return nil, domain.Invalid("事件字段无效")
	}
	return command(ctx, d, actor, ledger, r.CommandID, r, "EVENT_CONFIRMED", func(tx pgx.Tx, l domain.Ledger) (any, error) {
		var revision int64
		var status string
		e := tx.QueryRow(ctx, `SELECT revision,status FROM qb.events WHERE ledger_id=$1 AND id=$2`, l.ID, r.EventID).Scan(&revision, &status)
		ids := []string{}
		if errors.Is(e, pgx.ErrNoRows) {
			if r.ExpectedRevision != 0 {
				return nil, domain.Conflict("事件版本已改变")
			}
			_, e = tx.Exec(ctx, `INSERT INTO qb.events(id,ledger_id) VALUES($1,$2)`, r.EventID, l.ID)
		} else if e == nil {
			if revision != r.ExpectedRevision || status == "MERGED" || status == "SPLIT" {
				return nil, domain.Conflict("事件版本已改变")
			}
			old, err := eventTx(ctx, tx, l.ID, r.EventID)
			if err != nil {
				return nil, err
			}
			ids = old.EvidenceIDs
			var linked bool
			err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM qb.relations WHERE ledger_id=$1 AND active AND ((to_event=$2 AND type='REFUND_OF') OR (type='REIMBURSES' AND (from_event=$2 OR to_event=$2)) OR (type='TRANSFER_LEG_OF' AND (from_event=$2 OR to_event=$2))))`, l.ID, r.EventID).Scan(&linked)
			if err != nil {
				return nil, err
			}
			timeChanged := (old.Facts.OccurredAt == nil) != (r.Facts.OccurredAt == nil)
			if old.Facts.OccurredAt != nil && r.Facts.OccurredAt != nil {
				timeChanged = !old.Facts.OccurredAt.Equal(*r.Facts.OccurredAt)
			}
			if linked && (old.Facts.Kind != r.Facts.Kind || old.Facts.Amount != r.Facts.Amount || old.Facts.FundingAccount != r.Facts.FundingAccount || old.Facts.Currency != r.Facts.Currency || old.Facts.TimePrecision != r.Facts.TimePrecision || old.Facts.RepaymentAccount != r.Facts.RepaymentAccount || timeChanged) {
				return nil, domain.Conflict("关联事件须先解除或修订后再改原消费")
			}
		}
		if e != nil {
			return nil, e
		}
		source, obs := domain.ID(), domain.ID()
		_, e = tx.Exec(ctx, `INSERT INTO qb.source_events(id,ledger_id,source_identity,object_key,version) VALUES($1,$2,'manual.confirmation',$3,1)`, source, l.ID, r.CommandID)
		if e != nil {
			return nil, e
		}
		payload := domain.Hashable(r)
		_, e = tx.Exec(ctx, `INSERT INTO qb.observations(id,ledger_id,source_event_id,snapshot_key,version,payload_hash,payload) VALUES($1,$2,$3,'1',1,$4,$5)`, obs, l.ID, source, Hash(payload), payload)
		if e != nil {
			return nil, e
		}
		_, e = tx.Exec(ctx, `INSERT INTO qb.evidence_links(ledger_id,source_event_id,event_id,observation_id) VALUES($1,$2,$3,$4)`, l.ID, source, r.EventID, obs)
		if e != nil {
			return nil, e
		}
		if r.LearnCategory {
			if !textLimit(r.Facts.Merchant, 200) || !textLimit(r.Facts.Category, 100) {
				return nil, domain.Invalid("学习分类须有商户和分类")
			}
			_, e = tx.Exec(ctx, `UPDATE qb.rules SET active=false WHERE ledger_id=$1 AND merchant=$2 AND active`, l.ID, r.Facts.Merchant)
			if e != nil {
				return nil, e
			}
			_, e = tx.Exec(ctx, `INSERT INTO qb.rules(id,ledger_id,merchant,category,version) VALUES($1,$2,$3,$4,$5)`, domain.ID(), l.ID, r.Facts.Merchant, r.Facts.Category, l.Version+1)
			if e != nil {
				return nil, e
			}
		}
		ev, err := writeEvent(ctx, tx, l, r.EventID, revision, r.Facts, append(ids, obs))
		if err != nil {
			return nil, err
		}
		if err = recordUserFunding(ctx, tx, l, actor, r.EventID, obs, r.Facts); err != nil {
			return nil, err
		}
		return ev, nil
	})
}
func (d *DB) Events(ctx context.Context, actor, ledger string) ([]domain.Event, error) {
	return d.EventsPage(ctx, actor, ledger, "")
}
func (d *DB) EventsPage(ctx context.Context, actor, ledger, before string) ([]domain.Event, error) {
	var cursor any
	if before != "" {
		if !domain.IsUUID(before) {
			return nil, domain.Invalid("分页游标无效")
		}
		cursor = before
	}
	if e := d.Authorized(ctx, actor, ledger); e != nil {
		return nil, e
	}
	tx, e := d.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	rows, e := tx.Query(ctx, `SELECT id::text FROM qb.events WHERE ledger_id=$1 AND ($2::uuid IS NULL OR (created_at,id)<(SELECT created_at,id FROM qb.events WHERE ledger_id=$1 AND id=$2)) ORDER BY created_at DESC,id DESC LIMIT 500`, ledger, cursor)
	if e != nil {
		return nil, e
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return nil, e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	result := []domain.Event{}
	for _, id := range ids {
		ev, err := eventTx(ctx, tx, ledger, id)
		if err != nil {
			return nil, err
		}
		result = append(result, ev)
	}
	return result, tx.Commit(ctx)
}
