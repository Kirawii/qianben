package store

import (
	"context"
	"encoding/json"
	"github.com/Kirawii/qianben/backend/internal/domain"
	"github.com/jackc/pgx/v5"
	"strconv"
	"time"
)

type CreateLedgerRequest struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Cutover time.Time `json:"cutover_time"`
}

func (d *DB) CreateLedger(ctx context.Context, actor string, r CreateLedgerRequest) (domain.Ledger, error) {
	r.Cutover = r.Cutover.Truncate(time.Microsecond)
	var l domain.Ledger
	if !domain.IsUUID(r.ID) || !textLimit(r.Name, 100) || r.Cutover.IsZero() || r.Cutover.After(time.Now()) {
		return l, domain.Invalid("账本 ID、名称或启用时间无效")
	}
	tx, e := d.Pool.Begin(ctx)
	if e != nil {
		return l, e
	}
	defer tx.Rollback(ctx)
	if e = actorTx(ctx, tx, actor); e != nil {
		return l, e
	}
	_, e = tx.Exec(ctx, `INSERT INTO qb.ledgers(id,user_id,name,currency,cutover_time) VALUES($1,$2,$3,'CNY',$4) ON CONFLICT(id) DO NOTHING`, r.ID, actor, r.Name, r.Cutover)
	if e != nil {
		return l, e
	}
	l, e = loadLedger(ctx, tx, actor, r.ID, "FOR UPDATE")
	if e != nil {
		return l, e
	}
	if l.Name != r.Name || !l.Cutover.Equal(r.Cutover) {
		return l, domain.Conflict("账本 ID 已被其他内容使用")
	}
	for _, a := range [][3]string{{"expense.general", "生活消费", "EXPENSE"}, {"income.general", "收入", "INCOME"}, {"equity.opening", "期初权益", "EQUITY"}, {"suspense.asset", "待查扣款", "ASSET"}, {"suspense.liability", "待查入款", "LIABILITY"}, {"clearing.transfer", "转账清算", "ASSET"}, {"receivable.reimbursement", "报销应收", "ASSET"}, {"asset.fixed", "固定资产原值", "ASSET"}} {
		_, e = tx.Exec(ctx, `INSERT INTO qb.accounts(id,ledger_id,code,name,type,cash,initialized,system) VALUES($1,$2,$3,$4,$5,false,true,true) ON CONFLICT(ledger_id,code) DO NOTHING`, domain.ID(), l.ID, a[0], a[1], a[2])
		if e != nil {
			return l, e
		}
	}
	return l, tx.Commit(ctx)
}

type AccountRequest struct {
	CommandID string `json:"command_id"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	Cash      bool   `json:"cash"`
	Provider  string `json:"provider"`
	MaskedRef string `json:"masked_ref"`
}

func (d *DB) CreateAccount(ctx context.Context, actor, ledger string, r AccountRequest) (json.RawMessage, error) {
	if !textLimit(r.Name, 100) || (r.Type != "ASSET" && r.Type != "LIABILITY") || (r.Cash && r.Type != "ASSET") || len(r.Provider) > 100 || len(r.MaskedRef) > 32 {
		return nil, domain.Invalid("账户信息无效")
	}
	return command(ctx, d, actor, ledger, r.CommandID, r, "ACCOUNT_CREATED", func(tx pgx.Tx, l domain.Ledger) (any, error) {
		a := domain.Account{ID: domain.ID(), Name: r.Name, Type: r.Type, Cash: r.Cash, Provider: r.Provider, MaskedRef: r.MaskedRef}
		a.Code = "user." + a.ID
		_, e := tx.Exec(ctx, `INSERT INTO qb.accounts(id,ledger_id,code,name,type,cash,provider,masked_ref) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, a.ID, l.ID, a.Code, a.Name, a.Type, a.Cash, a.Provider, a.MaskedRef)
		if e != nil {
			return nil, e
		}
		external := domain.ID()
		_, e = tx.Exec(ctx, `INSERT INTO qb.external_accounts(id,ledger_id,ledger_account_id,provider,masked_ref,currency) VALUES($1,$2,$3,$4,$5,'CNY')`, external, l.ID, a.ID, a.Provider, a.MaskedRef)
		if e != nil {
			return nil, e
		}
		instrumentType := "BALANCE"
		if a.Type == "LIABILITY" {
			instrumentType = "CARD"
		}
		_, e = tx.Exec(ctx, `INSERT INTO qb.payment_instruments(id,ledger_id,external_account_id,type) VALUES($1,$2,$3,$4)`, domain.ID(), l.ID, external, instrumentType)
		return a, e
	})
}

