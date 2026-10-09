package store

import (
	"context"
	"github.com/Kirawii/qianben/backend/internal/domain"
	"github.com/jackc/pgx/v5"
	"sort"
	"strconv"
	"time"
)

type Reports struct {
	ConfirmedAssets      domain.Amount   `json:"confirmed_assets_minor"`
	ConfirmedLiabilities domain.Amount   `json:"confirmed_liabilities_minor"`
	ConfirmedNetWorth    domain.Amount   `json:"confirmed_net_worth_minor"`
	ProvisionalNetWorth  domain.Amount   `json:"provisional_net_worth_minor"`
	Assets               domain.Amount   `json:"assets_minor"`
	Liabilities          domain.Amount   `json:"liabilities_minor"`
	NetWorth             domain.Amount   `json:"net_worth_minor"`
	Income               domain.Amount   `json:"income_minor"`
	Surplus              domain.Amount   `json:"surplus_minor"`
	Categories           []CategoryTotal `json:"categories"`
	Version              int64           `json:"source_version"`
	Algorithm            string          `json:"algorithm_version"`
	From                 time.Time       `json:"from"`
	To                   time.Time       `json:"to"`
	Expense              domain.Amount   `json:"expense_minor"`
	Consumption          domain.Amount   `json:"consumption_minor"`
	CashChange           domain.Amount   `json:"cash_change_minor"`
	ExternalCash         domain.Amount   `json:"external_cash_minor"`
	InternalCash         domain.Amount   `json:"internal_cash_minor"`
	PendingTransferCash  domain.Amount   `json:"pending_transfer_cash_minor"`
	PendingTransferIn    domain.Amount   `json:"pending_transfer_in_minor"`
	PendingTransferOut   domain.Amount   `json:"pending_transfer_out_minor"`
	CardRepaymentCash    domain.Amount   `json:"card_repayment_cash_minor"`
	UnresolvedCash       domain.Amount   `json:"unresolved_cash_minor"`
	UnresolvedBalance    domain.Amount   `json:"unresolved_balance_minor"`
	ReviewCount          int64           `json:"review_count"`
	TransferClearing     domain.Amount   `json:"transfer_clearing_minor"`
	UnlinkedTransfers    int64           `json:"unlinked_transfer_count"`
}

type CategoryTotal struct {
	Category string        `json:"category"`
	Amount   domain.Amount `json:"consumption_minor"`
}

