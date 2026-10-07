package store

import (
	"context"
	"github.com/Kirawii/qianben/backend/internal/domain"
	"github.com/jackc/pgx/v5"
	"strconv"
	"time"
)

type Reports struct {
	Version           int64         `json:"source_version"`
	Algorithm         string        `json:"algorithm_version"`
	From              time.Time     `json:"from"`
	To                time.Time     `json:"to"`
	Expense           domain.Amount `json:"expense_minor"`
	Consumption       domain.Amount `json:"consumption_minor"`
	CashChange        domain.Amount `json:"cash_change_minor"`
	ExternalCash      domain.Amount `json:"external_cash_minor"`
	InternalCash      domain.Amount `json:"internal_cash_minor"`
	UnresolvedCash    domain.Amount `json:"unresolved_cash_minor"`
	UnresolvedBalance domain.Amount `json:"unresolved_balance_minor"`
	ReviewCount       int64         `json:"review_count"`
	TransferClearing  domain.Amount `json:"transfer_clearing_minor"`
	UnlinkedTransfers int64         `json:"unlinked_transfer_count"`
}

func (d *DB) Reports(ctx context.Context, actor, ledger string, from, to time.Time) (Reports, error) {
	r := Reports{From: from, To: to, Algorithm: "core-v1"}
	if !from.Before(to) {
		return r, domain.Invalid("报表时间范围无效")
	}
	tx, e := d.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return r, e
	}
	defer tx.Rollback(ctx)
	l, e := loadLedger(ctx, tx, actor, ledger, "")
	if e != nil {
		return r, e
	}
	r.Version = l.Version
	rows, e := tx.Query(ctx, `SELECT a.code,a.cash,(e.debit-e.credit)::text,j.effective_at,COALESCE(v.facts->>'kind','OPENING') FROM qb.entries e JOIN qb.accounts a ON a.id=e.account_id JOIN qb.journals j ON j.id=e.journal_id LEFT JOIN qb.journals original ON original.id=j.reversal_of JOIN qb.posting_revisions p ON p.id=COALESCE(original.posting_revision_id,j.posting_revision_id) LEFT JOIN qb.event_revisions v ON v.id=p.event_revision_id WHERE e.ledger_id=$1`, ledger)
	if e != nil {
		return r, e
	}
	var suspenseAsset, suspenseLiability domain.Amount
	for rows.Next() {
		var code, value, kind string
		var cash bool
		var at time.Time
		if e = rows.Scan(&code, &cash, &value, &at, &kind); e != nil {
			rows.Close()
			return r, e
		}
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			rows.Close()
			return r, err
		}
		amount := domain.Amount(n)
		add := func(target *domain.Amount) error { v, err := domain.Add(*target, amount); *target = v; return err }
		if at.Before(to) && code == "clearing.transfer" {
			if e = add(&r.TransferClearing); e != nil {
				rows.Close()
				return r, e
			}
		}
		if at.Before(to) && (code == "suspense.asset" || code == "suspense.liability") {
			target := &suspenseAsset
			if code == "suspense.liability" {
				target = &suspenseLiability
			}
			if e = add(target); e != nil {
				rows.Close()
				return r, e
			}
			amount = domain.Amount(n)
		}
		if at.Before(from) || !at.Before(to) || kind == "OPENING" {
			continue
		}
		if code == "expense.general" {
			if e = add(&r.Expense); e != nil {
				rows.Close()
				return r, e
			}
			if e = add(&r.Consumption); e != nil {
				rows.Close()
				return r, e
			}
		}
		if code == "asset.fixed" {
			if e = add(&r.Consumption); e != nil {
				rows.Close()
				return r, e
			}
		}
		if cash {
			if e = add(&r.CashChange); e != nil {
				rows.Close()
				return r, e
			}
			target := &r.ExternalCash
			switch kind {
			case "TRANSFER_OUT", "TRANSFER_IN", "CARD_REPAYMENT":
				target = &r.InternalCash
			case "CASH_OUT", "CASH_IN":
				target = &r.UnresolvedCash
			}
			if e = add(target); e != nil {
				rows.Close()
				return r, e
			}
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return r, e
	}
	// Debit suspense assets and credit suspense liabilities are both unresolved.
	// Reversal lines cancel before balances are combined.
	r.UnresolvedBalance, e = domain.Add(suspenseAsset, -suspenseLiability)
	if e != nil {
		return r, e
	}
	e = tx.QueryRow(ctx, `SELECT count(*) FROM qb.events WHERE ledger_id=$1 AND status='REVIEW_REQUIRED'`, ledger).Scan(&r.ReviewCount)
	if e != nil {
		return r, e
	}
	e = tx.QueryRow(ctx, `SELECT count(*) FROM qb.events ev JOIN qb.event_revisions v ON v.id=ev.current_revision_id WHERE ev.ledger_id=$1 AND v.facts->>'kind' IN ('TRANSFER_OUT','TRANSFER_IN') AND ev.status NOT IN ('MERGED','SPLIT') AND NOT EXISTS(SELECT 1 FROM qb.relations r WHERE r.ledger_id=ev.ledger_id AND r.type='TRANSFER_LEG_OF' AND r.active AND (r.from_event=ev.id OR r.to_event=ev.id))`, ledger).Scan(&r.UnlinkedTransfers)
	if e != nil {
		return r, e
	}
	return r, tx.Commit(ctx)
}
func (d *DB) Ledgers(ctx context.Context, actor string) ([]domain.Ledger, error) {
	rows, e := d.Pool.Query(ctx, `SELECT id::text,name,currency,cutover_time,version,status FROM qb.ledgers WHERE user_id=$1 AND status='ACTIVE' ORDER BY created_at`, actor)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	result := []domain.Ledger{}
	for rows.Next() {
		var l domain.Ledger
		if e = rows.Scan(&l.ID, &l.Name, &l.Currency, &l.Cutover, &l.Version, &l.Status); e != nil {
			return nil, e
		}
		result = append(result, l)
	}
	return result, rows.Err()
}
