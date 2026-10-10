package ondevice

import (
	"encoding/json"
	"github.com/Kirawii/qianben/backend/internal/domain"
	"testing"
	"time"
)

func TestServerFreeLedgerReplayRevisionAndRestart(t *testing.T) {
	saved := ""
	call := func(op, ledger, command string, body any) Result {
		input, _ := json.Marshal(Request{Operation: op, Ledger: ledger, Command: command, Body: domain.Hashable(body)})
		var result Result
		if e := json.Unmarshal([]byte(Invoke(saved, string(input))), &result); e != nil {
			t.Fatal(e)
		}
		if result.Status == 200 {
			saved = string(result.State)
		}
		return result
	}
	ok := func(r Result) json.RawMessage {
		if r.Status != 200 {
			t.Fatal(r.Error)
		}
		return r.Response
	}
	cut := time.Now().UTC().Truncate(time.Second).Add(-24 * time.Hour)
	ledger := domain.ID()
	ok(call("create-ledger", "", "", map[string]any{"id": ledger, "name": "独立账本", "cutover_time": cut}))
	var bank domain.Account
	json.Unmarshal(ok(call("create-account", ledger, "account-command", map[string]any{"name": "银行卡", "type": "ASSET", "cash": true})), &bank)
	event := domain.ID()
	at := cut.Add(time.Hour)
	facts := domain.Facts{Kind: "EXPENSE", Amount: 1234, Currency: "CNY", OccurredAt: &at, TimePrecision: "EXACT", FundingAccount: bank.ID, Category: "餐饮"}
	body := map[string]any{"event_id": event, "expected_revision": 0, "facts": facts}
	var ev domain.Event
	json.Unmarshal(ok(call("save-event", ledger, "expense-command", body)), &ev)
	if ev.Posted || ev.Reason != "ACCOUNT_NOT_INITIALIZED" {
		t.Fatal(ev)
	}
	opening := map[string]any{"account_id": bank.ID, "amount_minor": "10000", "as_of": cut, "meaning": "BALANCE", "expected_revision": 0}
	ok(call("opening", ledger, "opening-command", opening))
	var state State
	json.Unmarshal([]byte(saved), &state)
	book := state.Ledgers[ledger]
	if !book.Events[event].Posted || book.Events[event].Revision != 2 {
		t.Fatal("opening did not activate the blocked transaction")
	}
	assertBank := func(expected domain.Amount) {
		t.Helper()
		var accounts []domain.Account
		json.Unmarshal(ok(call("accounts", ledger, "", nil)), &accounts)
		for _, a := range accounts {
			if a.ID == bank.ID {
				if a.Balance != expected {
					t.Fatalf("balance=%d want=%d", a.Balance, expected)
				}
				return
			}
		}
		t.Fatal("bank missing")
	}
	assertBank(8766)
	previous := saved
	ok(call("opening", ledger, "opening-command", opening))
	if saved != previous {
		t.Fatal("receipt retry changed state")
	}
	// A fresh Invoke always decodes persistent state; no server or process-owned
	// memory is necessary to reopen the ledger and keep receipts.
	facts.Category = "咖啡"
	json.Unmarshal(ok(call("save-event", ledger, "category-command", map[string]any{"event_id": event, "expected_revision": 2, "facts": facts})), &ev)
	json.Unmarshal([]byte(saved), &state)
	if len(state.Ledgers[ledger].Journals) != 2 {
		t.Fatal("display classification rewrote accounting")
	}
	facts.Amount = 1500
	ok(call("save-event", ledger, "amount-command", map[string]any{"event_id": event, "expected_revision": 3, "facts": facts}))
	assertBank(8500)
	json.Unmarshal([]byte(saved), &state)
	if len(state.Ledgers[ledger].Journals) != 4 {
		t.Fatal("correction must retain original, reversal and replacement")
	}
	for _, j := range state.Ledgers[ledger].Journals {
		var dr, cr domain.Amount
		for _, line := range j.Entries {
			dr += line.Debit
			cr += line.Credit
		}
		if dr != cr {
			t.Fatal("unbalanced", j)
		}
	}
	previous = saved
	r := call("save-event", ledger, "stale-command", body)
	if r.Status != 409 || len(r.State) != 0 || saved != previous {
		t.Fatal("stale write was not rejected atomically", r)
	}
	opening["amount_minor"] = "20000"
	if r = call("opening", ledger, "opening-command", opening); r.Status != 409 || len(r.State) != 0 {
		t.Fatal("changed command replay accepted", r)
	}
	assertBank(8500)
}

func TestUnknownAndHistoricalNeverChangeBalance(t *testing.T) {
	s := State{Version: 1, Ledgers: map[string]*Book{}}
	cut := time.Now().UTC().Add(-time.Hour)
	ledger := domain.ID()
	s.apply(Request{Operation: "create-ledger", Body: domain.Hashable(map[string]any{"id": ledger, "name": "历史和待查", "cutover_time": cut})})
	b := s.Ledgers[ledger]
	bank := domain.ID()
	b.Accounts[bank] = domain.Account{ID: bank, Code: "user." + bank, Type: "ASSET", Cash: true, Initialized: true}
	at := cut.Add(time.Minute)
	f := domain.Facts{Kind: "CASH_OUT", Amount: 1000, Currency: "CNY", OccurredAt: &at, TimePrecision: "EXACT", FundingAccount: bank}
	ev, e := b.save(domain.ID(), 0, f)
	if e != nil || !ev.Posted || ev.Reason != "PURPOSE_REQUIRED" {
		t.Fatal(ev, e)
	}
	f.Currency = "UNKNOWN"
	ev, e = b.save(domain.ID(), 0, f)
	if e != nil || ev.Posted {
		t.Fatal(ev, e)
	}
	earlier := cut.Add(-time.Minute)
	f.Currency = "CNY"
	f.OccurredAt = &earlier
	ev, e = b.save(domain.ID(), 0, f)
	if e != nil || ev.Posted || ev.Status != "HISTORICAL_ONLY" {
		t.Fatal(ev, e)
	}
	if len(b.Journals) != 1 {
		t.Fatal("unconfirmed or historical changed formal ledger")
	}
}

func TestMalformedStateAndUnportedRelationDoNotPublishState(t *testing.T) {
	for _, saved := range []string{`{"version":9}`, `{broken`} {
		var r Result
		json.Unmarshal([]byte(Invoke(saved, `{"operation":"ledgers"}`)), &r)
		if r.Status == 200 || len(r.State) > 0 {
			t.Fatal("invalid state accepted", r)
		}
	}
	s := State{Ledgers: map[string]*Book{}}
	ledger := domain.ID()
	s.apply(Request{Operation: "create-ledger", Body: domain.Hashable(map[string]any{"id": ledger, "name": "退款", "cutover_time": time.Now().Add(-time.Hour)})})
	saved, _ := json.Marshal(s)
	req := Request{Operation: "save-event", Ledger: ledger, Command: "refund-command", Body: domain.Hashable(map[string]any{"event_id": domain.ID(), "facts": domain.Facts{Kind: "REFUND", Amount: 1000}})}
	input, _ := json.Marshal(req)
	var r Result
	json.Unmarshal([]byte(Invoke(string(saved), string(input))), &r)
	if r.Status == 200 || len(r.State) > 0 {
		t.Fatal("unported refund pretended to work", r)
	}
}
