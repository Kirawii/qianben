package evidence

import (
	"github.com/Kirawii/qianben/backend/internal/domain"
	"testing"
	"time"
)

func TestDuplicateCandidatesRespectCompleteCluster(t *testing.T) {
	now := time.Now()
	makeEvent := func(provider, object string) MatchEvent {
		return MatchEvent{Facts: domain.Facts{Kind: "CASH_OUT", Amount: 1000, Currency: "CNY"}, ObservedAt: now, Sources: map[string][]string{provider: {object}}}
	}
	bank := makeEvent("android.notification:com.icbc", "bank-1")
	payment := makeEvent("android.notification:com.tencent.mm", "payment-1")
	if len(DuplicateReasons(bank, payment)) == 0 {
		t.Fatal("cross-source candidate missing")
	}
	shared := bank
	shared.Sources = map[string][]string{"android.notification:com.icbc": {"bank-1"}, "android.notification:com.tencent.mm": {"payment-1"}}
	if len(DuplicateReasons(shared, payment)) != 0 {
		t.Fatal("shared inherited evidence proposed as duplicate")
	}
	secondBank := makeEvent("android.notification:com.icbc", "bank-2")
	if len(DuplicateReasons(bank, secondBank)) != 0 {
		t.Fatal("two real equal bank debits proposed as duplicate")
	}
	payment.Sources["android.notification:com.icbc"] = []string{"bank-2"}
	if len(DuplicateReasons(bank, payment)) != 0 {
		t.Fatal("pairwise similarity bypassed whole-cluster conflict")
	}
	delete(payment.Sources, "android.notification:com.icbc")
	payment.Facts.FundingAccount = "account-b"
	bank.Facts.FundingAccount = "account-a"
	if len(DuplicateReasons(bank, payment)) != 0 {
		t.Fatal("conflicting actual funding accounts accepted")
	}
	bank.Facts.FundingAccount = ""
	payment.ObservedAt = now.Add(3 * time.Minute)
	if len(DuplicateReasons(bank, payment)) != 0 {
		t.Fatal("remote capture times accepted")
	}
	payment.ObservedAt = now
	payment.Facts.Currency = "USD"
	if len(DuplicateReasons(bank, payment)) != 0 {
		t.Fatal("different settlement currencies accepted")
	}
}
