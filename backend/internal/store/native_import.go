package store

import (
	"context"
	"encoding/json"
	"github.com/Kirawii/qianben/backend/internal/domain"
)

type NativeCSVImportRequest struct {
	CommandID    string            `json:"command_id"`
	AccountID    string            `json:"statement_account_id"`
	Format       string            `json:"format"`
	Content      string            `json:"content"`
	ConfirmedCNY bool              `json:"confirmed_final_cny"`
	Mappings     map[string]string `json:"payment_method_accounts"`
}

func (d *DB) ImportNativeCSV(ctx context.Context, actor, ledger string, r NativeCSVImportRequest) (json.RawMessage, error) {
	if !domain.IsUUID(r.AccountID) {
		return nil, domain.Invalid("须指定稳定的账单来源账户")
	}
	p, e := parseNativeCSV(NativeCSVRequest{r.Format, r.Content})
	if e != nil {
		return nil, e
	}
	methods := map[string]bool{}
	for _, m := range p.Methods {
		methods[m] = true
	}
	for method, account := range r.Mappings {
		if !methods[method] || !domain.IsUUID(account) {
			return nil, domain.Invalid("支付方式或映射账户无效")
		}
	}
	lines := []csvLine{}
	for _, row := range p.Rows {
		at := row.At
		f := domain.Facts{Kind: "UNKNOWN", Amount: row.Amount, Currency: "UNKNOWN", OccurredAt: &at, TimePrecision: "EXACT", Merchant: row.Merchant}
		final := (r.Format == "WECHAT_PERSONAL_V1" && (row.Status == "支付成功" || row.Status == "已收钱")) || (r.Format == "ALIPAY_PERSONAL_V1" && row.Status == "交易成功")
		if r.ConfirmedCNY {
			f.Currency = "CNY"
		}
		if final && r.ConfirmedCNY && r.Mappings[row.Method] != "" {
			switch row.Direction {
			case "OUT":
				f.Kind = "CASH_OUT"
			case "IN":
				f.Kind = "CASH_IN"
			}
			if f.Kind != "UNKNOWN" {
				f.FundingAccount = r.Mappings[row.Method]
			}
		}
		raw := []string{string(domain.Hashable(row)), string(domain.Hashable(f))}
		lines = append(lines, csvLine{row.Record, f, raw})
	}
	identity := "csv.native:" + r.Format + ":" + r.AccountID
	fileRequest := r
	fileRequest.CommandID = ""
	return d.importCSVLines(ctx, actor, ledger, CSVRequest{r.CommandID, r.AccountID, r.Content}, lines, r, r.Format, identity, Hash(domain.Hashable(fileRequest)))
}
