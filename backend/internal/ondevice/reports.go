package ondevice

import (
	"github.com/Kirawii/qianben/backend/internal/domain"
	"sort"
	"strings"
	"time"
)

func (b *Book) report(from, to time.Time) (any, error) {
	if from.IsZero() || !from.Before(to) {
		return nil, domain.Invalid("报表区间无效")
	}
	values := map[string]domain.Amount{}
	keys := []string{"confirmed_assets_minor", "confirmed_liabilities_minor", "confirmed_net_worth_minor", "provisional_net_worth_minor", "assets_minor", "liabilities_minor", "net_worth_minor", "income_minor", "surplus_minor", "expense_minor", "consumption_minor", "cash_change_minor", "external_cash_minor", "internal_cash_minor", "pending_transfer_cash_minor", "pending_transfer_in_minor", "pending_transfer_out_minor", "card_repayment_cash_minor", "unresolved_cash_minor", "unresolved_balance_minor", "transfer_clearing_minor"}
	for _, key := range keys {
		values[key] = 0
	}
	add := func(key string, n domain.Amount) error { v, e := domain.Add(values[key], n); values[key] = v; return e }
	categories := map[string]domain.Amount{}
	accounts, e := b.balances(to)
	if e != nil {
		return nil, e
	}
	var suspenseIn, suspenseOut domain.Amount
	for _, a := range accounts {
		if a.Type == "ASSET" {
			if e = add("assets_minor", a.Balance); e != nil {
				return nil, e
			}
			if a.Code != "suspense.asset" {
				if e = add("confirmed_assets_minor", a.Balance); e != nil {
					return nil, e
				}
			}
		}
		if a.Type == "LIABILITY" {
			if e = add("liabilities_minor", a.Balance); e != nil {
				return nil, e
			}
			if a.Code != "suspense.liability" {
				if e = add("confirmed_liabilities_minor", a.Balance); e != nil {
					return nil, e
				}
			}
		}
		if a.Code == "suspense.asset" {
			suspenseOut = a.Balance
		}
		if a.Code == "suspense.liability" {
			suspenseIn = a.Balance
		}
		if a.Code == "clearing.transfer" {
			values["transfer_clearing_minor"] = a.Balance
		}
	}
	values["net_worth_minor"], e = domain.Subtract(values["assets_minor"], values["liabilities_minor"])
	if e != nil {
		return nil, e
	}
	values["confirmed_net_worth_minor"], e = domain.Subtract(values["confirmed_assets_minor"], values["confirmed_liabilities_minor"])
	if e != nil {
		return nil, e
	}
	values["provisional_net_worth_minor"], e = domain.Subtract(suspenseOut, suspenseIn)
	if e != nil {
		return nil, e
	}
	values["unresolved_balance_minor"], e = domain.Add(suspenseOut, suspenseIn)
	if e != nil {
		return nil, e
	}
	for _, j := range b.Journals {
		if j.Event == "" || j.At.Before(from) || !j.At.Before(to) {
			continue
		}
		ev := b.Events[j.Event]
		for _, line := range j.Entries {
			a := b.Accounts[line.AccountID]
			n, err := domain.Subtract(line.Debit, line.Credit)
			if err != nil {
				return nil, err
			}
			if a.Cash {
				if e = add("cash_change_minor", n); e != nil {
					return nil, e
				}
				key := "external_cash_minor"
				if ev.Facts.Kind == "CASH_OUT" || ev.Facts.Kind == "CASH_IN" {
					key = "unresolved_cash_minor"
				}
				if strings.HasPrefix(ev.Facts.Kind, "TRANSFER_") {
					key = "pending_transfer_cash_minor"
				}
				if e = add(key, n); e != nil {
					return nil, e
				}
				if ev.Facts.Kind == "CARD_REPAYMENT" {
					if e = add("card_repayment_cash_minor", n); e != nil {
						return nil, e
					}
				}
			}
			if a.Code == "expense.general" {
				if e = add("expense_minor", n); e != nil {
					return nil, e
				}
				if e = add("consumption_minor", n); e != nil {
					return nil, e
				}
				category := ev.Facts.Category
				if category == "" {
					category = "未分类"
				}
				categories[category], e = domain.Add(categories[category], n)
				if e != nil {
					return nil, e
				}
			}
			if a.Code == "income.general" {
				n, e = domain.Subtract(0, n)
				if e != nil {
					return nil, e
				}
				if e = add("income_minor", n); e != nil {
					return nil, e
				}
			}
		}
	}
	values["surplus_minor"], e = domain.Subtract(values["income_minor"], values["expense_minor"])
	if e != nil {
		return nil, e
	}
	review, unlinked := 0, 0
	for _, ev := range b.Events {
		if ev.Status == "REVIEW_REQUIRED" {
			review++
		}
		if ev.Posted && strings.HasPrefix(ev.Facts.Kind, "TRANSFER_") && ev.Facts.OccurredAt.Before(to) {
			unlinked++
			if !ev.Facts.OccurredAt.Before(from) {
				key := "pending_transfer_out_minor"
				if ev.Facts.Kind == "TRANSFER_IN" {
					key = "pending_transfer_in_minor"
				}
				if e = add(key, ev.Facts.Amount); e != nil {
					return nil, e
				}
			}
		}
	}
	out := map[string]any{"source_version": b.Ledger.Version, "algorithm_version": "ondevice-core-v1", "from": from, "to": to, "review_count": review, "unlinked_transfer_count": unlinked}
	for key, value := range values {
		out[key] = value
	}
	rows := []map[string]any{}
	names := []string{}
	for name := range categories {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		rows = append(rows, map[string]any{"category": name, "consumption_minor": categories[name]})
	}
	out["categories"] = rows
	return out, nil
}