func (d *DB) Reports(ctx context.Context, actor, ledger string, from, to time.Time) (Reports, error) {
	r := Reports{From: from, To: to, Algorithm: "core-v4", Categories: []CategoryTotal{}}
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
	rows, e := tx.Query(ctx, `SELECT a.code,a.cash,(e.debit-e.credit)::text,j.effective_at,COALESCE(v.facts->>'kind','OPENING'),COALESCE(NULLIF(current_facts.facts->>'category',''),'未分类'),
 COALESCE((SELECT CASE WHEN peer_account.cash THEN 'INTERNAL' ELSE 'EXTERNAL' END FROM qb.relations rel JOIN qb.events peer ON peer.id=CASE WHEN rel.from_event=current_event.id THEN rel.to_event ELSE rel.from_event END JOIN qb.event_revisions peer_facts ON peer_facts.id=peer.current_revision_id JOIN qb.accounts peer_account ON peer_account.id::text=peer_facts.facts->>'funding_account_id' AND peer_account.ledger_id=rel.ledger_id WHERE rel.ledger_id=$1 AND rel.type='TRANSFER_LEG_OF' AND rel.active AND (rel.from_event=current_event.id OR rel.to_event=current_event.id) AND peer.status NOT IN ('MERGED','SPLIT') AND (peer_facts.facts->>'occurred_at')::timestamptz<$2 AND EXISTS(SELECT 1 FROM qb.posting_intents pi WHERE pi.ledger_id=rel.ledger_id AND pi.lineage_id=peer.id AND pi.status='ACTIVE' AND pi.purpose='ECONOMIC') LIMIT 1),'PENDING')
 FROM qb.entries e JOIN qb.accounts a ON a.id=e.account_id JOIN qb.journals j ON j.id=e.journal_id LEFT JOIN qb.journals original ON original.id=j.reversal_of JOIN qb.posting_revisions p ON p.id=COALESCE(original.posting_revision_id,j.posting_revision_id) LEFT JOIN qb.event_revisions v ON v.id=p.event_revision_id LEFT JOIN qb.events current_event ON current_event.id=v.event_id LEFT JOIN qb.event_revisions current_facts ON current_facts.id=current_event.current_revision_id WHERE e.ledger_id=$1`, ledger, to)
	if e != nil {
		return r, e
	}
	var suspenseAsset, suspenseLiability domain.Amount
	categories := map[string]domain.Amount{}
	for rows.Next() {
		var code, value, kind, category, transferClass string
		var cash bool
		var at time.Time
		if e = rows.Scan(&code, &cash, &value, &at, &kind, &category, &transferClass); e != nil {
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
		if code == "income.general" {
			r.Income, e = domain.Add(r.Income, -amount)
			if e != nil {
				rows.Close()
				return r, e
			}
		}
		if code == "expense.general" || code == "asset.fixed" {
			categories[category], e = domain.Add(categories[category], amount)
			if e != nil {
				rows.Close()
				return r, e
			}
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
			if kind == "CARD_REPAYMENT" {
				if e = add(&r.CardRepaymentCash); e != nil {
					rows.Close()
					return r, e
				}
			}
			if (kind == "TRANSFER_IN" || kind == "TRANSFER_OUT") && transferClass == "PENDING" {
				if kind == "TRANSFER_IN" {
					e = add(&r.PendingTransferIn)
				} else {
					r.PendingTransferOut, e = domain.Subtract(r.PendingTransferOut, amount)
				}
				if e != nil {
					rows.Close()
					return r, e
				}
			}
			if e = add(&r.CashChange); e != nil {
				rows.Close()
				return r, e
			}
			target := &r.ExternalCash
			switch kind {
			case "TRANSFER_OUT", "TRANSFER_IN":
				switch transferClass {
				case "INTERNAL":
					target = &r.InternalCash
				case "PENDING":
					target = &r.PendingTransferCash
				}
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
	for category, amount := range categories {
		r.Categories = append(r.Categories, CategoryTotal{category, amount})
	}
	sort.Slice(r.Categories, func(i, j int) bool { return r.Categories[i].Category < r.Categories[j].Category })
	r.Surplus, e = domain.Subtract(r.Income, r.Expense)
	if e != nil {
		return r, e
	}
	// Balance-sheet values are as of the exclusive period end, independent of period start.
	var assets, liabilities string
	e = tx.QueryRow(ctx, `SELECT COALESCE(sum(CASE WHEN a.type='ASSET' THEN e.debit::numeric-e.credit ELSE 0 END),0)::text,COALESCE(sum(CASE WHEN a.type='LIABILITY' THEN e.credit::numeric-e.debit ELSE 0 END),0)::text FROM qb.entries e JOIN qb.accounts a ON a.id=e.account_id JOIN qb.journals j ON j.id=e.journal_id WHERE e.ledger_id=$1 AND j.effective_at<$2`, ledger, to).Scan(&assets, &liabilities)
	if e != nil {
		return r, e
	}
	a, err := strconv.ParseInt(assets, 10, 64)
	if err != nil {
		return r, err
	}
	b, err := strconv.ParseInt(liabilities, 10, 64)
	if err != nil {
		return r, err
	}
	r.Assets, r.Liabilities = domain.Amount(a), domain.Amount(b)
	r.NetWorth, e = domain.Subtract(r.Assets, r.Liabilities)
	if e != nil {
		return r, e
	}
	// Debit suspense assets and credit suspense liabilities are both unresolved.
	// Reversal lines cancel before balances are combined.
	r.UnresolvedBalance, e = domain.Add(suspenseAsset, -suspenseLiability)
	if e != nil {
		return r, e
	}
	r.ConfirmedAssets, e = domain.Subtract(r.Assets, suspenseAsset)
	if e != nil {
		return r, e
	}
	r.ConfirmedLiabilities, e = domain.Add(r.Liabilities, suspenseLiability)
	if e != nil {
		return r, e
	}
	r.ConfirmedNetWorth, e = domain.Subtract(r.ConfirmedAssets, r.ConfirmedLiabilities)
	if e != nil {
		return r, e
	}
	r.ProvisionalNetWorth, e = domain.Add(suspenseAsset, suspenseLiability)
	if e != nil {
		return r, e
	}
	e = tx.QueryRow(ctx, `SELECT count(*) FROM qb.events WHERE ledger_id=$1 AND status='REVIEW_REQUIRED'`, ledger).Scan(&r.ReviewCount)
	if e != nil {
		return r, e
	}
	e = tx.QueryRow(ctx, `SELECT count(*) FROM qb.events ev JOIN qb.event_revisions v ON v.id=ev.current_revision_id WHERE ev.ledger_id=$1 AND v.facts->>'kind' IN ('TRANSFER_OUT','TRANSFER_IN') AND ev.status NOT IN ('MERGED','SPLIT') AND (v.facts->>'occurred_at')::timestamptz<$2 AND EXISTS(SELECT 1 FROM qb.posting_intents p WHERE p.ledger_id=ev.ledger_id AND p.lineage_id=ev.id AND p.purpose='ECONOMIC' AND p.status='ACTIVE') AND NOT EXISTS(SELECT 1 FROM qb.relations r WHERE r.ledger_id=ev.ledger_id AND r.type='TRANSFER_LEG_OF' AND r.active AND (r.from_event=ev.id OR r.to_event=ev.id))`, ledger, to).Scan(&r.UnlinkedTransfers)
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
