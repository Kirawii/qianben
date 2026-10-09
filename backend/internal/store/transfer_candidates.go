package store

import (
	"context"
	"github.com/Kirawii/qianben/backend/internal/domain"
	"github.com/jackc/pgx/v5"
	"strconv"
	"time"
)

// These are review candidates. Amount and time alone do not prove a transfer identity.
func (d *DB) TransferCandidates(ctx context.Context, actor, ledger, event string) (CandidateResult, error) {
	result := CandidateResult{Algorithm: "transfer-review-v1", Candidates: []DuplicateCandidate{}}
	if !domain.IsUUID(event) {
		return result, domain.Invalid("事件 ID 无效")
	}
	tx, err := d.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	l, err := loadLedger(ctx, tx, actor, ledger, "")
	if err != nil {
		return result, err
	}
	result.Version = l.Version
	target, err := eventTx(ctx, tx, ledger, event)
	if err != nil {
		return result, err
	}
	result.TargetRevision = target.Revision
	f := target.Facts
	if !target.Posted || target.Status == "MERGED" || target.Status == "SPLIT" || (f.Kind != "TRANSFER_OUT" && f.Kind != "TRANSFER_IN") || f.Currency != "CNY" || f.OccurredAt == nil || f.TimePrecision != "EXACT" {
		return result, domain.Invalid("转账候选须有已入账的转账端及明确发生时间")
	}
	if err = unlinked(ctx, tx, ledger, event); err != nil {
		return result, err
	}
	opposite := "TRANSFER_IN"
	if f.Kind == "TRANSFER_IN" {
		opposite = "TRANSFER_OUT"
	}
	rows, err := tx.Query(ctx, `SELECT ev.id::text FROM qb.events ev JOIN qb.event_revisions v ON v.id=ev.current_revision_id JOIN qb.accounts a ON a.id::text=v.facts->>'funding_account_id' AND a.ledger_id=ev.ledger_id
 WHERE ev.ledger_id=$1 AND ev.id<>$2 AND ev.status NOT IN ('MERGED','SPLIT') AND v.facts->>'kind'=$3 AND v.facts->>'currency'='CNY' AND v.facts->>'amount_minor'=$4 AND v.facts->>'time_precision'='EXACT' AND v.facts->>'funding_account_id'<>$5 AND a.code LIKE 'user.%'
 AND (v.facts->>'occurred_at')::timestamptz BETWEEN $6 AND $7 AND EXISTS(SELECT 1 FROM qb.posting_intents p WHERE p.ledger_id=ev.ledger_id AND p.lineage_id=ev.id AND p.purpose='ECONOMIC' AND p.status='ACTIVE')
 AND NOT EXISTS(SELECT 1 FROM qb.relations r WHERE r.ledger_id=ev.ledger_id AND r.active AND r.type IN ('REFUND_OF','TRANSFER_LEG_OF','REIMBURSES') AND (r.from_event=ev.id OR r.to_event=ev.id)) ORDER BY ev.created_at DESC,ev.id DESC LIMIT 501`, ledger, event, opposite, strconv.FormatInt(int64(f.Amount), 10), f.FundingAccount, f.OccurredAt.Add(-7*24*time.Hour), f.OccurredAt.Add(7*24*time.Hour))
	if err != nil {
		return result, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return result, err
		}
		if len(ids) == 500 {
			result.Truncated = true
			break
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	for _, id := range ids {
		ev, e := eventTx(ctx, tx, ledger, id)
		if e != nil {
			return result, e
		}
		result.Candidates = append(result.Candidates, DuplicateCandidate{Event: ev, Reasons: []string{"等额 CNY、相反转账方向、不同实际账户", "两端均已入账且没有占用的经济关联", "发生时间相距不超过七天；须核实为同一笔划转，不能只凭金额关联"}})
	}
	result.Ambiguous = len(result.Candidates) > 1
	return result, tx.Commit(ctx)
}
