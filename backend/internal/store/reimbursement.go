package store

import (
	"context"
	"encoding/json"
	"github.com/Kirawii/qianben/backend/internal/domain"
	"github.com/jackc/pgx/v5"
)

type ReimbursementAllocation struct {
	AdvanceID        string        `json:"advance_id"`
	ExpectedRevision int64         `json:"expected_revision"`
	Amount           domain.Amount `json:"amount_minor"`
}
type ReimbursementRequest struct {
	CommandID        string                    `json:"command_id"`
	ReimbursementID  string                    `json:"reimbursement_id"`
	ExpectedRevision int64                     `json:"expected_revision"`
	Allocations      []ReimbursementAllocation `json:"allocations"`
}

func (d *DB) ReimbursementBalances(ctx context.Context, actor, ledger string) ([]json.RawMessage, error) {
	if e := d.Authorized(ctx, actor, ledger); e != nil {
		return nil, e
	}
	rows, e := d.Pool.Query(ctx, `SELECT jsonb_build_object('event_id',ev.id,'kind',v.facts->>'kind','amount_minor',v.facts->>'amount_minor','allocated_minor',COALESCE(sum(r.allocation),0)::text,'remaining_minor',((v.facts->>'amount_minor')::numeric-COALESCE(sum(r.allocation),0))::text) FROM qb.events ev JOIN qb.event_revisions v ON v.id=ev.current_revision_id LEFT JOIN qb.relations r ON r.ledger_id=ev.ledger_id AND r.type='REIMBURSES' AND r.active AND ((v.facts->>'kind'='ADVANCE' AND r.to_event=ev.id) OR (v.facts->>'kind'='REIMBURSEMENT' AND r.from_event=ev.id)) WHERE ev.ledger_id=$1 AND ev.status NOT IN ('MERGED','SPLIT') AND v.facts->>'kind' IN ('ADVANCE','REIMBURSEMENT') AND EXISTS(SELECT 1 FROM qb.posting_intents p WHERE p.ledger_id=ev.ledger_id AND p.lineage_id=ev.id AND p.purpose='ECONOMIC' AND p.status='ACTIVE') GROUP BY ev.id,v.id ORDER BY ev.created_at,ev.id`, ledger)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	result := []json.RawMessage{}
	for rows.Next() {
		var value []byte
		if e = rows.Scan(&value); e != nil {
			return nil, e
		}
		result = append(result, json.RawMessage(value))
	}
	return result, rows.Err()
}

// Replacing the allocation batch never posts another journal: collection was
// already recorded by the reimbursement event. An empty batch explicitly unlinks.
func (d *DB) AllocateReimbursement(ctx context.Context, actor, ledger string, r ReimbursementRequest) (json.RawMessage, error) {
	if !domain.IsUUID(r.ReimbursementID) || len(r.Allocations) > 50 {
		return nil, domain.Invalid("报销分配字段无效")
	}
	return command(ctx, d, actor, ledger, r.CommandID, r, "REIMBURSEMENT_ALLOCATED", func(tx pgx.Tx, l domain.Ledger) (any, error) {
		receipt, e := eventTx(ctx, tx, l.ID, r.ReimbursementID)
		if e != nil {
			return nil, e
		}
		if receipt.Revision != r.ExpectedRevision {
			return nil, domain.Conflict("报销版本已改变")
		}
		if !receipt.Posted || receipt.Facts.Kind != "REIMBURSEMENT" || receipt.Status == "MERGED" || receipt.Status == "SPLIT" {
			return nil, domain.Invalid("须选择已入账报销回款")
		}
		seen := map[string]bool{}
		var total domain.Amount
		for _, allocation := range r.Allocations {
			if !domain.IsUUID(allocation.AdvanceID) || seen[allocation.AdvanceID] || allocation.Amount <= 0 {
				return nil, domain.Invalid("垫付分配须为正数且不能重复")
			}
			seen[allocation.AdvanceID] = true
			advance, err := eventTx(ctx, tx, l.ID, allocation.AdvanceID)
			if err != nil {
				return nil, err
			}
			if advance.Revision != allocation.ExpectedRevision {
				return nil, domain.Conflict("垫付版本已改变")
			}
			if !advance.Posted || advance.Facts.Kind != "ADVANCE" || advance.Status == "MERGED" || advance.Status == "SPLIT" {
				return nil, domain.Invalid("须关联已入账垫付")
			}
			if receipt.Facts.OccurredAt.Before(*advance.Facts.OccurredAt) {
				return nil, domain.Invalid("报销时间不能早于垫付")
			}
			var used int64
			err = tx.QueryRow(ctx, `SELECT COALESCE(sum(allocation),0)::bigint FROM qb.relations WHERE ledger_id=$1 AND to_event=$2 AND from_event<>$3 AND type='REIMBURSES' AND active`, l.ID, advance.ID, receipt.ID).Scan(&used)
			if err != nil {
				return nil, err
			}
			covered, err := domain.Add(domain.Amount(used), allocation.Amount)
			if err != nil || covered > advance.Facts.Amount {
				return nil, domain.Invalid("累计报销超过垫付金额")
			}
			total, err = domain.Add(total, allocation.Amount)
			if err != nil || total > receipt.Facts.Amount {
				return nil, domain.Invalid("分配合计超过报销回款金额")
			}
		}
		_, e = tx.Exec(ctx, `INSERT INTO qb.relation_history(ledger_id,relation_id,prior_data,result_data,committed_version) SELECT ledger_id,id,to_jsonb(r),'{"active":false}'::jsonb,$3 FROM qb.relations r WHERE ledger_id=$1 AND from_event=$2 AND type='REIMBURSES' AND active`, l.ID, receipt.ID, l.Version+1)
		if e != nil {
			return nil, e
		}
		_, e = tx.Exec(ctx, `UPDATE qb.relations SET active=false WHERE ledger_id=$1 AND from_event=$2 AND type='REIMBURSES' AND active`, l.ID, receipt.ID)
		if e != nil {
			return nil, e
		}
		for _, allocation := range r.Allocations {
			_, e = tx.Exec(ctx, `INSERT INTO qb.relations(id,ledger_id,from_event,to_event,type,allocation,committed_version) VALUES($1,$2,$3,$4,'REIMBURSES',$5,$6)`, domain.ID(), l.ID, receipt.ID, allocation.AdvanceID, int64(allocation.Amount), l.Version+1)
			if e != nil {
				return nil, e
			}
		}
		return map[string]any{"allocated_minor": total, "unallocated_minor": receipt.Facts.Amount - total, "allocations": r.Allocations}, nil
	})
}
