package evidence

import (
	"github.com/Kirawii/qianben/backend/internal/domain"
	"strings"
	"testing"
)

func TestParserDoesNotInvent(t *testing.T) {
	f, tail := Parse(domain.Delivery{Title: "招商银行", Text: "尾号1234账户支出人民币30.50元", Availability: "READABLE"})
	if f.Amount != 3050 || tail != "1234" || f.Kind != "CASH_OUT" || f.OccurredAt != nil || f.FundingAccount != "" {
		t.Fatalf("%+v %s", f, tail)
	}
	f, _ = Parse(domain.Delivery{Text: "张三说：我转了100元"})
	if f.Kind != "UNKNOWN" {
		t.Fatal("chat accepted")
	}
	f, _ = Parse(domain.Delivery{Text: "支付成功30元 余额人民币100元", GroupSummary: true})
	if f.Amount != 0 {
		t.Fatal("summary posted")
	}
	f, _ = Parse(domain.Delivery{Title: "银行通知", Text: "支付成功 支付10美元"})
	if f.Currency == "CNY" {
		t.Fatal("foreign currency assumed as CNY")
	}
	if Candidate(domain.Delivery{Package: "com.tencent.mm", Title: "朋友", Text: "支付成功人民币10元"}) {
		t.Fatal("chat title accepted as payment source")
	}
}

func TestStructuredNotificationIsOnlyAHint(t *testing.T) {
	d := domain.Delivery{Package: "com.tencent.mm", Title: "微信支付", Availability: "AVAILABLE", Structured: &domain.NotificationSummary{Version: "local-notification-v1", Amount: 3050, Currency: "CNY", Kind: "CASH_OUT", RawHash: strings.Repeat("a", 64)}}
	if !Candidate(d) {
		t.Fatal("valid local hint rejected")
	}
	f, _ := Parse(d)
	if f.Amount != 3050 || f.OccurredAt != nil || f.FundingAccount != "" || f.Merchant != "" || f.Kind != "CASH_OUT" {
		t.Fatalf("invented evidence: %+v", f)
	}
	d.Text = "private raw text"
	if Candidate(d) {
		t.Fatal("mixed raw and structured accepted")
	}
	d.Text = ""
	d.Title = "朋友"
	if Candidate(d) {
		t.Fatal("chat accepted")
	}
	d.Title = "微信支付"
	d.Structured.Version = "future"
	if Candidate(d) {
		t.Fatal("unknown schema accepted")
	}
	d.Structured.Version = "local-notification-v1"
	d.Structured.Kind = "EXPENSE"
	if Candidate(d) {
		t.Fatal("economic purpose inferred")
	}
	d.Structured.Kind = "UNKNOWN"
	d.Structured.Amount = 0
	d.Structured.Currency = "UNKNOWN"
	if !Candidate(d) {
		t.Fatal("unresolved observation discarded")
	}
}
