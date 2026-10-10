package store

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/Kirawii/qianben/backend/internal/domain"
	"github.com/jackc/pgx/v5"
)

type HistoricalAnalysis struct {
	Version          int64                `json:"source_version"`
	Algorithm        string               `json:"algorithm_version"`
	From             time.Time            `json:"from"`
	To               time.Time            `json:"to"`
	Cutover          time.Time            `json:"cutover_time"`
	GrossConsumption domain.Amount        `json:"gross_consumption_minor"`
	Refund           domain.Amount        `json:"refund_minor"`
	NetConsumption   domain.Amount        `json:"net_consumption_minor"`
	IncludedCount    int64                `json:"included_count"`
	ExcludedCount    int64                `json:"excluded_count"`
	Categories       []CategoryTotal      `json:"categories"`
	Excluded         []HistoricalExcluded `json:"excluded"`
}

type HistoricalExcluded struct {
	Kind   string        `json:"kind"`
	Count  int64         `json:"count"`
	Amount domain.Amount `json:"amount_minor"`
}

// Historical facts are analytical evidence; they never replace opening balances.
func (d *DB) HistoricalAnalysis(ctx context.Context, actor, ledger string, from, to time.Time) (HistoricalAnalysis, error) {
	r := HistoricalAnalysis{From: from, To: to, Algorithm: "historical-v2", Categories: []CategoryTotal{}, Excluded: []HistoricalExcluded{}}
	if !from.Before(to) {
		return r, domain.Invalid("历史分析时间范围无效")
	}
	tx, err := d.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return r, err
	}
	defer tx.Rollback(ctx)
	l, err := loadLedger(ctx, tx, actor, ledger, "")
	if err != nil {
		return r, err
	}
	r.Version, r.Cutover = l.Version, l.Cutover
	rows, err := tx.Query(ctx, `SELECT v.facts,COALESCE(original.facts->>'category',''),rel.id IS NOT NULL FROM qb.events e JOIN qb.event_revisions v ON v.id=e.current_revision_id LEFT JOIN qb.relations rel ON rel.ledger_id=e.ledger_id AND rel.from_event=e.id AND rel.type='REFUND_OF' AND rel.active LEFT JOIN qb.events parent ON parent.id=rel.to_event AND parent.ledger_id=e.ledger_id LEFT JOIN qb.event_revisions original ON original.id=parent.current_revision_id WHERE e.ledger_id=$1 AND e.status='HISTORICAL_ONLY' AND (v.facts->>'occurred_at')::timestamptz >= $2 AND (v.facts->>'occurred_at')::timestamptz < $3 AND (v.facts->>'occurred_at')::timestamptz <= $4`, ledger, from, to, l.Cutover)
	if err != nil {
		return r, err
	}
	defer rows.Close()
	categories := map[string]domain.Amount{}
	excluded := map[string]HistoricalExcluded{}
	for rows.Next() {
		var raw []byte
		var originalCategory string
		var linkedRefund bool
		if err = rows.Scan(&raw, &originalCategory, &linkedRefund); err != nil {
			return r, err
		}
		var f domain.Facts
		if err = json.Unmarshal(raw, &f); err != nil {
			return r, err
		}
		refund := linkedRefund && (f.Kind == "REFUND" || f.Kind == "ASSET_REFUND")
		if f.Currency == "CNY" && f.Amount > 0 && (f.Kind == "EXPENSE" || f.Kind == "ASSET_PURCHASE" || refund) {
			amount := f.Amount
			if refund {
				r.Refund, err = domain.Add(r.Refund, f.Amount)
				amount = -amount
			} else {
				r.GrossConsumption, err = domain.Add(r.GrossConsumption, f.Amount)
			}
			if err != nil {
				return r, err
			}
			category := f.Category
			if refund {
				category = originalCategory
			}
			if category == "" {
				category = "未分类"
			}
			categories[category], err = domain.Add(categories[category], amount)
			if err != nil {
				return r, err
			}
			r.IncludedCount++
		} else {
			kind := f.Kind
			if kind == "" {
				kind = "UNKNOWN"
			}
			x := excluded[kind]
			x.Kind = kind
			x.Count++
			if f.Currency == "CNY" {
				x.Amount, err = domain.Add(x.Amount, f.Amount)
				if err != nil {
					return r, err
				}
			}
			excluded[kind] = x
			r.ExcludedCount++
		}
	}
	if err = rows.Err(); err != nil {
		return r, err
	}
	r.NetConsumption, err = domain.Subtract(r.GrossConsumption, r.Refund)
	if err != nil {
		return r, err
	}
	for c, a := range categories {
		r.Categories = append(r.Categories, CategoryTotal{c, a})
	}
	for _, x := range excluded {
		r.Excluded = append(r.Excluded, x)
	}
	sort.Slice(r.Categories, func(i, j int) bool { return r.Categories[i].Category < r.Categories[j].Category })
	sort.Slice(r.Excluded, func(i, j int) bool { return r.Excluded[i].Kind < r.Excluded[j].Kind })
	return r, tx.Commit(ctx)
}
