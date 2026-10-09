package store

import (
	"context"
	"encoding/json"
	"github.com/Kirawii/qianben/backend/internal/domain"
	"os"
	"testing"
	"time"
)

func TestForeignSettlementLifecycle(t *testing.T) {
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("real PostgreSQL required")
	}
	ctx := context.Background()
	admin, err := Open(ctx, os.Getenv("TEST_ADMIN_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Pool.Close()
	db, err := Open(ctx, os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Pool.Close()
	actor := domain.ID()
	if _, err = admin.Pool.Exec(ctx, `INSERT INTO qb.users(id,name) VALUES($1,'foreign settlement fixture')`, actor); err != nil {
		t.Fatal(err)
	}
	cut := time.Now().UTC().Truncate(time.Second).Add(-48 * time.Hour)
	l, err := db.CreateLedger(ctx, actor, CreateLedgerRequest{domain.ID(), "外币结算测试", cut})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if e := db.DeleteLedger(ctx, actor, l.ID, l.Name); e != nil {
			t.Error(e)
		}
	}()
	at := cut.Add(time.Hour)
	f := domain.Facts{Kind: "EXPENSE", Currency: "CNY", TimePrecision: "UNKNOWN", Foreign: &domain.ForeignSettlement{OriginalAmount: "10", OriginalCurrency: "USD", ReferenceRate: "7.2", ReferenceSource: "用户提供的参考值", ReferenceAt: &at}}
	id := domain.ID()
	save := func(facts domain.Facts, rev int64) domain.Event {
		r, e := db.SaveEvent(ctx, actor, l.ID, EventRequest{CommandID: domain.ID(), EventID: id, ExpectedRevision: rev, Facts: facts})
		if e != nil {
			t.Fatal(e)
		}
		var ev domain.Event
		if e = json.Unmarshal(r, &ev); e != nil {
			t.Fatal(e)
		}
		return ev
	}
	pending := save(f, 0)
	if pending.Posted || pending.Reason != "CNY_SETTLEMENT_REQUIRED" || pending.Facts.Foreign.OriginalAmount != "10" || pending.Facts.Currency != "UNKNOWN" || pending.Facts.OccurredAt != nil || pending.Facts.FundingAccount != "" {
		t.Fatal("reference rate produced a posting", pending)
	}
	report, err := db.Reports(ctx, actor, l.ID, cut, at.Add(time.Hour))
	if err != nil || report.Consumption != 0 || report.CashChange != 0 || report.Assets != 0 {
		t.Fatal("pending foreign affected balances", report, err)
	}
	raw, err := db.CreateAccount(ctx, actor, l.ID, AccountRequest{CommandID: domain.ID(), Name: "CNY 银行卡", Type: "ASSET", Cash: true})
	if err != nil {
		t.Fatal(err)
	}
	var bank domain.Account
	json.Unmarshal(raw, &bank)
	if _, err = db.SetOpening(ctx, actor, l.ID, OpeningRequest{CommandID: domain.ID(), AccountID: bank.ID, Amount: 100000, AsOf: cut, Meaning: "BALANCE"}); err != nil {
		t.Fatal(err)
	}
	f.Amount = 7300
	f.OccurredAt = &at
	f.TimePrecision = "EXACT"
	f.Currency = "CNY"
	f.FundingAccount = bank.ID
	f.Foreign.CNYConfirmed = true
	settled := save(f, pending.Revision)
	if !settled.Posted || settled.Facts.Foreign.DerivedRate != "7.300000000000" || settled.Facts.Foreign.RateSource != "SETTLEMENT_DERIVED_USER_CONFIRMED" {
		t.Fatal("actual settlement not accepted", settled)
	}
	report, err = db.Reports(ctx, actor, l.ID, cut, at.Add(time.Hour))
	if err != nil || report.Consumption != 7300 || report.CashChange != -7300 || report.Assets != 92700 {
		t.Fatal("CNY settlement used original amount or reference rate", report, err)
	}
	var journalsBefore, journalsAfter int
	if err = admin.Pool.QueryRow(ctx, `SELECT count(*) FROM qb.journals WHERE ledger_id=$1`, l.ID).Scan(&journalsBefore); err != nil {
		t.Fatal(err)
	}
	updatedFacts := settled.Facts
	copyForeign := *updatedFacts.Foreign
	updatedFacts.Foreign = &copyForeign
	updatedFacts.Foreign.OriginalAmount = "20"
	revised := save(updatedFacts, settled.Revision)
	if revised.Facts.Foreign.DerivedRate != "3.650000000000" {
		t.Fatal("original evidence correction did not recalculate rate", revised)
	}
	if err = admin.Pool.QueryRow(ctx, `SELECT count(*) FROM qb.journals WHERE ledger_id=$1`, l.ID).Scan(&journalsAfter); err != nil || journalsBefore != journalsAfter {
		t.Fatal("original metadata correction re-posted CNY", journalsBefore, journalsAfter, err)
	}
	revised.Facts.Foreign.CNYConfirmed = false
	revised.Facts.Amount = 0
	suspended := save(revised.Facts, revised.Revision)
	if suspended.Posted || suspended.Reason != "CNY_SETTLEMENT_REQUIRED" {
		t.Fatal("revoked settlement kept posting", suspended)
	}
	report, err = db.Reports(ctx, actor, l.ID, cut, at.Add(time.Hour))
	if err != nil || report.Consumption != 0 || report.CashChange != 0 || report.Assets != 100000 {
		t.Fatal("settlement revocation did not reverse atomically", report, err)
	}
	history, err := db.History(ctx, actor, l.ID, id)
	if err != nil || len(history) != 4 {
		t.Fatal("foreign audit history", len(history), err)
	}
	if _, err = db.History(ctx, domain.ID(), l.ID, id); err == nil {
		t.Fatal("cross-user foreign history")
	}
}
