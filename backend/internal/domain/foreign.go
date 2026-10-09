package domain

import (
	"math/big"
	"regexp"
	"strings"
	"time"
)

type ForeignSettlement struct {
	OriginalAmount   string     `json:"original_amount"`
	OriginalCurrency string     `json:"original_currency"`
	CNYConfirmed     bool       `json:"cny_settlement_confirmed"`
	DerivedRate      string     `json:"derived_rate,omitempty"`
	RateNumerator    string     `json:"derived_rate_numerator,omitempty"`
	RateDenominator  string     `json:"derived_rate_denominator,omitempty"`
	RateSource       string     `json:"rate_source,omitempty"`
	ReferenceRate    string     `json:"reference_rate,omitempty"`
	ReferenceSource  string     `json:"reference_source,omitempty"`
	ReferenceAt      *time.Time `json:"reference_at,omitempty"`
}

var foreignDecimal = regexp.MustCompile(`^(?:0|[1-9][0-9]{0,11})(?:\.[0-9]{1,6})?$`)
var foreignCode = regexp.MustCompile(`^[A-Z]{3}$`)

// Reference rates never establish a CNY settlement. Derived rates use exact rational arithmetic.
func NormalizeForeign(f Facts) (Facts, error) {
	if f.Foreign == nil {
		return f, nil
	}
	x := *f.Foreign
	f.Foreign = &x
	x.OriginalCurrency = strings.ToUpper(strings.TrimSpace(x.OriginalCurrency))
	if !foreignCode.MatchString(x.OriginalCurrency) || x.OriginalCurrency == "CNY" || !foreignDecimal.MatchString(x.OriginalAmount) {
		return f, Invalid("原币代码或金额无效")
	}
	original, ok := new(big.Rat).SetString(x.OriginalAmount)
	if !ok || original.Sign() <= 0 {
		return f, Invalid("原币金额须为正数")
	}
	if x.ReferenceRate != "" {
		reference, valid := new(big.Rat).SetString(x.ReferenceRate)
		if !foreignDecimal.MatchString(x.ReferenceRate) || !valid || reference.Sign() <= 0 || strings.TrimSpace(x.ReferenceSource) == "" || len(x.ReferenceSource) > 200 || x.ReferenceAt == nil || x.ReferenceAt.IsZero() {
			return f, Invalid("参考汇率须有有效数值、来源和时间")
		}
		at := x.ReferenceAt.Truncate(time.Microsecond)
		x.ReferenceAt = &at
	} else if x.ReferenceSource != "" || x.ReferenceAt != nil {
		return f, Invalid("参考汇率元数据缺少数值")
	}
	x.DerivedRate, x.RateSource = "", ""
	x.RateNumerator, x.RateDenominator = "", ""
	if !x.CNYConfirmed {
		if f.Amount != 0 {
			return f, Invalid("未确认结算时不能把估算人民币金额作为结算金额")
		}
		f.Currency = "UNKNOWN"
		return f, nil
	}
	if f.Currency != "CNY" || f.Amount <= 0 || f.Amount > Amount(9_000_000_000_000_000) {
		return f, Invalid("确认外币结算须有实际人民币分金额")
	}
	settled := new(big.Rat).SetFrac(big.NewInt(int64(f.Amount)), big.NewInt(100))
	ratio := new(big.Rat).Quo(settled, original)
	x.DerivedRate = ratio.FloatString(12)
	x.RateNumerator, x.RateDenominator = ratio.Num().String(), ratio.Denom().String()
	x.RateSource = "SETTLEMENT_DERIVED_USER_CONFIRMED"
	return f, nil
}
