package accounting

import (
	"fmt"
	"github.com/Kirawii/qianben/backend/internal/domain"
)

type Blocked struct{ Reason string }

func (e *Blocked) Error() string { return e.Reason }
func Plan(ledger domain.Ledger, f domain.Facts, accounts map[string]domain.Account, system map[string]string) (domain.Plan, error) {
	p := domain.Plan{Purpose: "ECONOMIC"}
	var foreignErr error
	f, foreignErr = domain.NormalizeForeign(f)
	if foreignErr != nil {
		return p, foreignErr
	}
	block := func(s string) (domain.Plan, error) { return p, &Blocked{s} }
	if f.Foreign != nil && !f.Foreign.CNYConfirmed {
		return block("CNY_SETTLEMENT_REQUIRED")
	}
	if f.Amount <= 0 || f.Amount > domain.Amount(9_000_000_000_000_000) {
		return p, domain.Invalid("金额须为有效的正整数分")
	}
	if f.Currency != "CNY" {
		return block("CNY_SETTLEMENT_REQUIRED")
	}
	if f.OccurredAt == nil {
		return block("EVENT_TIME_REQUIRED")
	}
	if f.TimePrecision != "" && f.TimePrecision != "EXACT" && f.TimePrecision != "DAY" {
		return block("EVENT_TIME_PRECISION_REQUIRED")
	}
	t := *f.OccurredAt
	if f.TimePrecision == "DAY" {
		end := t.AddDate(0, 0, 1)
		if !t.After(ledger.Cutover) && end.After(ledger.Cutover) {
			return block("CUTOVER_TIME_AMBIGUOUS")
		}
	}
	if !t.After(ledger.Cutover) {
		return block("HISTORICAL_ONLY")
	}
	a, ok := accounts[f.FundingAccount]
	if !ok {
		return block("FUNDING_ACCOUNT_UNKNOWN")
	}
	if !a.Initialized {
		return block("ACCOUNT_NOT_INITIALIZED")
	}
	if a.Type != "ASSET" && a.Type != "LIABILITY" {
		return p, domain.Invalid("出资账户必须是资产或负债账户")
	}
	p.EffectiveAt = t
	dr, cr := "", ""
	switch f.Kind {
	case "EXPENSE":
		dr, cr = system["expense.general"], a.ID
	case "INCOME":
		if !a.Cash {
			return p, domain.Invalid("收入须有现金类到账账户")
		}
		dr, cr = a.ID, system["income.general"]
	case "CASH_OUT":
		dr, cr = system["suspense.asset"], a.ID
	case "CASH_IN":
		dr, cr = a.ID, system["suspense.liability"]
	case "TRANSFER_OUT":
		if !a.Cash {
			return p, domain.Invalid("转账端须为现金类账户")
		}
		dr, cr = system["clearing.transfer"], a.ID
	case "TRANSFER_IN":
		if !a.Cash {
			return p, domain.Invalid("转账端须为现金类账户")
		}
		dr, cr = a.ID, system["clearing.transfer"]
	case "ADVANCE":
		dr, cr = system["receivable.reimbursement"], a.ID
	case "REIMBURSEMENT":
		if !a.Cash {
			return p, domain.Invalid("报销到账须为现金类账户")
		}
		dr, cr = a.ID, system["receivable.reimbursement"]
	case "ASSET_PURCHASE":
		if f.AssetTitle == "" {
			return block("ASSET_CONFIRMATION_REQUIRED")
		}
		dr, cr = system["asset.fixed"], a.ID
	case "REFUND":
		if f.OriginalEvent == "" && !f.HistoricalOriginal {
			return block("REFUND_ORIGINAL_REQUIRED")
		}
		dr, cr = a.ID, system["expense.general"]
	case "ASSET_REFUND":
		if f.OriginalEvent == "" {
			return block("REFUND_ORIGINAL_REQUIRED")
		}
		dr, cr = a.ID, system["asset.fixed"]
	case "CARD_REPAYMENT":
		return block("REPAYMENT_TARGET_REQUIRED")
	case "PENDING", "UNKNOWN":
		return block("FINAL_FINANCIAL_STATE_REQUIRED")
	default:
		return p, domain.Invalid("不支持的经济事件类型")
	}
	if dr == "" || cr == "" || dr == cr {
		return p, domain.Invalid("无效对手科目")
	}
	p.Entries = []domain.Entry{{AccountID: dr, Debit: f.Amount}, {AccountID: cr, Credit: f.Amount}}
	return p, Validate(p)
}

func Repayment(ledger domain.Ledger, f domain.Facts, bank, card domain.Account) (domain.Plan, error) {
	f.Kind = "EXPENSE"
	p, e := Plan(ledger, f, map[string]domain.Account{bank.ID: bank}, map[string]string{"expense.general": card.ID})
	if e != nil {
		return p, e
	}
	if !bank.Cash || card.Type != "LIABILITY" || !card.Initialized {
		return p, domain.Invalid("还款须有已初始化银行及信用卡账户")
	}
	return p, nil
}

func Validate(p domain.Plan) error {
	if len(p.Entries) < 2 {
		return domain.Invalid("分录至少两行")
	}
	var dr, cr domain.Amount
	for _, l := range p.Entries {
		if !domain.IsUUID(l.AccountID) || l.Debit < 0 || l.Credit < 0 || (l.Debit == 0) == (l.Credit == 0) {
			return domain.Invalid("无效分录行")
		}
		var e error
		dr, e = domain.Add(dr, l.Debit)
		if e != nil {
			return e
		}
		cr, e = domain.Add(cr, l.Credit)
		if e != nil {
			return e
		}
	}
	if dr != cr {
		return fmt.Errorf("journal unbalanced: %d != %d", dr, cr)
	}
	return nil
}
