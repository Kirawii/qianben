package ondevice

import (
	"github.com/Kirawii/qianben/backend/internal/domain"
	"strings"
	"testing"
	"time"
)

func TestLocalNotificationAckAndChangedSnapshotDoNotDuplicateOrRewriteCash(t *testing.T) {
	s := State{Ledgers: map[string]*Book{}}
	id := domain.ID()
	cut := time.Now().Add(-time.Hour)
	s.apply(Request{Operation: "create-ledger", Body: domain.Hashable(map[string]any{"id": id, "name": "通知合成", "cutover_time": cut})})
	b := s.Ledgers[id]
	d := domain.Delivery{DeliveryID: domain.ID(), DeviceID: domain.ID(), SourceIdentity: "android.notification:com.tencent.mm", SourceObjectKey: domain.ID(), SnapshotKey: "snapshot1", Package: "com.tencent.mm", Title: "微信支付", ObservedAt: time.Now(), Availability: "AVAILABLE", Structured: &domain.NotificationSummary{Version: "local-notification-v1", Amount: 1234, Currency: "CNY", Kind: "CASH_OUT", RawHash: strings.Repeat("a", 64)}}
	batch := func(d domain.Delivery) any {
		r, e := b.ingest(domain.Hashable(map[string]any{"items": []domain.Delivery{d}}))
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	batch(d)
	batch(d)
	if len(b.Events) != 1 || len(b.Journals) != 0 || len(b.Observations) != 1 {
		t.Fatal("retry duplicated evidence or posted ambiguous notification")
	}
	var ev domain.Event
	for _, event := range b.Events {
		ev = event
	}
	bank := domain.ID()
	b.Accounts[bank] = domain.Account{ID: bank, Code: "user." + bank, Type: "ASSET", Cash: true, Initialized: true}
	at := cut.Add(time.Minute)
	f := ev.Facts
	f.FundingAccount = bank
	f.OccurredAt = &at
	f.TimePrecision = "EXACT"
	f.Kind = "EXPENSE"
	ev, e := b.save(ev.ID, ev.Revision, f)
	if e != nil || !ev.Posted {
		t.Fatal(ev, e)
	}
	d.DeliveryID = domain.ID()
	d.SnapshotKey = "snapshot2"
	d.Structured.Amount = 5432
	batch(d)
	ev = b.Events[ev.ID]
	if ev.Facts.Amount != 1234 || !ev.Posted || ev.Reason != "EVIDENCE_CHANGED" || len(ev.EvidenceIDs) != 2 || len(b.Journals) != 1 {
		t.Fatal("source update silently rewrote accepted accounting", ev)
	}
	report, e := b.report(cut, time.Now().Add(time.Minute))
	if e != nil {
		t.Fatal(e)
	}
	if report.(map[string]any)["consumption_minor"] != domain.Amount(1234) {
		t.Fatal(report)
	}
}
