package store

import (
	"context"
	"encoding/json"
	"github.com/Kirawii/qianben/backend/internal/domain"
	"os"
	"testing"
	"time"
)

func TestEvidenceAndRepair(t *testing.T) {
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("real PostgreSQL configuration required")
	}
	ctx := context.Background()
	admin, e := Open(ctx, os.Getenv("TEST_ADMIN_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	defer admin.Pool.Close()
	db, e := Open(ctx, os.Getenv("TEST_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Pool.Close()
	actor := domain.ID()
	if _, e = admin.Pool.Exec(ctx, `INSERT INTO qb.users(id,name) VALUES($1,'recovery fixture')`, actor); e != nil {
		t.Fatal(e)
	}
	cut := time.Now().UTC().Truncate(time.Second).Add(-24 * time.Hour)
	l, e := db.CreateLedger(ctx, actor, CreateLedgerRequest{domain.ID(), "恢复测试", cut})
	if e != nil {
		t.Fatal(e)
	}
	defer func() {
		if e := db.DeleteLedger(ctx, actor, l.ID, l.Name); e != nil {
			t.Error(e)
		}
	}()
	raw, e := db.CreateAccount(ctx, actor, l.ID, AccountRequest{CommandID: domain.ID(), Name: "银行", Type: "ASSET", Cash: true})
	if e != nil {
		t.Fatal(e)
	}
	var a domain.Account
	json.Unmarshal(raw, &a)
	if _, e = db.SetOpening(ctx, actor, l.ID, OpeningRequest{CommandID: domain.ID(), AccountID: a.ID, Amount: 10000, AsOf: cut, Meaning: "BALANCE"}); e != nil {
		t.Fatal(e)
	}
	observed := cut.Add(time.Hour)
	delivery := domain.Delivery{DeliveryID: domain.ID(), DeviceID: "test-device", SourceIdentity: "android.notification:com.icbc", SourceObjectKey: domain.ID(), SnapshotKey: "episode:1", CaptureSequence: 1, Package: "com.icbc", Title: "银行通知", Text: "账户支出人民币10.00元", ObservedAt: observed, NotificationPostedAt: observed, Availability: "AVAILABLE"}
	var observation string
	for i := 0; i < 20; i++ {
		acks, err := db.Ingest(ctx, actor, l.ID, []domain.Delivery{delivery})
		if err != nil || len(acks) != 1 || acks[0].Status != "ACK" {
			t.Fatal("ingest", acks, err)
		}
		if i == 0 {
			observation = acks[0].ObservationID
		} else if acks[0].ObservationID != observation {
			t.Fatal("duplicate observation")
		}
	}
	changed := delivery
	changed.DeliveryID = domain.ID()
	changed.SnapshotKey = "episode:2"
	changed.CaptureSequence = 2
	changed.Text = "账户支出人民币11.00元"
	if _, e = db.Ingest(ctx, actor, l.ID, []domain.Delivery{changed}); e != nil {
		t.Fatal(e)
	}
	changed.DeliveryID = domain.ID()
	changed.SnapshotKey = "episode:3"
	changed.CaptureSequence = 3
	changed.Text = delivery.Text
	if _, e = db.Ingest(ctx, actor, l.ID, []domain.Delivery{changed}); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 1000; i++ {
		worked, err := db.Work(ctx)
		if err != nil {
			t.Fatal("worker", err)
		}
		if !worked {
			break
		}
		if i == 999 {
			t.Fatal("outbox did not drain")
		}
	}
	events, e := db.Events(ctx, actor, l.ID)
	if e != nil || len(events) != 1 || len(events[0].EvidenceIDs) != 3 || events[0].Posted {
		t.Fatal("snapshot recovery", events, e)
	}
	first := events[0]
	f := domain.Facts{Kind: "EXPENSE", Amount: 1000, Currency: "CNY", OccurredAt: &observed, FundingAccount: a.ID}
	_, e = db.SaveEvent(ctx, actor, l.ID, EventRequest{CommandID: domain.ID(), EventID: first.ID, ExpectedRevision: first.Revision, Facts: f})
	if e != nil {
		t.Fatal(e)
	}
	second := domain.ID()
	_, e = db.SaveEvent(ctx, actor, l.ID, EventRequest{CommandID: domain.ID(), EventID: second, Facts: f})
	if e != nil {
		t.Fatal(e)
	}
	r, e := db.Reports(ctx, actor, l.ID, cut, observed.Add(time.Hour))
	if e != nil || r.Consumption != 2000 {
		t.Fatal("same amount must stay separate", r, e)
	}
	raw, e = db.Merge(ctx, actor, l.ID, MergeRequest{CommandID: domain.ID(), SourceID: first.ID, TargetID: second, SourceRevision: first.Revision + 1, TargetRevision: 1})
	if e != nil {
		t.Fatal("merge", e)
	}
	var merged domain.Event
	json.Unmarshal(raw, &merged)
	r, e = db.Reports(ctx, actor, l.ID, cut, observed.Add(time.Hour))
	if e != nil || r.Consumption != 1000 || r.CashChange != -1000 {
		t.Fatal("merge effects", r, e)
	}
	left, right := f, f
	left.Amount = 400
	right.Amount = 600
	bad := SplitRequest{CommandID: domain.ID(), SourceID: second, ExpectedRevision: merged.Revision, Children: []domain.Facts{left, right}}
	bad.Children[1].FundingAccount = domain.ID()
	if _, e = db.Split(ctx, actor, l.ID, bad); e == nil {
		t.Fatal("invalid split accepted")
	}
	after, e := db.Reports(ctx, actor, l.ID, cut, observed.Add(time.Hour))
	if e != nil || after.Version != r.Version || after.Consumption != r.Consumption {
		t.Fatal("split rollback", after, e)
	}
	_, e = db.Split(ctx, actor, l.ID, SplitRequest{CommandID: domain.ID(), SourceID: second, ExpectedRevision: merged.Revision, Children: []domain.Facts{left, right}})
	if e != nil {
		t.Fatal("split", e)
	}
	r, e = db.Reports(ctx, actor, l.ID, cut, observed.Add(time.Hour))
	if e != nil || r.Consumption != 1000 || r.CashChange != -1000 {
		t.Fatal("split effects", r, e)
	}
	var observations int
	e = admin.Pool.QueryRow(ctx, `SELECT count(*) FROM qb.observations WHERE ledger_id=$1 AND source_event_id IN(SELECT id FROM qb.source_events WHERE source_identity='android.notification:com.icbc')`, l.ID).Scan(&observations)
	if e != nil || observations != 3 {
		t.Fatal("A-B-A snapshots", observations, e)
	}
	cardRaw, e := db.CreateAccount(ctx, actor, l.ID, AccountRequest{CommandID: domain.ID(), Name: "信用卡", Type: "LIABILITY"})
	if e != nil {
		t.Fatal(e)
	}
	var card domain.Account
	json.Unmarshal(cardRaw, &card)
	if _, e = db.SetOpening(ctx, actor, l.ID, OpeningRequest{CommandID: domain.ID(), AccountID: card.ID, AsOf: cut, Meaning: "DEBT"}); e != nil {
		t.Fatal(e)
	}
	credit := f
	credit.FundingAccount = card.ID
	credit.Amount = 500
	if _, e = db.SaveEvent(ctx, actor, l.ID, EventRequest{CommandID: domain.ID(), EventID: domain.ID(), Facts: credit}); e != nil {
		t.Fatal(e)
	}
	repay := f
	repay.Kind = "CARD_REPAYMENT"
	repay.Amount = 500
	repay.RepaymentAccount = card.ID
	if _, e = db.SaveEvent(ctx, actor, l.ID, EventRequest{CommandID: domain.ID(), EventID: domain.ID(), Facts: repay}); e != nil {
		t.Fatal(e)
	}
	r, e = db.Reports(ctx, actor, l.ID, cut, observed.Add(time.Hour))
	if e != nil || r.Consumption != 1500 || r.CashChange != -1500 || r.InternalCash != -500 {
		t.Fatal("credit repayment must not count consumption twice", r, e)
	}
	csvReq := CSVRequest{CommandID: domain.ID(), AccountID: a.ID, Content: "record_id,occurred_at,amount_minor,kind,merchant,category\nfixture-csv," + observed.Format(time.RFC3339) + ",100,EXPENSE,Test,Cafe\n"}
	if _, e = db.ImportCSV(ctx, actor, l.ID, csvReq); e != nil {
		t.Fatal(e)
	}
	csvReq.CommandID = domain.ID()
	if _, e = db.ImportCSV(ctx, actor, l.ID, csvReq); e != nil {
		t.Fatal(e)
	}
	r, e = db.Reports(ctx, actor, l.ID, cut, observed.Add(time.Hour))
	if e != nil || r.Consumption != 1600 {
		t.Fatal("CSV duplicate", r, e)
	}
	walletRaw, e := db.CreateAccount(ctx, actor, l.ID, AccountRequest{CommandID: domain.ID(), Name: "钱包", Type: "ASSET", Cash: true})
	if e != nil {
		t.Fatal(e)
	}
	var wallet domain.Account
	json.Unmarshal(walletRaw, &wallet)
	if _, e = db.SetOpening(ctx, actor, l.ID, OpeningRequest{CommandID: domain.ID(), AccountID: wallet.ID, AsOf: cut, Meaning: "BALANCE"}); e != nil {
		t.Fatal(e)
	}
	out, in := f, f
	out.Kind = "TRANSFER_OUT"
	in.Kind = "TRANSFER_IN"
	in.FundingAccount = wallet.ID
	later := observed.Add(24 * time.Hour)
	in.OccurredAt = &later
	outID, inID := domain.ID(), domain.ID()
	if _, e = db.SaveEvent(ctx, actor, l.ID, EventRequest{CommandID: domain.ID(), EventID: outID, Facts: out}); e != nil {
		t.Fatal(e)
	}
	if _, e = db.SaveEvent(ctx, actor, l.ID, EventRequest{CommandID: domain.ID(), EventID: inID, Facts: in}); e != nil {
		t.Fatal(e)
	}
	early, e := db.Reports(ctx, actor, l.ID, cut, observed.Add(time.Hour))
	if e != nil || early.TransferClearing != 1000 || early.InternalCash != -1500 {
		t.Fatal("cross-day cash transfer", early, e)
	}
	var beforeJournals, afterJournals int
	admin.Pool.QueryRow(ctx, `SELECT count(*) FROM qb.journals WHERE ledger_id=$1`, l.ID).Scan(&beforeJournals)
	linked, e := db.LinkTransfer(ctx, actor, l.ID, TransferRequest{CommandID: domain.ID(), OutID: outID, InID: inID, OutRevision: 1, InRevision: 1})
	if e != nil {
		t.Fatal(e)
	}
	admin.Pool.QueryRow(ctx, `SELECT count(*) FROM qb.journals WHERE ledger_id=$1`, l.ID).Scan(&afterJournals)
	if beforeJournals != afterJournals {
		t.Fatal("transfer linkage created a third journal")
	}
	badOut := out
	badOut.Kind = "EXPENSE"
	if _, e = db.SaveEvent(ctx, actor, l.ID, EventRequest{CommandID: domain.ID(), EventID: outID, ExpectedRevision: 1, Facts: badOut}); e == nil {
		t.Fatal("linked transfer changed without unlink")
	}
	var relation map[string]string
	json.Unmarshal(linked, &relation)
	if _, e = db.UnlinkTransfer(ctx, actor, l.ID, UnlinkRequest{CommandID: domain.ID(), RelationID: relation["relation_id"]}); e != nil {
		t.Fatal(e)
	}
	r, e = db.Reports(ctx, actor, l.ID, cut, later.Add(time.Hour))
	if e != nil || r.TransferClearing != 0 || r.CashChange != -1600 || r.InternalCash != -500 {
		t.Fatal("transfer full-window report", r, e)
	}
}
