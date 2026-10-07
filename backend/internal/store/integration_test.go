package store

import (
	"context"
	"encoding/json"
	"github.com/Kirawii/qianben/backend/internal/domain"
	"os"
	"sync"
	"testing"
	"time"
)

func TestPostgresLifecycle(t *testing.T) {
	adminURL := os.Getenv("TEST_ADMIN_DATABASE_URL")
	appURL := os.Getenv("TEST_DATABASE_URL")
	if adminURL == "" || appURL == "" {
		t.Skip("set TEST_ADMIN_DATABASE_URL and TEST_DATABASE_URL for real PostgreSQL checks")
	}
	ctx := context.Background()
	admin, e := Open(ctx, adminURL)
	if e != nil {
		t.Fatal(e)
	}
	defer admin.Pool.Close()
	db, e := Open(ctx, appURL)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Pool.Close()
	actor := domain.ID()
	_, e = admin.Pool.Exec(ctx, `INSERT INTO qb.users(id,name) VALUES($1,'integration fixture')`, actor)
	if e != nil {
		t.Fatal(e)
	}
	cutover := time.Now().UTC().Truncate(time.Second).Add(-48 * time.Hour)
	ledger, e := db.CreateLedger(ctx, actor, CreateLedgerRequest{domain.ID(), "测试账本", cutover})
	if e != nil {
		t.Fatal(e)
	}
	defer func() {
		tx, err := db.Pool.Begin(ctx)
		if err != nil {
			t.Error(err)
			return
		}
		defer tx.Rollback(ctx)
		actorTx(ctx, tx, actor)
		if _, err = tx.Exec(ctx, `SELECT qb.purge_ledger($1)`, ledger.ID); err != nil {
			t.Error("purge:", err)
			return
		}
		if err = tx.Commit(ctx); err != nil {
			t.Error(err)
		}
	}()
	raw, e := db.CreateAccount(ctx, actor, ledger.ID, AccountRequest{CommandID: domain.ID(), Name: "银行卡", Type: "ASSET", Cash: true})
	if e != nil {
		t.Fatal(e)
	}
	var account domain.Account
	if e = json.Unmarshal(raw, &account); e != nil {
		t.Fatal(e)
	}
	_, e = db.SetOpening(ctx, actor, ledger.ID, OpeningRequest{CommandID: domain.ID(), AccountID: account.ID, Amount: 100000, AsOf: cutover, Meaning: "BALANCE"})
	if e != nil {
		t.Fatal(e)
	}
	at := cutover.Add(time.Hour)
	req := EventRequest{CommandID: domain.ID(), EventID: domain.ID(), Facts: domain.Facts{Kind: "CASH_OUT", Amount: 3000, Currency: "CNY", OccurredAt: &at, TimePrecision: "EXACT", FundingAccount: account.ID}}
	first, e := db.SaveEvent(ctx, actor, ledger.ID, req)
	if e != nil {
		t.Fatal(e)
	}
	again, e := db.SaveEvent(ctx, actor, ledger.ID, req)
	var firstEvent, againEvent domain.Event
	json.Unmarshal(first, &firstEvent)
	json.Unmarshal(again, &againEvent)
	if e != nil || string(domain.Hashable(firstEvent)) != string(domain.Hashable(againEvent)) {
		t.Fatal("retry changed result", e)
	}
	r, e := db.Reports(ctx, actor, ledger.ID, cutover, at.Add(time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	if r.UnresolvedBalance != 3000 || r.UnresolvedCash != -3000 || r.Consumption != 0 {
		t.Fatalf("suspense report: %+v", r)
	}
	req.ExpectedRevision = 1
	req.Facts.Kind = "EXPENSE"
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			copy := req
			copy.CommandID = domain.ID()
			_, err := db.SaveEvent(ctx, actor, ledger.ID, copy)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	success, conflicts := 0, 0
	for err := range errs {
		if err == nil {
			success++
		} else {
			if f, ok := err.(*domain.Fault); ok && f.Status == 409 {
				conflicts++
			} else {
				t.Fatal(err)
			}
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatal("concurrency", success, conflicts)
	}
	r, e = db.Reports(ctx, actor, ledger.ID, cutover, at.Add(time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	if r.UnresolvedBalance != 0 || r.UnresolvedCash != 0 || r.ExternalCash != -3000 || r.Expense != 3000 || r.CashChange != -3000 {
		t.Fatalf("corrected report: %+v", r)
	}
	refundAt := at.Add(24 * time.Hour)
	refund := EventRequest{CommandID: domain.ID(), EventID: domain.ID(), Facts: domain.Facts{Kind: "REFUND", Amount: 1000, Currency: "CNY", OccurredAt: &refundAt, FundingAccount: account.ID, OriginalEvent: req.EventID}}
	if _, e = db.SaveEvent(ctx, actor, ledger.ID, refund); e != nil {
		t.Fatal(e)
	}
	refund.CommandID = domain.ID()
	refund.EventID = domain.ID()
	refund.Facts.Amount = 2500
	if _, e = db.SaveEvent(ctx, actor, ledger.ID, refund); e == nil {
		t.Fatal("over refund accepted")
	}
	r, e = db.Reports(ctx, actor, ledger.ID, cutover, refundAt.Add(time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	if r.Consumption != 2000 || r.CashChange != -2000 {
		t.Fatalf("refund report %+v", r)
	}
	if _, e = db.Accounts(ctx, domain.ID(), ledger.ID); e == nil {
		t.Fatal("cross tenant read accepted")
	}
	if _, e = db.Pool.Exec(ctx, `INSERT INTO qb.journals DEFAULT VALUES`); e == nil {
		t.Fatal("app has direct journal writes")
	}
	var count int
	var sum string
	e = admin.Pool.QueryRow(ctx, `SELECT count(*),COALESCE(sum(debit-credit),0)::text FROM qb.entries WHERE ledger_id=$1`, ledger.ID).Scan(&count, &sum)
	if e != nil || count != 10 || sum != "0" {
		t.Fatal("journal invariants", count, sum, e)
	}
}
