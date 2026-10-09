package store

import (
	"context"
	"encoding/json"
	"fmt"
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
	r, e = db.Reports(ctx, actor, l.ID, cut, observed.Add(time.Hour))
	if e != nil || r.Assets != 9000 || r.Liabilities != 500 || r.NetWorth != 8500 {
		t.Fatal("credit balance sheet", r, e)
	}
	repay := f
	repay.Kind = "CARD_REPAYMENT"
	repay.Amount = 500
	repay.RepaymentAccount = card.ID
	if _, e = db.SaveEvent(ctx, actor, l.ID, EventRequest{CommandID: domain.ID(), EventID: domain.ID(), Facts: repay}); e != nil {
		t.Fatal(e)
	}
	r, e = db.Reports(ctx, actor, l.ID, cut, observed.Add(time.Hour))
	if e != nil || r.Consumption != 1500 || r.CashChange != -1500 || r.InternalCash != 0 || r.ExternalCash != -1500 {
		t.Fatal("credit repayment must not count consumption twice", r, e)
	}
	if r.Assets != 8500 || r.Liabilities != 0 || r.NetWorth != 8500 {
		t.Fatal("repayment changed net worth", r)
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
	out.TimePrecision, in.TimePrecision = "EXACT", "EXACT"
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
	if e != nil || early.TransferClearing != 1000 || early.InternalCash != 0 || early.PendingTransferCash != -1000 || early.PendingTransferOut != 1000 || early.PendingTransferIn != 0 || early.UnlinkedTransfers != 1 {
		t.Fatal("cross-day cash transfer", early, e)
	}
	transferCandidates, e := db.TransferCandidates(ctx, actor, l.ID, outID)
	if e != nil || len(transferCandidates.Candidates) != 1 || transferCandidates.Candidates[0].Event.ID != inID || transferCandidates.Ambiguous {
		t.Fatal("cross-day transfer candidate", transferCandidates, e)
	}
	if _, e = db.TransferCandidates(ctx, domain.ID(), l.ID, outID); e == nil {
		t.Fatal("cross-user transfer candidates")
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
	if _, e = db.TransferCandidates(ctx, actor, l.ID, outID); e == nil {
		t.Fatal("linked transfer offered another candidate")
	}
	early, e = db.Reports(ctx, actor, l.ID, cut, observed.Add(time.Hour))
	if e != nil || early.PendingTransferCash != -1000 || early.InternalCash != 0 {
		t.Fatal("future arrival prematurely closed historical transfer", early, e)
	}
	closed, e := db.Reports(ctx, actor, l.ID, cut, later.Add(time.Hour))
	if e != nil || closed.PendingTransferCash != 0 || closed.InternalCash != 0 || closed.ExternalCash != -1600 {
		t.Fatal("closed full-window transfer", closed, e)
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
	if e != nil || r.TransferClearing != 0 || r.CashChange != -1600 || r.InternalCash != 0 || r.PendingTransferCash != 0 || r.PendingTransferIn != 1000 || r.PendingTransferOut != 1000 || r.CardRepaymentCash != -500 || r.UnlinkedTransfers != 2 {
		t.Fatal("transfer full-window report", r, e)
	}
	secondIn := domain.ID()
	if _, e = db.SaveEvent(ctx, actor, l.ID, EventRequest{CommandID: domain.ID(), EventID: secondIn, Facts: in}); e != nil {
		t.Fatal(e)
	}
	sameAccount := in
	sameAccount.FundingAccount = out.FundingAccount
	if _, e = db.SaveEvent(ctx, actor, l.ID, EventRequest{CommandID: domain.ID(), EventID: domain.ID(), Facts: sameAccount}); e != nil {
		t.Fatal(e)
	}
	ambiguousTransfer, e := db.TransferCandidates(ctx, actor, l.ID, outID)
	if e != nil || len(ambiguousTransfer.Candidates) != 2 || !ambiguousTransfer.Ambiguous {
		t.Fatal("transfer ambiguity or same-account veto", ambiguousTransfer, e)
	}
	if _, e = db.LinkTransfer(ctx, actor, l.ID, TransferRequest{CommandID: domain.ID(), OutID: outID, InID: secondIn, OutRevision: 99, InRevision: 1}); e == nil {
		t.Fatal("stale transfer candidate revision accepted")
	}
	// Non-cash assets must not enter the cash-transfer template.
	baseline, e := db.Reports(ctx, actor, l.ID, cut, later.Add(time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	investmentRaw, e := db.CreateAccount(ctx, actor, l.ID, AccountRequest{CommandID: domain.ID(), Name: "非现金投资账户", Type: "ASSET", Cash: false})
	if e != nil {
		t.Fatal(e)
	}
	var investment domain.Account
	json.Unmarshal(investmentRaw, &investment)
	if _, e = db.SetOpening(ctx, actor, l.ID, OpeningRequest{CommandID: domain.ID(), AccountID: investment.ID, AsOf: cut, Meaning: "BALANCE"}); e != nil {
		t.Fatal(e)
	}
	investIn := in
	investIn.Amount = 500
	investIn.FundingAccount = investment.ID
	if _, e = db.SaveEvent(ctx, actor, l.ID, EventRequest{CommandID: domain.ID(), EventID: domain.ID(), Facts: investIn}); e == nil {
		t.Fatal("non-cash asset accepted as cash-transfer leg")
	}
	afterInvestment, e := db.Reports(ctx, actor, l.ID, cut, later.Add(time.Hour))
	if e != nil || afterInvestment.ExternalCash != baseline.ExternalCash || afterInvestment.CashChange != baseline.CashChange || afterInvestment.PendingTransferCash != baseline.PendingTransferCash || afterInvestment.InternalCash != baseline.InternalCash {
		t.Fatal("rejected non-cash transfer changed cash flow", baseline, afterInvestment, e)
	}
	// Candidate queries inspect immutable evidence across the whole cluster.
	notify := func(pkg, title string) domain.Delivery {
		item := delivery
		item.DeliveryID, item.SourceObjectKey = domain.ID(), domain.ID()
		item.SnapshotKey = "candidate:1"
		item.CaptureSequence = 1
		item.Package, item.SourceIdentity, item.Title = pkg, "android.notification:"+pkg, title
		item.Text = "支付成功人民币7.77元"
		if _, err := db.Ingest(ctx, actor, l.ID, []domain.Delivery{item}); err != nil {
			t.Fatal(err)
		}
		return item
	}
	drain := func() {
		t.Helper()
		for i := 0; i < 1000; i++ {
			worked, err := db.Work(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !worked {
				return
			}
		}
		t.Fatal("candidate outbox did not drain")
	}
	notify("com.icbc", "候选银行卡")
	notify("com.tencent.mm", "微信支付")
	drain()
	events, e = db.Events(ctx, actor, l.ID)
	if e != nil {
		t.Fatal(e)
	}
	var payment domain.Event
	for _, ev := range events {
		if ev.Facts.Merchant == "微信支付" && ev.Facts.Amount == 777 {
			payment = ev
		}
	}
	if payment.ID == "" {
		t.Fatal("candidate payment not interpreted")
	}
	pendingHistory, err := db.History(ctx, actor, l.ID, payment.ID)
	if err != nil || len(pendingHistory) != 1 {
		t.Fatal("pending history", pendingHistory, err)
	}
	var pendingVersion struct {
		AccountingRevision *int64 `json:"accounting_revision"`
	}
	if err = json.Unmarshal(pendingHistory[0], &pendingVersion); err != nil || pendingVersion.AccountingRevision != nil {
		t.Fatal("unposted revision claimed inherited journal", pendingVersion, err)
	}
	candidates, e := db.DuplicateCandidates(ctx, actor, l.ID, payment.ID)
	if e != nil || len(candidates.Candidates) != 1 || candidates.Ambiguous || candidates.Truncated {
		t.Fatal("cross-source candidate query", candidates, e)
	}
	bankCandidate := candidates.Candidates[0].Event
	notify("com.icbc", "第二笔真实同额扣款")
	drain()
	candidates, e = db.DuplicateCandidates(ctx, actor, l.ID, payment.ID)
	if e != nil || len(candidates.Candidates) != 2 || !candidates.Ambiguous {
		t.Fatal("ambiguous equal payments", candidates, e)
	}
	if _, e = db.DuplicateCandidates(ctx, domain.ID(), l.ID, payment.ID); e == nil {
		t.Fatal("cross-user candidate read")
	}
	raw, e = db.Merge(ctx, actor, l.ID, MergeRequest{CommandID: domain.ID(), SourceID: bankCandidate.ID, TargetID: payment.ID, SourceRevision: bankCandidate.Revision, TargetRevision: payment.Revision})
	if e != nil {
		t.Fatal(e)
	}
	candidates, e = db.DuplicateCandidates(ctx, actor, l.ID, payment.ID)
	if e != nil || len(candidates.Candidates) != 0 {
		t.Fatal("whole cluster conflict did not veto second bank transaction", candidates, e)
	}
	if _, e = admin.Pool.Exec(ctx, `UPDATE qb.events SET created_at=now()-interval '8 days' WHERE ledger_id=$1 AND id=$2`, l.ID, payment.ID); e != nil {
		t.Fatal(e)
	}
	qualityRaw, e := db.Quality(ctx, actor, l.ID)
	if e != nil {
		t.Fatal(e)
	}
	var quality struct {
		Review  int64             `json:"review_count"`
		Old     int64             `json:"review_older_than_7d_count"`
		CSV     int64             `json:"csv_evidence_count"`
		Reasons []json.RawMessage `json:"review_reasons"`
	}
	if e = json.Unmarshal(qualityRaw, &quality); e != nil || quality.Review != 2 || quality.Old != 1 || quality.CSV != 1 || len(quality.Reasons) == 0 {
		t.Fatal("quality aging and import evidence", quality, e)
	}
	// Minimal uploads are immutable structured observations, not fabricated raw text.
	item := delivery
	item.DeliveryID, item.SourceObjectKey, item.SnapshotKey = domain.ID(), domain.ID(), "local:1"
	item.Title, item.Text, item.BigText = "工商银行", "", ""
	item.Structured = &domain.NotificationSummary{Version: "local-notification-v1", Amount: 12345, Currency: "CNY", Kind: "CASH_OUT", RawHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	for i := 0; i < 2; i++ {
		acks, err := db.Ingest(ctx, actor, l.ID, []domain.Delivery{item})
		if err != nil || len(acks) != 1 || acks[0].Status != "ACK" {
			t.Fatal("structured ingest retry", acks, err)
		}
	}
	drain()
	events, e = db.Events(ctx, actor, l.ID)
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, ev := range events {
		if ev.Facts.Amount == 12345 {
			found = true
			if ev.Posted || ev.Facts.OccurredAt != nil || ev.Facts.FundingAccount != "" || ev.Facts.Merchant != "" {
				t.Fatal("structured hint acquired unsupported proof", ev)
			}
		}
	}
	if !found {
		t.Fatal("structured observation not interpreted")
	}
	changed = item
	copySummary := *item.Structured
	changed.Structured = &copySummary
	changed.Structured.Amount = 54321
	acks, err := db.Ingest(ctx, actor, l.ID, []domain.Delivery{changed})
	if err != nil || len(acks) != 1 || acks[0].Status != "REJECTED" {
		t.Fatal("mutated structured delivery accepted", acks, err)
	}
	var historyEvent domain.Event
	for _, ev := range events {
		if ev.Posted && ev.Facts.Kind == "EXPENSE" {
			historyEvent = ev
			break
		}
	}
	if historyEvent.ID == "" {
		t.Fatal("no posted history fixture")
	}
	for i := 0; i < 104; i++ {
		facts := historyEvent.Facts
		facts.Note = fmt.Sprintf("history pagination %d", i)
		raw, err := db.SaveEvent(ctx, actor, l.ID, EventRequest{CommandID: domain.ID(), EventID: historyEvent.ID, ExpectedRevision: historyEvent.Revision, Facts: facts})
		if err != nil {
			t.Fatal("history revision", i, err)
		}
		if err = json.Unmarshal(raw, &historyEvent); err != nil {
			t.Fatal(err)
		}
	}
	page, err := db.History(ctx, actor, l.ID, historyEvent.ID)
	if err != nil || len(page) != 100 {
		t.Fatal("history first page", len(page), err)
	}
	var last struct {
		Revision int64 `json:"revision"`
	}
	if err = json.Unmarshal(page[99], &last); err != nil {
		t.Fatal(err)
	}
	older, err := db.HistoryBefore(ctx, actor, l.ID, historyEvent.ID, last.Revision)
	if err != nil || len(older) != int(last.Revision-1) {
		t.Fatal("history older page", len(older), last, err)
	}
	if len(older) == 0 {
		t.Fatal("older revisions lost")
	}
	var oldest struct {
		Revision int64 `json:"revision"`
	}
	json.Unmarshal(older[len(older)-1], &oldest)
	if oldest.Revision != 1 {
		t.Fatal("history does not reach original", oldest)
	}
	if _, err = db.HistoryBefore(ctx, domain.ID(), l.ID, historyEvent.ID, last.Revision); err == nil {
		t.Fatal("cross-user history")
	}
	if _, err = db.HistoryBefore(ctx, actor, l.ID, historyEvent.ID, -1); err == nil {
		t.Fatal("negative history cursor")
	}
}
