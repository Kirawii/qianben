package evidence

import (
	"github.com/Kirawii/qianben/backend/internal/domain"
	"regexp"
	"strconv"
	"strings"
)

var amountRE = regexp.MustCompile(`(?:人民币|金额|支付|支出|收入|退款|扣款|到账|消费|付款|收款|[¥￥])\s*[:：]?\s*(\d{1,12}(?:\.\d{1,2})?)\s*(?:元|CNY)?`)
var tailRE = regexp.MustCompile(`(?:尾号|尾数|卡号末四位)\s*[:：]?\s*(\d{4})`)
var gateRE = regexp.MustCompile(`支付成功|付款成功|交易成功|消费人民币|扣款成功|退款成功|退款到账|收款成功|到账人民币|账户支出|账户收入`)
var foreignRE = regexp.MustCompile(`(?i)USD|EUR|JPY|HKD|GBP|美元|欧元|日元|港币|英镑|\$|€|£`)

const ParserVersion = "notification-v1"

func Candidate(d domain.Delivery) bool {
	if d.Structured != nil {
		return ValidSummary(d)
	}
	if d.Package == "com.tencent.mm" && d.Title != "微信支付" && d.Title != "微信收款助手" && d.Title != "微信支付凭证" {
		return false
	}
	if d.Package == "com.eg.android.AlipayGphone" && d.Title != "支付宝" && d.Title != "支付助手" && d.Title != "收款到账" && d.Title != "收钱到账" {
		return false
	}
	return !d.GroupSummary && d.Availability != "REDACTED" && gateRE.MatchString(d.Title+" "+d.Text+" "+d.BigText)
}
func Parse(d domain.Delivery) (domain.Facts, string) {
	f := domain.Facts{Kind: "UNKNOWN", Currency: "CNY", Merchant: d.Title, Category: "待分类", TimePrecision: "UNKNOWN"}
	if d.Structured != nil {
		if !ValidSummary(d) {
			return f, "INVALID_STRUCTURED_NOTIFICATION"
		}
		f.Amount, f.Currency, f.Kind = d.Structured.Amount, d.Structured.Currency, d.Structured.Kind
		f.Merchant = ""
		return f, "LOCAL_STRUCTURED_HINT"
	}
	text := d.Title + " " + d.Text
	if d.BigText != "" {
		text = d.Title + " " + d.BigText
	}
	if d.GroupSummary {
		return f, "GROUP_SUMMARY"
	}
	if d.Availability == "REDACTED" {
		return f, "CONTENT_UNAVAILABLE"
	}
	if !gateRE.MatchString(text) {
		return f, "UNSUPPORTED_TEMPLATE"
	}
	if foreignRE.MatchString(text) {
		f.Currency = "UNKNOWN"
	}
	matches := amountRE.FindAllStringSubmatch(text, -1)
	unique := map[string]bool{}
	for _, m := range matches {
		unique[m[1]] = true
	}
	if len(unique) != 1 {
		return f, "AMOUNT_AMBIGUOUS"
	}
	for s := range unique {
		parts := strings.Split(s, ".")
		whole, e := strconv.ParseInt(parts[0], 10, 64)
		if e != nil {
			return f, "AMOUNT_INVALID"
		}
		fraction := int64(0)
		if len(parts) > 1 {
			v := parts[1]
			if len(v) == 1 {
				v += "0"
			}
			fraction, _ = strconv.ParseInt(v, 10, 64)
		}
		f.Amount = domain.Amount(whole*100 + fraction)
	}
	if f.Amount <= 0 {
		return f, "AMOUNT_INVALID"
	}
	if strings.Contains(text, "退款") {
		f.Kind = "CASH_IN"
	} else if strings.Contains(text, "收款成功") || strings.Contains(text, "到账人民币") || strings.Contains(text, "账户收入") {
		f.Kind = "CASH_IN"
	} else {
		f.Kind = "CASH_OUT"
	}
	tail := ""
	if m := tailRE.FindStringSubmatch(text); len(m) > 1 {
		tail = m[1]
	}
	return f, tail
}

func ValidSummary(d domain.Delivery) bool {
	s := d.Structured
	if s == nil || s.Version != "local-notification-v1" || d.GroupSummary || d.Availability == "REDACTED" || d.Text != "" || d.BigText != "" || s.Amount < 0 || s.Amount > 99999999999999 {
		return false
	}
	if s.Currency != "CNY" && s.Currency != "UNKNOWN" {
		return false
	}
	if s.Kind != "UNKNOWN" && s.Kind != "CASH_IN" && s.Kind != "CASH_OUT" {
		return false
	}
	if s.Amount == 0 && s.Kind != "UNKNOWN" {
		return false
	}
	if len(s.RawHash) != 64 {
		return false
	}
	for _, c := range s.RawHash {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	if d.Package == "com.tencent.mm" && d.Title != "微信支付" && d.Title != "微信收款助手" && d.Title != "微信支付凭证" {
		return false
	}
	if d.Package == "com.eg.android.AlipayGphone" && d.Title != "支付宝" && d.Title != "支付助手" && d.Title != "收款到账" && d.Title != "收钱到账" {
		return false
	}
	return true
}
