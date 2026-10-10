// Package ondevice provides a deterministic, server-free transaction boundary.
// Android persists the returned state atomically before displaying the response.
package ondevice

import (
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/Kirawii/qianben/backend/internal/accounting"
	"github.com/Kirawii/qianben/backend/internal/domain"
)

type State struct {
	Version int              `json:"version"`
	Ledgers map[string]*Book `json:"ledgers"`
}
type Book struct {
	Ledger          domain.Ledger              `json:"ledger"`
	Accounts        map[string]domain.Account  `json:"accounts"`
	Events          map[string]domain.Event    `json:"events"`
	History         map[string][]domain.Event  `json:"history"`
	Journals        []Journal                  `json:"journals"`
	Receipts        map[string]Receipt         `json:"receipts"`
	Observations    map[string]domain.Delivery `json:"observations,omitempty"`
	Snapshots       map[string]string          `json:"snapshots,omitempty"`
	Sources         map[string]string          `json:"sources,omitempty"`
	HistoryTimes    map[string][]time.Time     `json:"history_times,omitempty"`
	HistoryVersions map[string][]int64         `json:"history_versions,omitempty"`
}
type Journal struct {
	ID         string         `json:"id"`
	Event      string         `json:"event_id"`
	Revision   int64          `json:"revision"`
	ReversalOf string         `json:"reversal_of,omitempty"`
	At         time.Time      `json:"effective_at"`
	Entries    []domain.Entry `json:"entries"`
}
type Receipt struct {
	Request  json.RawMessage `json:"request"`
	Response json.RawMessage `json:"response"`
}
type Request struct {
	Operation string          `json:"operation"`
	Ledger    string          `json:"ledger_id"`
	Command   string          `json:"command_id"`
	Body      json.RawMessage `json:"body"`
}
type Result struct {
	State    json.RawMessage `json:"state,omitempty"`
	Response json.RawMessage `json:"response,omitempty"`
	Error    string          `json:"error,omitempty"`
	Status   int             `json:"status"`
}

// Invoke uses caller-owned input only. An error never publishes partially changed
// state; retries require persisting the successful state and response together.
func Invoke(saved, input string) string {
	r := Result{Status: 200}
	var req Request
	var state State
	e := json.Unmarshal([]byte(input), &req)
	if e == nil && saved != "" {
		e = json.Unmarshal([]byte(saved), &state)
	}
	if e == nil && len(req.Body) > 0 {
		var body any
		e = json.Unmarshal(req.Body, &body)
		if e == nil {
			req.Body, e = json.Marshal(body)
		}
	}
	if e == nil && state.Version != 0 && state.Version != 1 {
		e = domain.Invalid("不支持的本机账本版本")
	}
	if state.Ledgers == nil {
		state.Ledgers = map[string]*Book{}
	}
	state.Version = 1
	var response any
	if e == nil {
		response, e = state.apply(req)
	}
	if e != nil {
		r.Status = 422
		r.Error = e.Error()
		var f *domain.Fault
		if errors.As(e, &f) {
			r.Status = f.Status
			r.Error = f.Message
		}
	} else {
		r.State, _ = json.Marshal(state)
		r.Response, _ = json.Marshal(response)
	}
	result, _ := json.Marshal(r)
	return string(result)
}

