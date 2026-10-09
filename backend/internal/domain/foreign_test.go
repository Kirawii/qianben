package domain

import (
	"testing"
	"time"
)

func TestForeignSettlementHasNoReferenceRateAuthority(t *testing.T) {
	at := time.Now().UTC()
	f := Facts{Kind: "EXPENSE", Currency: "CNY", Foreign: &ForeignSettlement{OriginalAmount: "10", OriginalCurrency: "usd", ReferenceRate: "7.2", ReferenceSource: "用户提供的参考值，未核验", ReferenceAt: &at}}
	pending, err := NormalizeForeign(f)
	if err != nil || pending.Currency != "UNKNOWN" || pending.Amount != 0 || pending.Foreign.DerivedRate != "" || f.Foreign.OriginalCurrency != "usd" {
		t.Fatal("reference became settlement or input mutated", pending, err)
	}
	f.Amount = 7300
	f.Foreign.CNYConfirmed = true
	f.Foreign.DerivedRate = "999"
	f.Foreign.RateSource = "EXTERNAL_QUOTE"
	settled, err := NormalizeForeign(f)
	if err != nil || settled.Foreign.DerivedRate != "7.300000000000" || settled.Foreign.RateSource != "SETTLEMENT_DERIVED_USER_CONFIRMED" || settled.Foreign.RateNumerator != "73" || settled.Foreign.RateDenominator != "10" {
		t.Fatal("rate not derived from actual settlement", settled, err)
	}
	f.Foreign.CNYConfirmed = false
	if _, err = NormalizeForeign(f); err == nil {
		t.Fatal("estimated CNY amount accepted without confirmation")
	}
	f.Amount = 0
	f.Foreign.OriginalAmount = "1/3"
	if _, err = NormalizeForeign(f); err == nil {
		t.Fatal("fraction expression accepted as original decimal")
	}
	f.Foreign.OriginalAmount = "10"
	f.Foreign.ReferenceSource = ""
	if _, err = NormalizeForeign(f); err == nil {
		t.Fatal("reference source missing")
	}
	tiny, err := NormalizeForeign(Facts{Currency: "CNY", Amount: 1, Foreign: &ForeignSettlement{OriginalAmount: "999999999999.999999", OriginalCurrency: "JPY", CNYConfirmed: true}})
	if err != nil || tiny.Foreign.RateNumerator != "10000" || tiny.Foreign.RateDenominator != "999999999999999999" {
		t.Fatal("tiny rate lost exact evidence", tiny, err)
	}
}
