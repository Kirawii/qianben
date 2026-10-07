package accounting

import (
	"errors"
	"github.com/Kirawii/qianben/backend/internal/domain"
	"math"
	"testing"
	"time"
)

func TestPostingScenarios(t *testing.T) {
	bank := domain.Account{ID: domain.ID(), Type: "ASSET", Cash: true, Initialized: true}
	card := domain.Account{ID: domain.ID(), Type: "LIABILITY", Initialized: true}
	sys := map[string]string{}
	for _, k := range []string{"expense.general", "income.general", "suspense.asset", "suspense.liability", "clearing.transfer", "receivable.reimbursement", "asset.fixed"} {
		sys[k] = domain.ID()
	}
	cut := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	now := cut.Add(time.Hour)
	for _, kind := range []string{"EXPENSE", "INCOME", "CASH_OUT", "CASH_IN", "TRANSFER_OUT", "TRANSFER_IN", "ADVANCE", "REIMBURSEMENT", "ASSET_PURCHASE", "REFUND", "ASSET_REFUND"} {
		t.Run(kind, func(t *testing.T) {
			f := domain.Facts{Kind: kind, Amount: 10001, Currency: "CNY", OccurredAt: &now, FundingAccount: bank.ID, AssetTitle: "电脑", OriginalEvent: domain.ID()}
			p, e := Plan(domain.Ledger{Cutover: cut}, f, map[string]domain.Account{bank.ID: bank}, sys)
			if e != nil {
				t.Fatal(e)
			}
			if p.Entries[0].Debit != 10001 || p.Entries[1].Credit != 10001 {
				t.Fatal("amount lost")
			}
		})
	}
	f := domain.Facts{Kind: "EXPENSE", Amount: 3000, Currency: "CNY", OccurredAt: &now, FundingAccount: card.ID}
	p, e := Plan(domain.Ledger{Cutover: cut}, f, map[string]domain.Account{card.ID: card}, sys)
	if e != nil || p.Entries[1].AccountID != card.ID {
		t.Fatal("credit purchase must increase card debt", e)
	}
}
func TestBlocksAndArithmetic(t *testing.T) {
	bank := domain.Account{ID: domain.ID(), Type: "ASSET", Initialized: true}
	cut := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	date := cut.Add(-12 * time.Hour)
	f := domain.Facts{Kind: "EXPENSE", Amount: 1, Currency: "CNY", OccurredAt: &date, TimePrecision: "DAY", FundingAccount: bank.ID}
	_, e := Plan(domain.Ledger{Cutover: cut}, f, map[string]domain.Account{bank.ID: bank}, nil)
	var b *Blocked
	if !errors.As(e, &b) || b.Reason != "CUTOVER_TIME_AMBIGUOUS" {
		t.Fatal(e)
	}
	if _, e := domain.Add(domain.Amount(math.MaxInt64), 1); e == nil {
		t.Fatal("overflow accepted")
	}
	f.Currency = "USD"
	_, e = Plan(domain.Ledger{Cutover: cut}, f, nil, nil)
	if !errors.As(e, &b) || b.Reason != "CNY_SETTLEMENT_REQUIRED" {
		t.Fatal(e)
	}
}