func (s *State) apply(r Request) (any, error) {
	if r.Operation == "ledgers" {
		out := []domain.Ledger{}
		for _, b := range s.Ledgers {
			out = append(out, b.Ledger)
		}
		sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
		return out, nil
	}
	if r.Operation == "create-ledger" {
		var l domain.Ledger
		if e := json.Unmarshal(r.Body, &l); e != nil {
			return nil, e
		}
		if !domain.IsUUID(l.ID) || strings.TrimSpace(l.Name) == "" || len(l.Name) > 300 || l.Cutover.IsZero() || l.Cutover.After(time.Now()) {
			return nil, domain.Invalid("账本信息无效")
		}
		if old := s.Ledgers[l.ID]; old != nil {
			if old.Ledger.Name != l.Name || !old.Ledger.Cutover.Equal(l.Cutover) {
				return nil, domain.Conflict("账本 ID 内容冲突")
			}
			return old.Ledger, nil
		}
		l.Currency = "CNY"
		l.Status = "ACTIVE"
		l.Version = 0
		b := &Book{Ledger: l, Accounts: map[string]domain.Account{}, Events: map[string]domain.Event{}, History: map[string][]domain.Event{}, Receipts: map[string]Receipt{}, Journals: []Journal{}}
		for _, a := range [][3]string{{"expense.general", "生活消费", "EXPENSE"}, {"income.general", "收入", "INCOME"}, {"equity.opening", "期初权益", "EQUITY"}, {"suspense.asset", "待查扣款", "ASSET"}, {"suspense.liability", "待查入款", "LIABILITY"}, {"clearing.transfer", "转账清算", "ASSET"}, {"receivable.reimbursement", "报销应收", "ASSET"}, {"asset.fixed", "固定资产原值", "ASSET"}} {
			id := domain.ID()
			b.Accounts[id] = domain.Account{ID: id, Code: a[0], Name: a[1], Type: a[2], Initialized: true}
		}
		s.Ledgers[l.ID] = b
		return l, nil
	}
	b := s.Ledgers[r.Ledger]
	if b == nil {
		return nil, domain.Missing("本机账本不存在")
	}
	switch r.Operation {
	case "accounts":
		return b.balances(time.Time{})
	case "events":
		out := []domain.Event{}
		for _, ev := range b.Events {
			out = append(out, ev)
		}
		sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
		return out, nil
	case "history":
		var v struct {
			ID     string `json:"event_id"`
			Before string `json:"before_revision"`
		}
		if e := json.Unmarshal(r.Body, &v); e != nil {
			return nil, e
		}
		return b.history(v.ID, v.Before)
	case "assets":
		out := []map[string]any{}
		for _, ev := range b.Events {
			if ev.Posted && ev.Facts.Kind == "ASSET_PURCHASE" {
				out = append(out, map[string]any{"id": ev.ID, "event_id": ev.ID, "title": ev.Facts.AssetTitle, "purchase_minor": ev.Facts.Amount, "original_minor": ev.Facts.Amount})
			}
		}
		return out, nil
	case "reports":
		var v struct {
			From time.Time `json:"from"`
			To   time.Time `json:"to"`
		}
		if e := json.Unmarshal(r.Body, &v); e != nil {
			return nil, e
		}
		return b.report(v.From, v.To)
	case "observations":
		return b.ingest(r.Body)
	}
	if len(r.Command) < 8 || len(r.Command) > 128 {
		return nil, domain.Invalid("本机操作需要固定 command_id")
	}
	canonical, _ := json.Marshal(r)
	if old, ok := b.Receipts[r.Command]; ok {
		if string(old.Request) != string(canonical) {
			return nil, domain.Conflict("同一操作 ID 的内容不同")
		}
		return old.Response, nil
	}
	var result any
	var e error
	switch r.Operation {
	case "delete":
		var v struct {
			Name string `json:"confirm_name"`
		}
		if e = json.Unmarshal(r.Body, &v); e == nil {
			if v.Name != b.Ledger.Name {
				e = domain.Invalid("须输入完整账本名称")
			} else {
				delete(s.Ledgers, r.Ledger)
				return map[string]any{"deleted": true}, nil
			}
		}
	case "create-account":
		var a domain.Account
		e = json.Unmarshal(r.Body, &a)
		if e == nil && (strings.TrimSpace(a.Name) == "" || len(a.Name) > 300 || (a.Type != "ASSET" && a.Type != "LIABILITY") || (a.Cash && a.Type != "ASSET")) {
			e = domain.Invalid("账户信息无效")
		}
		if e == nil {
			a.ID = domain.ID()
			a.Code = "user." + a.ID
			a.Initialized = false
			a.Revision = 0
			a.Balance = 0
			b.Accounts[a.ID] = a
			result = a
		}
	case "opening":
		var v struct {
			Account  string        `json:"account_id"`
			Amount   domain.Amount `json:"amount_minor"`
			AsOf     time.Time     `json:"as_of"`
			Revision int64         `json:"expected_revision"`
			Meaning  string        `json:"meaning"`
		}
		e = json.Unmarshal(r.Body, &v)
		a, ok := b.Accounts[v.Account]
		if e == nil && (!ok || !strings.HasPrefix(a.Code, "user.") || !v.AsOf.Equal(b.Ledger.Cutover) || v.Amount < 0 || v.Amount > 9_000_000_000_000_000) {
			e = domain.Invalid("期初账户、金额或统一时点无效")
		}
		if e == nil && ((a.Type == "ASSET" && v.Meaning != "BALANCE") || (a.Type == "LIABILITY" && v.Meaning != "DEBT")) {
			e = domain.Invalid("期初资产须填余额，负债须填欠款")
		}
		if e == nil && (a.Revision != v.Revision || a.Initialized) {
			e = domain.Conflict("本机期初已设置或版本已变化")
		}
		if e == nil {
			equity := b.system()["equity.opening"]
			entries := []domain.Entry{{AccountID: a.ID, Debit: v.Amount}, {AccountID: equity, Credit: v.Amount}}
			if a.Type == "LIABILITY" {
				entries = []domain.Entry{{AccountID: equity, Debit: v.Amount}, {AccountID: a.ID, Credit: v.Amount}}
			}
			if v.Amount > 0 {
				e = accounting.Validate(domain.Plan{Entries: entries})
				if e == nil {
					b.Journals = append(b.Journals, Journal{ID: domain.ID(), At: v.AsOf, Entries: entries})
				}
			}
			if e == nil {
				a.Initialized = true
				a.Revision++
				b.Accounts[a.ID] = a
				for id, ev := range b.Events {
					if !ev.Posted && ev.Reason == "ACCOUNT_NOT_INITIALIZED" && ev.Facts.FundingAccount == a.ID {
						_, e = b.save(id, ev.Revision, ev.Facts)
						if e != nil {
							break
						}
					}
				}
				result = a
			}
		}
	case "save-event":
		var v struct {
			ID       string       `json:"event_id"`
			Revision int64        `json:"expected_revision"`
			Facts    domain.Facts `json:"facts"`
			Learn    bool         `json:"learn_category"`
		}
		e = json.Unmarshal(r.Body, &v)
		if e == nil && v.Learn {
			e = domain.Invalid("本机商户分类规则尚未补齐，请取消记住分类后保存")
		}
		if e == nil {
			result, e = b.save(v.ID, v.Revision, v.Facts)
		}
	default:
		e = domain.Invalid("该功能尚未迁移到本机账本，不能冒充已完成")
	}
	if e != nil {
		return nil, e
	}
	// Check all signed accumulations before allowing Android to persist the state.
	if _, e = b.balances(time.Time{}); e != nil {
		return nil, e
	}
	b.Ledger.Version++
	raw, _ := json.Marshal(result)
	b.Receipts[r.Command] = Receipt{canonical, raw}
	return result, nil
}

