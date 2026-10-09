package store

import (
	"context"
	"encoding/json"
	"github.com/Kirawii/qianben/backend/internal/domain"
	"os"
	"testing"
	"time"
)

func TestFundingTimelineAndUserProof(t *testing.T) {
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
	if _, err = admin.Pool.Exec(ctx, `INSERT INTO qb.users(id,name) VALUES($1,'funding fixture')`, actor); err != nil {
		t.Fatal(err)
	}
	cut := time.Now().UTC().Truncate(time.Second).Add(-48 * time.Hour)
	l, err := db.CreateLedger(ctx, actor, CreateLedgerRequest{domain.ID(), "资金来源测试", cut})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		tx, e := db.Pool.Begin(ctx)
		if e != nil {
			t.Error(e)
			return
		}
		defer tx.Rollback(ctx)
		if e = actorTx(ctx, tx, actor); e != nil {
			t.Error(e)
			return
		}
		if _, e = tx.Exec(ctx, `SELECT qb.purge_ledger($1)`, l.ID); e != nil {
			t.Error("funding purge", e)
			return
		}
		if e = tx.Commit(ctx); e != nil {
			t.Error(e)
		}
	}()
	makeAccount := func(name string) domain.Account {
		raw, e := db.CreateAccount(ctx, actor, l.ID, AccountRequest{CommandID: domain.ID(), Name: name, Type: "ASSET", Cash: true})
		if e != nil {
			t.Fatal(e)
		}
		var a domain.Account
		if e = json.Unmarshal(raw, &a); e != nil {
			t.Fatal(e)
		}
		if _, e = db.SetOpening(ctx, actor, l.ID, OpeningRequest{CommandID: domain.ID(), AccountID: a.ID, Amount: 100000, AsOf: cut, Meaning: "BALANCE"}); e != nil {
			t.Fatal(e)
		}
		return a
	}
	a, b := makeAccount("银行卡甲"), makeAccount("银行卡乙")
	read := func(event, rail string, at time.Time) struct {
		Version   int64 `json:"source_version"`
		Candidate *struct {
			Account string `json:"account_id"`
			Proves  bool   `json:"proves_actual_funding"`
		} `json:"candidate"`
		Proofs []struct {
			Kind     string `json:"kind"`
			Account  string `json:"account_id"`
			Revision int64  `json:"revision"`
			Current  bool   `json:"current_revision"`
		} `json:"proofs"`
	} {
		raw, e := db.Funding(ctx, actor, l.ID, event, rail, at)
		if e != nil {
			t.Fatal(e)
		}
		var out struct {
			Version   int64 `json:"source_version"`
			Candidate *struct {
				Account string `json:"account_id"`
				Proves  bool   `json:"proves_actual_funding"`
			} `json:"candidate"`
			Proofs []struct {
				Kind     string `json:"kind"`
				Account  string `json:"account_id"`
				Revision int64  `json:"revision"`
				Current  bool   `json:"current_revision"`
			} `json:"proofs"`
		}
		if e = json.Unmarshal(raw, &out); e != nil {
			t.Fatal(e)
		}
		return out
	}
	from := cut.Add(time.Hour)
	changed := from.Add(time.Hour)
	until := changed.Add(time.Hour)
	req := FundingPreferenceRequest{CommandID: domain.ID(), ExpectedVersion: read("", "", from).Version, Rail: "WECHAT", AccountID: a.ID, EffectiveFrom: from}
	if _, err = db.SetFundingPreference(ctx, actor, l.ID, req); err != nil {
		t.Fatal(err)
	}
	if _, err = db.SetFundingPreference(ctx, actor, l.ID, req); err != nil {
		t.Fatal("idempotent preference", err)
	}
	stale := req
	stale.CommandID = domain.ID()
	stale.EffectiveFrom = changed
	if _, err = db.SetFundingPreference(ctx, actor, l.ID, stale); err == nil {
		t.Fatal("stale preference accepted")
	}
	req = FundingPreferenceRequest{CommandID: domain.ID(), ExpectedVersion: read("", "", changed).Version, Rail: "WECHAT", AccountID: b.ID, EffectiveFrom: changed, EffectiveTo: &until}
	if _, err = db.SetFundingPreference(ctx, actor, l.ID, req); err != nil {
		t.Fatal(err)
	}
	if c := read("", "WECHAT", from).Candidate; c == nil || c.Account != a.ID || c.Proves {
		t.Fatal("old preference is not a candidate", c)
	}
	if c := read("", "WECHAT", changed).Candidate; c == nil || c.Account != b.ID || c.Proves {
		t.Fatal("changed preference", c)
	}
	if c := read("", "WECHAT", until).Candidate; c != nil {
		t.Fatal("expired preference revived old account", c)
	}
	req = FundingPreferenceRequest{CommandID: domain.ID(), ExpectedVersion: read("", "", until).Version, Rail: "WECHAT", EffectiveFrom: until}
	if _, err = db.SetFundingPreference(ctx, actor, l.ID, req); err != nil {
		t.Fatal(err)
	}
	if c := read("", "WECHAT", until.Add(time.Hour)).Candidate; c != nil {
		t.Fatal("cleared preference revived old account", c)
	}
	version := read("", "", until).Version
	bad := FundingPreferenceRequest{CommandID: domain.ID(), ExpectedVersion: version, Rail: "WECHAT", AccountID: domain.ID(), EffectiveFrom: until.Add(time.Hour)}
	if _, err = db.SetFundingPreference(ctx, actor, l.ID, bad); err == nil {
		t.Fatal("unknown account accepted as preference")
	}
	if read("", "", until).Version != version {
		t.Fatal("rejected preference changed version")
	}
	if _, err = db.SetFundingPreference(ctx, domain.ID(), l.ID, bad); err == nil {
		t.Fatal("cross-user preference write")
	}
	// Preferences alone do not fill the actual funding account or make a journal.
	at := from.Add(30 * time.Minute)
	facts := domain.Facts{Kind: "EXPENSE", Amount: 1000, Currency: "CNY", OccurredAt: &at, TimePrecision: "EXACT", Category: "餐饮"}
	id := domain.ID()
	raw, err := db.SaveEvent(ctx, actor, l.ID, EventRequest{CommandID: domain.ID(), EventID: id, Facts: facts})
	if err != nil {
		t.Fatal(err)
	}
	var ev domain.Event
	json.Unmarshal(raw, &ev)
	if ev.Posted || ev.Facts.FundingAccount != "" || len(read(id, "", at).Proofs) != 0 {
		t.Fatal("default preference used as proof", ev)
	}
	facts.FundingAccount = b.ID
	r := EventRequest{CommandID: domain.ID(), EventID: id, ExpectedRevision: ev.Revision, Facts: facts}
	raw, err = db.SaveEvent(ctx, actor, l.ID, r)
	if err != nil {
		t.Fatal(err)
	}
	json.Unmarshal(raw, &ev)
	if !ev.Posted {
		t.Fatal("explicit user funding not posted", ev)
	}
	if _, err = db.SaveEvent(ctx, actor, l.ID, r); err != nil {
		t.Fatal(err)
	}
	proofs := read(id, "", at).Proofs
	if len(proofs) != 1 || proofs[0].Kind != "USER_CONFIRMED" || proofs[0].Account != b.ID || !proofs[0].Current {
		t.Fatal("user proof", proofs)
	}
	facts.FundingAccount = a.ID
	raw, err = db.SaveEvent(ctx, actor, l.ID, EventRequest{CommandID: domain.ID(), EventID: id, ExpectedRevision: ev.Revision, Facts: facts})
	if err != nil {
		t.Fatal(err)
	}
	proofs = read(id, "", at).Proofs
	if len(proofs) != 2 || proofs[0].Account != a.ID || !proofs[0].Current || proofs[1].Current {
		t.Fatal("old proof overwritten", proofs)
	}
	if _, err = db.Funding(ctx, domain.ID(), l.ID, id, "", at); err == nil {
		t.Fatal("cross-user funding read")
	}
	if _, err = db.Pool.Exec(ctx, `UPDATE qb.funding_relations SET note='tampered' WHERE ledger_id=$1`, l.ID); err == nil {
		t.Fatal("funding history mutable")
	}
	if _, err = admin.Pool.Exec(ctx, `UPDATE qb.funding_relations SET note='tampered' WHERE ledger_id=$1`, l.ID); err == nil {
		t.Fatal("immutable trigger missing")
	}
}