type OpeningRequest struct {
	CommandID        string        `json:"command_id"`
	AccountID        string        `json:"account_id"`
	ExpectedRevision int64         `json:"expected_revision"`
	Amount           domain.Amount `json:"amount_minor"`
	AsOf             time.Time     `json:"as_of"`
	Meaning          string        `json:"meaning"`
}

func (d *DB) SetOpening(ctx context.Context, actor, ledger string, r OpeningRequest) (json.RawMessage, error) {
	return command(ctx, d, actor, ledger, r.CommandID, r, "OPENING_SET", func(tx pgx.Tx, l domain.Ledger) (any, error) {
		all, sys, e := accountsTx(ctx, tx, l.ID)
		if e != nil {
			return nil, e
		}
		a, ok := all[r.AccountID]
		if !ok || !(a.Type == "ASSET" || a.Type == "LIABILITY") || a.Code == "asset.fixed" || a.Code == "suspense.asset" || a.Code == "suspense.liability" || a.Code == "clearing.transfer" || a.Code == "receivable.reimbursement" {
			return nil, domain.Invalid("期初账户无效")
		}
		if a.Initialized || a.Revision != r.ExpectedRevision {
			return nil, domain.Conflict("账户已初始化或版本已改变")
		}
		if !r.AsOf.Equal(l.Cutover) || r.Amount < 0 || r.Amount > 9000000000000000 || (a.Type == "ASSET" && r.Meaning != "BALANCE") || (a.Type == "LIABILITY" && r.Meaning != "DEBT") {
			return nil, domain.Invalid("期初金额、口径或时间无效")
		}
		if r.Amount > 0 {
			entries := []domain.Entry{{AccountID: a.ID, Debit: r.Amount}, {AccountID: sys["equity.opening"], Credit: r.Amount}}
			if a.Type == "LIABILITY" {
				entries[0].Debit = 0
				entries[0].Credit = r.Amount
				entries[1].Credit = 0
				entries[1].Debit = r.Amount
			}
			var journal *string
			e = tx.QueryRow(ctx, `SELECT qb.post($1,$2,$3,NULL,$4,$5,$6)::text`, l.ID, a.ID, "OPENING:"+a.ID, domain.Hashable(entries), l.Cutover, l.Version+1).Scan(&journal)
			if e != nil {
				return nil, e
			}
		}
		_, e = tx.Exec(ctx, `UPDATE qb.accounts SET initialized=true,revision=revision+1 WHERE ledger_id=$1 AND id=$2`, l.ID, a.ID)
		a.Initialized = true
		a.Revision++
		return a, e
	})
}
func (d *DB) Accounts(ctx context.Context, actor, ledger string) ([]domain.Account, error) {
	if e := d.Authorized(ctx, actor, ledger); e != nil {
		return nil, e
	}
	rows, e := d.Pool.Query(ctx, `SELECT a.id::text,a.code,a.name,a.type,a.cash,a.initialized,a.revision,a.provider,a.masked_ref,COALESCE(sum(CASE WHEN a.type IN ('LIABILITY','INCOME','EQUITY') THEN e.credit-e.debit ELSE e.debit-e.credit END),0)::text FROM qb.accounts a LEFT JOIN qb.entries e ON e.ledger_id=a.ledger_id AND e.account_id=a.id WHERE a.ledger_id=$1 GROUP BY a.id ORDER BY a.code`, ledger)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	result := []domain.Account{}
	for rows.Next() {
		var a domain.Account
		var balance string
		if e = rows.Scan(&a.ID, &a.Code, &a.Name, &a.Type, &a.Cash, &a.Initialized, &a.Revision, &a.Provider, &a.MaskedRef, &balance); e != nil {
			return nil, e
		}
		n, err := strconv.ParseInt(balance, 10, 64)
		if err != nil {
			return nil, err
		}
		a.Balance = domain.Amount(n)
		result = append(result, a)
	}
	return result, rows.Err()
}
