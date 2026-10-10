package store

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/Kirawii/qianben/backend/internal/domain"
)

func TestHistoricalAnalysisDoesNotRepost(t *testing.T) {
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
	if _, err = admin.Pool.Exec(ctx, `INSERT INTO qb.users(id,name) VALUES($1,'historical analysis fixture')`, actor); err != nil {
		t.Fatal(err)
	}
	cut := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)
	l, err := db.CreateLedger(ctx, actor, CreateLedgerRequest{domain.ID(), "历史分析测试", cut})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if e := db.DeleteLedger(ctx, actor, l.ID, l.Name); e != nil {
			t.Error(e)
		}
	}()
	raw, err := db.CreateAccount(ctx, actor, l.ID, AccountRequest{CommandID: domain.ID(), Name: "银行", Type: "ASSET", Cash: true})
	if err != nil {
		t.Fatal(err)
	}
	var a domain.Account
	if err = json.Unmarshal(raw, &a); err != nil {
		t.Fatal(err)
	}
	if _, err = db.SetOpening(ctx, actor, l.ID, OpeningRequest{CommandID: domain.ID(), AccountID: a.ID, Amount: 10000, AsOf: cut, Meaning: "BALANCE"}); err != nil {
		t.Fatal(err)
	}
	at := cut.Add(-time.Hour)
	csv := CSVRequest{CommandID: domain.ID(), AccountID: a.ID, Content: "record_id,occurred_at,amount_minor,kind,merchant,category\nh1," + at.Format(time.RFC3339) + ",1234,EXPENSE,食堂,餐饮\nh2," + cut.Format(time.RFC3339) + ",200,TRANSFER_OUT,划转,\nh3," + cut.Add(time.Minute).Format(time.RFC3339) + ",100,EXPENSE,早餐,餐饮\n"}
	if _, err = db.ImportCSV(ctx, actor, l.ID, csv); err != nil {
		t.Fatal(err)
	}
	csv.CommandID = domain.ID()
	if _, err = db.ImportCSV(ctx, actor, l.ID, csv); err != nil {
		t.Fatal(err)
	}
	r, err := db.HistoricalAnalysis(ctx, actor, l.ID, at, cut.Add(time.Hour))
	if err != nil || r.GrossConsumption != 1234 || r.IncludedCount != 1 || r.ExcludedCount != 1 || len(r.Excluded) != 1 || r.Excluded[0].Amount != 200 {
		t.Fatal(r, err)
	}
	formal, err := db.Reports(ctx, actor, l.ID, at, cut.Add(time.Hour))
	if err != nil || formal.Consumption != 100 || formal.Assets != 9900 {
		t.Fatal("historical records changed balances", formal, err)
	}
	end, err := db.HistoricalAnalysis(ctx, actor, l.ID, at, cut)
	if err != nil || end.ExcludedCount != 0 {
		t.Fatal("exclusive end", end, err)
	}
	events, err := db.Events(ctx, actor, l.ID)
	if err != nil {
		t.Fatal(err)
	}
	var historical domain.Event
	for _, e := range events {
		if e.Facts.Amount == 1234 {
			historical = e
		}
	}
	historical.Facts.Category = "食品"
	if _, err = db.SaveEvent(ctx, actor, l.ID, EventRequest{CommandID: domain.ID(), EventID: historical.ID, ExpectedRevision: historical.Revision, Facts: historical.Facts}); err != nil {
		t.Fatal(err)
	}
	r, err = db.HistoricalAnalysis(ctx, actor, l.ID, at, cut.Add(time.Hour))
	if err != nil || r.GrossConsumption != 1234 || r.Categories[0].Category != "食品" {
		t.Fatal("latest category", r, err)
	}
	refundAt := at.Add(30 * time.Minute)
	refund := domain.Facts{Kind: "REFUND", Amount: 234, Currency: "CNY", OccurredAt: &refundAt, TimePrecision: "EXACT", FundingAccount: a.ID, OriginalEvent: historical.ID}
	if _, err = db.SaveEvent(ctx, actor, l.ID, EventRequest{CommandID: domain.ID(), EventID: domain.ID(), Facts: refund}); err != nil {
		t.Fatal(err)
	}
	r, err = db.HistoricalAnalysis(ctx, actor, l.ID, at, cut.Add(time.Hour))
	if err != nil || r.GrossConsumption != 1234 || r.Refund != 234 || r.NetConsumption != 1000 || r.IncludedCount != 2 || r.Categories[0].Amount != 1000 {
		t.Fatal("historical refund", r, err)
	}
	refundPeriod, e := db.HistoricalAnalysis(ctx, actor, l.ID, refundAt, cut)
	if e != nil || refundPeriod.GrossConsumption != 0 || refundPeriod.Refund != 234 || refundPeriod.NetConsumption != -234 || refundPeriod.Categories[0].Amount != -234 {
		t.Fatal("refund occurrence interval", refundPeriod, e)
	}
	refund.Amount = 1001
	if _, err = db.SaveEvent(ctx, actor, l.ID, EventRequest{CommandID: domain.ID(), EventID: domain.ID(), Facts: refund}); err == nil {
		t.Fatal("cumulative historical refund exceeded original")
	}
	historical.Facts.Amount = 100
	if _, err = db.SaveEvent(ctx, actor, l.ID, EventRequest{CommandID: domain.ID(), EventID: historical.ID, ExpectedRevision: historical.Revision + 1, Facts: historical.Facts}); err == nil {
		t.Fatal("linked historical original changed")
	}
	formal, err = db.Reports(ctx, actor, l.ID, at, cut.Add(time.Hour))
	if err != nil || formal.Consumption != 100 || formal.Assets != 9900 {
		t.Fatal("historical refund reposted", formal, err)
	}
	refund.Amount = 100
	later := cut.Add(2 * time.Minute)
	refund.OccurredAt = &later
	unproven := refund
	unproven.OriginalEvent = ""
	unproven.HistoricalOriginal = true
	pendingRaw, e := db.SaveEvent(ctx, actor, l.ID, EventRequest{CommandID: domain.ID(), EventID: domain.ID(), Facts: unproven})
	var pending domain.Event
	if e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(pendingRaw, &pending); e != nil || pending.Posted || pending.Reason != "REFUND_ORIGINAL_REQUIRED" {
		t.Fatal("checkbox established refund proof", pending, e)
	}
	if _, err = db.SaveEvent(ctx, actor, l.ID, EventRequest{CommandID: domain.ID(), EventID: domain.ID(), Facts: refund}); err != nil {
		t.Fatal("post-cutover refund with historical proof", err)
	}
	formal, err = db.Reports(ctx, actor, l.ID, cut, cut.Add(time.Hour))
	if err != nil || formal.Consumption != 0 || formal.Assets != 10000 {
		t.Fatal("refund did not use occurrence period", formal, err)
	}
	if _, err = db.HistoricalAnalysis(ctx, domain.ID(), l.ID, at, cut); err == nil {
		t.Fatal("cross-user analysis")
	}
	if _, err = db.HistoricalAnalysis(ctx, actor, l.ID, cut, at); err == nil {
		t.Fatal("invalid range")
	}
}
