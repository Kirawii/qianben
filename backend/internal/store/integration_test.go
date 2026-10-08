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
	if r.Assets != 100000 || r.NetWorth != 100000 || r.Liabilities != 0 {
		t.Fatalf("suspense balance sheet: %+v", r)
	}
	if r.ConfirmedAssets != 97000 || r.ConfirmedNetWorth != 97000 || r.ProvisionalNetWorth != 3000 {
		t.Fatalf("suspense must not inflate confirmed assets: %+v", r)
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
	if r.Assets != 98000 || r.NetWorth != 98000 || r.Surplus != -2000 || len(r.Categories) != 1 || r.Categories[0].Amount != 2000 {
		t.Fatalf("refund financial summary %+v", r)
	}
	// A refund-only reporting period must retain the full balance sheet and show negative consumption.
	period, e := db.Reports(ctx, actor, ledger.ID, refundAt, refundAt.Add(time.Hour))
	if e != nil || period.NetWorth != 98000 || period.Consumption != -1000 {
		t.Fatalf("period summary %+v %v", period, e)
	}
	incomeAt := refundAt.Add(2 * time.Hour)
	_, e = db.SaveEvent(ctx, actor, ledger.ID, EventRequest{CommandID: domain.ID(), EventID: domain.ID(), Facts: domain.Facts{Kind: "INCOME", Amount: 5000, Currency: "CNY", OccurredAt: &incomeAt, FundingAccount: account.ID}})
	if e != nil {
		t.Fatal(e)
	}
	r, e = db.Reports(ctx, actor, ledger.ID, cutover, incomeAt.Add(time.Hour))
	if e != nil || r.Income != 5000 || r.Surplus != 3000 || r.NetWorth != 103000 {
		t.Fatalf("income summary %+v %v", r, e)
	}
	// Rule revocation preserves accepted facts and remains idempotent on retry.
	req.CommandID = domain.ID()
	req.ExpectedRevision = 2
	req.Facts.Merchant = "测试商户"
	req.Facts.Category = "餐饮"
	req.LearnCategory = true
	if _, e = db.SaveEvent(ctx, actor, ledger.ID, req); e != nil {
		t.Fatal(e)
	}
	rules, e := db.Rules(ctx, actor, ledger.ID)
	if e != nil || len(rules) != 1 || !rules[0].Active {
		t.Fatalf("rules %v %v", rules, e)
	}
	revoke := RevokeRuleRequest{CommandID: domain.ID(), RuleID: rules[0].ID, ExpectedVersion: rules[0].Version}
	if _, e = db.RevokeRule(ctx, domain.ID(), ledger.ID, revoke); e == nil {
		t.Fatal("cross tenant revoke accepted")
	}
	if _, e = db.RevokeRule(ctx, actor, ledger.ID, revoke); e != nil {
		t.Fatal(e)
	}
	if _, e = db.RevokeRule(ctx, actor, ledger.ID, revoke); e != nil {
		t.Fatal("revoke retry", e)
	}
	rules, e = db.Rules(ctx, actor, ledger.ID)
	if e != nil || rules[0].Active {
		t.Fatal("rule remains active", e)
	}
	r, e = db.Reports(ctx, actor, ledger.ID, cutover, incomeAt.Add(time.Hour))
	if e != nil || r.NetWorth != 103000 || r.Consumption != 2000 {
		t.Fatalf("revoke changed history %+v %v", r, e)
	}
	checkReq := CheckRequest{CommandID: domain.ID(), AccountID: account.ID, AsOf: cutover, Actual: 99900}
	if _, e = db.CheckBalance(ctx, domain.ID(), ledger.ID, checkReq); e == nil {
		t.Fatal("cross tenant check accepted")
	}
	checkRaw, e := db.CheckBalance(ctx, actor, ledger.ID, checkReq)
	if e != nil {
		t.Fatal(e)
	}
	var check BalanceCheck
	if e = json.Unmarshal(checkRaw, &check); e != nil || check.Book != 100000 || check.Difference != -100 {
		t.Fatalf("historical balance check %+v %v", check, e)
	}
	if _, e = db.CheckBalance(ctx, actor, ledger.ID, checkReq); e != nil {
		t.Fatal("balance check retry", e)
	}
	checks, e := db.BalanceChecks(ctx, actor, ledger.ID)
	if e != nil || len(checks) != 1 {
		t.Fatal("duplicate balance check", checks, e)
	}
	checkReq.CommandID = domain.ID()
	checkReq.AsOf = cutover.Add(-time.Second)
	if _, e = db.CheckBalance(ctx, actor, ledger.ID, checkReq); e == nil {
		t.Fatal("pre-cutover check accepted")
	}
	r, e = db.Reports(ctx, actor, ledger.ID, cutover, incomeAt.Add(time.Hour))
	if e != nil || r.NetWorth != 103000 {
		t.Fatal("check adjusted finances", r, e)
	}
	history, e := db.History(ctx, actor, ledger.ID, req.EventID)
	if e != nil || len(history) != 3 {
		t.Fatal("revision history", len(history), e)
	}
	var latest struct {
		Revision int64             `json:"revision"`
		Journals []json.RawMessage `json:"journals"`
	}
	if e = json.Unmarshal(history[0], &latest); e != nil || latest.Revision != 3 || len(latest.Journals) != 0 {
		t.Fatal("descriptive revision must not repost", latest, e)
	}
	var dining domain.Amount
	for _, category := range r.Categories {
		if category.Category == "餐饮" {
			dining = category.Amount
		}
	}
	if dining != 3000 {
		t.Fatal("classification-only revision did not update report", r.Categories)
	}
	var oldest struct {
		Journals []struct {
			ReversedBy *string `json:"reversed_by"`
		} `json:"journals"`
	}
	if e = json.Unmarshal(history[2], &oldest); e != nil || len(oldest.Journals) != 1 || oldest.Journals[0].ReversedBy == nil {
		t.Fatal("history missing reversal link", oldest, e)
	}
	if _, e = db.History(ctx, domain.ID(), ledger.ID, req.EventID); e == nil {
		t.Fatal("cross tenant history accepted")
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
	if e != nil || count != 12 || sum != "0" {
		t.Fatal("journal invariants", count, sum, e)
	}
	// Allocating partial and batched reimbursements links existing journals only.
	create := func(kind string, amount domain.Amount, at time.Time) domain.Event {
		t.Helper()
		b, err := db.SaveEvent(ctx, actor, ledger.ID, EventRequest{CommandID: domain.ID(), EventID: domain.ID(), Facts: domain.Facts{Kind: kind, Amount: amount, Currency: "CNY", OccurredAt: &at, FundingAccount: account.ID}})
		if err != nil {
			t.Fatal(err)
		}
		var ev domain.Event
		if err = json.Unmarshal(b, &ev); err != nil {
			t.Fatal(err)
		}
		return ev
	}
	a1 := create("ADVANCE", 2000, incomeAt.Add(time.Hour))
	a2 := create("ADVANCE", 3000, incomeAt.Add(2*time.Hour))
	b1 := create("REIMBURSEMENT", 4000, incomeAt.Add(3*time.Hour))
	b2 := create("REIMBURSEMENT", 1000, incomeAt.Add(4*time.Hour))
	allocation := ReimbursementRequest{CommandID: domain.ID(), ReimbursementID: b1.ID, ExpectedRevision: 1, Allocations: []ReimbursementAllocation{{a1.ID, 1, 1000}, {a2.ID, 1, 3000}}}
	accepted, err := db.AllocateReimbursement(ctx, actor, ledger.ID, allocation)
	if err != nil {
		t.Fatal(err)
	}
	retried, err := db.AllocateReimbursement(ctx, actor, ledger.ID, allocation)
	var acceptedResult, retriedResult any
	json.Unmarshal(accepted, &acceptedResult)
	json.Unmarshal(retried, &retriedResult)
	if err != nil || string(domain.Hashable(acceptedResult)) != string(domain.Hashable(retriedResult)) {
		t.Fatal("allocation retry", err)
	}
	second := ReimbursementRequest{CommandID: domain.ID(), ReimbursementID: b2.ID, ExpectedRevision: 1, Allocations: []ReimbursementAllocation{{a1.ID, 1, 1000}}}
	if _, err = db.AllocateReimbursement(ctx, actor, ledger.ID, second); err != nil {
		t.Fatal(err)
	}
	balances, err := db.ReimbursementBalances(ctx, actor, ledger.ID)
	if err != nil || len(balances) != 4 {
		t.Fatal("reimbursement balances", balances, err)
	}
	for _, raw := range balances {
		var balance struct {
			Remaining domain.Amount `json:"remaining_minor"`
			Allocated domain.Amount `json:"allocated_minor"`
			Amount    domain.Amount `json:"amount_minor"`
		}
		if err = json.Unmarshal(raw, &balance); err != nil || balance.Remaining != 0 || balance.Allocated != balance.Amount {
			t.Fatal("incorrect outstanding amount", balance, err)
		}
	}
	allocation.CommandID = domain.ID()
	allocation.Allocations[0].Amount = 2000
	if _, err = db.AllocateReimbursement(ctx, actor, ledger.ID, allocation); err == nil {
		t.Fatal("over-allocation accepted")
	}
	links, err := db.Relations(ctx, actor, ledger.ID, b1.ID)
	if err != nil || len(links) != 2 {
		t.Fatal("failed replacement changed prior allocations", links, err)
	}
	edit := EventRequest{CommandID: domain.ID(), EventID: b1.ID, ExpectedRevision: 1, Facts: b1.Facts}
	edit.Facts.Amount = 5000
	if _, err = db.SaveEvent(ctx, actor, ledger.ID, edit); err == nil {
		t.Fatal("linked reimbursement economic edit accepted")
	}
	allocation.CommandID = domain.ID()
	allocation.Allocations = nil
	if _, err = db.AllocateReimbursement(ctx, actor, ledger.ID, allocation); err != nil {
		t.Fatal(err)
	}
	links, err = db.Relations(ctx, actor, ledger.ID, b1.ID)
	if err != nil || len(links) != 0 {
		t.Fatal("allocation unlink", links, err)
	}
	e = admin.Pool.QueryRow(ctx, `SELECT count(*) FROM qb.entries WHERE ledger_id=$1`, ledger.ID).Scan(&count)
	if e != nil || count != 20 {
		t.Fatal("allocation posted duplicate journals", count, e)
	}
	r, e = db.Reports(ctx, actor, ledger.ID, cutover, incomeAt.Add(5*time.Hour))
	if e != nil || r.Income != 5000 || r.Consumption != 2000 || r.NetWorth != 103000 {
		t.Fatal("reimbursement treated as income or consumption", r, e)
	}
	lateRaw, e := db.CreateAccount(ctx, actor, ledger.ID, AccountRequest{CommandID: domain.ID(), Name: "晚补期初", Type: "ASSET", Cash: true})
	if e != nil {
		t.Fatal(e)
	}
	var late domain.Account
	json.Unmarshal(lateRaw, &late)
	newPending := func(kind string, at time.Time) EventRequest {
		t.Helper()
		request := EventRequest{CommandID: domain.ID(), EventID: domain.ID(), Facts: domain.Facts{Kind: kind, Amount: 1000, Currency: "CNY", OccurredAt: &at, FundingAccount: late.ID}}
		if _, err := db.SaveEvent(ctx, actor, ledger.ID, request); err != nil {
			t.Fatal(err)
		}
		return request
	}
	good := newPending("EXPENSE", incomeAt.Add(6*time.Hour))
	bad := newPending("UNSUPPORTED", incomeAt.Add(7*time.Hour))
	historical := newPending("EXPENSE", cutover)
	opening := OpeningRequest{CommandID: domain.ID(), AccountID: late.ID, Amount: 5000, AsOf: cutover, Meaning: "BALANCE"}
	if _, err := db.SetOpening(ctx, actor, ledger.ID, opening); err == nil {
		t.Fatal("invalid activation batch accepted")
	}
	all, err := db.Accounts(ctx, actor, ledger.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range all {
		if a.ID == late.ID && (a.Initialized || a.Balance != 0) {
			t.Fatal("failed activation committed opening", a)
		}
	}
	bad.CommandID = domain.ID()
	bad.ExpectedRevision = 1
	bad.Facts.Kind = "EXPENSE"
	if _, err = db.SaveEvent(ctx, actor, ledger.ID, bad); err != nil {
		t.Fatal(err)
	}
	opening.CommandID = domain.ID()
	openingResult, err := db.SetOpening(ctx, actor, ledger.ID, opening)
	if err != nil {
		t.Fatal(err)
	}
	var openingAccount domain.Account
	if err = json.Unmarshal(openingResult, &openingAccount); err != nil || openingAccount.Balance != 3000 {
		t.Fatal("opening response did not include activated balance", openingAccount, err)
	}
	events, err := db.Events(ctx, actor, ledger.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range events {
		if (ev.ID == good.EventID || ev.ID == bad.EventID) && !ev.Posted {
			t.Fatal("post-cutover event not activated", ev)
		}
		if ev.ID == historical.EventID && (ev.Posted || ev.Status != "HISTORICAL_ONLY") {
			t.Fatal("opening double counted historical event", ev)
		}
	}
	all, err = db.Accounts(ctx, actor, ledger.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range all {
		if a.ID == late.ID && (!a.Initialized || a.Balance != 3000) {
			t.Fatal("activated balance", a)
		}
		if a.ID == late.ID && (a.BookAsOf == nil || !a.BookAsOf.Equal(incomeAt.Add(7*time.Hour)) || a.EvidenceReceivedAt == nil || a.LastBalanceCheckAt != nil) {
			t.Fatal("account evidence freshness", a)
		}
		if a.ID == account.ID && (a.LastBalanceCheckAt == nil || !a.LastBalanceCheckAt.Equal(cutover)) {
			t.Fatal("balance-check freshness", a)
		}
	}
	qualityRaw, err := db.Quality(ctx, actor, ledger.ID)
	if err != nil {
		t.Fatal(err)
	}
	var quality struct {
		Posted    int64  `json:"posted_count"`
		Automatic int64  `json:"automatic_posted_count"`
		Manual    int64  `json:"manual_evidence_count"`
		Review    int64  `json:"review_count"`
		AgeBasis  string `json:"age_basis"`
	}
	if err = json.Unmarshal(qualityRaw, &quality); err != nil || quality.Posted != 9 || quality.Automatic != 0 || quality.Manual != 10 || quality.Review != 0 || quality.AgeBasis != "event_created_at" {
		t.Fatal("quality counts", quality, err)
	}
	if _, err = db.Quality(ctx, domain.ID(), ledger.ID); err == nil {
		t.Fatal("cross-user quality read")
	}
}
