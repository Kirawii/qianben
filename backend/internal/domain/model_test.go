package domain

import (
	"encoding/json"
	"math"
	"testing"
)

func TestMoneyWireAndLimits(t *testing.T) {
	for _, value := range []Amount{0, 1, 9007199254740993, Amount(math.MaxInt64), Amount(math.MinInt64)} {
		b, e := json.Marshal(value)
		if e != nil {
			t.Fatal(e)
		}
		var back Amount
		if e = json.Unmarshal(b, &back); e != nil || back != value {
			t.Fatal("money round trip", value, string(b), e)
		}
	}
	for _, raw := range []string{`100`, `"1.5"`, `"9223372036854775808"`, `null`} {
		var value Amount
		if json.Unmarshal([]byte(raw), &value) == nil {
			t.Fatal("invalid money accepted", raw)
		}
	}
	if _, e := Add(Amount(math.MinInt64), -1); e == nil {
		t.Fatal("negative overflow")
	}
}