func (b *Book) system() map[string]string {
	out := map[string]string{}
	for id, a := range b.Accounts {
		if !strings.HasPrefix(a.Code, "user.") {
			out[a.Code] = id
		}
	}
	return out
}
func (b *Book) save(id string, revision int64, f domain.Facts) (domain.Event, error) {
	if !domain.IsUUID(id) {
		return domain.Event{}, domain.Invalid("事件 ID 无效")
	}
	old := b.Events[id]
	if old.Revision != revision {
		return old, domain.Conflict("事件已被修订")
	}
	// Relation operations must be ported atomically before these kinds are enabled.
	if f.Kind == "REFUND" || f.Kind == "ASSET_REFUND" || f.Kind == "REIMBURSEMENT" {
		return old, domain.Invalid("关联退款及报销分配尚未迁移到本机")
	}
	normalized, e := domain.NormalizeForeign(f)
	if e != nil {
		return old, e
	}
	f = normalized
	for _, account := range []string{f.FundingAccount, f.RepaymentAccount} {
		if a, ok := b.Accounts[account]; ok && !strings.HasPrefix(a.Code, "user.") {
			return old, domain.Invalid("实际账户不能使用系统科目")
		}
	}
	plan, e := accounting.Plan(b.Ledger, f, b.Accounts, b.system())
	if f.Amount == 0 && f.Kind == "UNKNOWN" && f.Foreign == nil {
		e = &accounting.Blocked{Reason: "AMOUNT_REQUIRED"}
	}
	if f.Kind == "CARD_REPAYMENT" && f.RepaymentAccount != "" {
		plan, e = accounting.Repayment(b.Ledger, f, b.Accounts[f.FundingAccount], b.Accounts[f.RepaymentAccount])
	}
	ev := domain.Event{ID: id, Revision: revision + 1, Facts: f, Status: "ACTIVE", EvidenceIDs: append([]string{}, old.EvidenceIDs...)}
	if e != nil {
		var blocked *accounting.Blocked
		if !errors.As(e, &blocked) {
			return old, e
		}
		ev.Reason = blocked.Reason
		ev.Status = "REVIEW_REQUIRED"
		if blocked.Reason == "HISTORICAL_ONLY" {
			ev.Status = "HISTORICAL_ONLY"
		}
		plan.Entries = nil
	}
	if len(plan.Entries) > 0 {
		ev.Posted = true
		if f.Kind == "CASH_OUT" || f.Kind == "CASH_IN" {
			ev.Status = "REVIEW_REQUIRED"
			ev.Reason = "PURPOSE_REQUIRED"
		}
	}
	// Never erase journals. A changed interpretation reverses exactly the prior
	// active journal, then creates the replacement under the same event identity.
	reversed := map[string]bool{}
	for _, j := range b.Journals {
		if j.ReversalOf != "" {
			reversed[j.ReversalOf] = true
		}
	}
	for _, j := range b.Journals {
		if j.Event != id || j.ReversalOf != "" || reversed[j.ID] {
			continue
		}
		if ev.Posted && j.At.Equal(plan.EffectiveAt) && reflect.DeepEqual(j.Entries, plan.Entries) {
			b.Events[id] = ev
			b.record(ev)
			return ev, nil
		}
		entries := []domain.Entry{}
		for _, v := range j.Entries {
			entries = append(entries, domain.Entry{AccountID: v.AccountID, Debit: v.Credit, Credit: v.Debit})
		}
		b.Journals = append(b.Journals, Journal{ID: domain.ID(), Event: id, Revision: ev.Revision, ReversalOf: j.ID, At: j.At, Entries: entries})
	}
	if ev.Posted {
		b.Journals = append(b.Journals, Journal{ID: domain.ID(), Event: id, Revision: ev.Revision, At: plan.EffectiveAt, Entries: plan.Entries})
	}
	b.Events[id] = ev
	b.record(ev)
	return ev, nil
}
func (b *Book) balances(before time.Time) ([]domain.Account, error) {
	sums := map[string]domain.Amount{}
	for _, j := range b.Journals {
		if !before.IsZero() && !j.At.Before(before) {
			continue
		}
		for _, v := range j.Entries {
			n, e := domain.Subtract(v.Debit, v.Credit)
			if e != nil {
				return nil, e
			}
			sums[v.AccountID], e = domain.Add(sums[v.AccountID], n)
			if e != nil {
				return nil, e
			}
		}
	}
	out := []domain.Account{}
	for id, a := range b.Accounts {
		a.Balance = sums[id]
		if a.Type == "LIABILITY" || a.Type == "INCOME" || a.Type == "EQUITY" {
			v, e := domain.Subtract(0, a.Balance)
			if e != nil {
				return nil, e
			}
			a.Balance = v
		}
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
