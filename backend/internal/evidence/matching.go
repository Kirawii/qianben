package evidence

import (
	"github.com/Kirawii/qianben/backend/internal/domain"
	"strings"
	"time"
)

type MatchEvent struct {
	Facts      domain.Facts
	ObservedAt time.Time
	// Provider -> immutable source objects already in the complete event cluster.
	Sources map[string][]string
}

// DuplicateReasons proposes manual review only. Capture proximity never becomes
// an economic timestamp, and whole-cluster conflicts veto a candidate.
func DuplicateReasons(a, b MatchEvent) []string {
	if a.Facts.Amount <= 0 || a.Facts.Amount != b.Facts.Amount || a.Facts.Currency != "CNY" || b.Facts.Currency != "CNY" {
		return nil
	}
	direction := func(kind string) string {
		switch kind {
		case "CASH_OUT", "EXPENSE", "ASSET_PURCHASE", "ADVANCE", "TRANSFER_OUT":
			return "OUT"
		case "CASH_IN", "INCOME", "REIMBURSEMENT", "TRANSFER_IN", "REFUND", "ASSET_REFUND":
			return "IN"
		}
		return ""
	}
	if direction(a.Facts.Kind) == "" || direction(a.Facts.Kind) != direction(b.Facts.Kind) {
		return nil
	}
	weak := func(kind string) bool { return kind == "CASH_OUT" || kind == "CASH_IN" }
	if !weak(a.Facts.Kind) && !weak(b.Facts.Kind) && a.Facts.Kind != b.Facts.Kind {
		return nil
	}
	if a.Facts.FundingAccount != "" && b.Facts.FundingAccount != "" && a.Facts.FundingAccount != b.Facts.FundingAccount {
		return nil
	}
	if len(a.Sources) == 0 || len(b.Sources) == 0 {
		return nil
	}
	// Deliberately split events can share inherited evidence. They must not be
	// proposed as duplicate observations solely because that evidence matches.
	for provider, objects := range a.Sources {
		for _, left := range objects {
			for _, right := range b.Sources[provider] {
				if left == right {
					return nil
				}
			}
		}
	}
	sources := map[string]map[string]bool{}
	for _, cluster := range []MatchEvent{a, b} {
		for provider, objects := range cluster.Sources {
			if !strings.HasPrefix(provider, "android.notification:") {
				continue
			}
			if sources[provider] == nil {
				sources[provider] = map[string]bool{}
			}
			for _, object := range objects {
				sources[provider][object] = true
			}
			if len(sources[provider]) > 1 {
				return nil
			}
		}
	}
	if len(sources) < 2 {
		return nil
	}
	at, bt := a.ObservedAt, b.ObservedAt
	reasons := []string{"金额与币种一致", "资金方向一致", "完整证据簇未发现同来源多个交易对象"}
	if a.Facts.OccurredAt != nil && b.Facts.OccurredAt != nil {
		at, bt = *a.Facts.OccurredAt, *b.Facts.OccurredAt
		reasons = append(reasons, "经济发生时间相近")
	} else {
		reasons = append(reasons, "采集时间相近，仅用于候选，不代表经济发生时间")
	}
	if at.IsZero() || bt.IsZero() {
		return nil
	}
	delta := at.Sub(bt)
	if delta < -2*time.Minute || delta > 2*time.Minute {
		return nil
	}
	return append(reasons, "需人工核实交易详情；同额近时不能证明重复")
}
