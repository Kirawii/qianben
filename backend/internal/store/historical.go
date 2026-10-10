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
	r := HistoricalAnalysis{From: from, To: to, Algorithm: "historical-v1", Categories: []CategoryTotal{}, Excluded: []HistoricalExcluded{}}
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
	rows, err := tx.Query(ctx, `SELECT v.facts FROM qb.events e JOIN qb.event_revisions v ON v.id=e.current_revision_id WHERE e.ledger_id=$1 AND e.status='HISTORICAL_ONLY' AND (v.facts->>'occurred_at')::timestamptz >= $2 AND (v.facts->>'occurred_at')::timestamptz < $3 AND (v.facts->>'occurred_at')::timestamptz <= $4`, ledger, from, to, l.Cutover)
	if err != nil {
		return r, err
	}
	defer rows.Close()
	categories := map[string]domain.Amount{}
	excluded := map[string]HistoricalExcluded{}
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return r, err
		}
		var f domain.Facts
		if err = json.Unmarshal(raw, &f); err != nil {
			return r, err
		}
		if f.Currency == "CNY" && f.Amount > 0 && (f.Kind == "EXPENSE" || f.Kind == "ASSET_PURCHASE") {
			r.GrossConsumption, err = domain.Add(r.GrossConsumption, f.Amount)
			if err != nil {
				return r, err
			}
			category := f.Category
			if category == "" {
				category = "未分类"
			}
			categories[category], err = domain.Add(categories[category], f.Amount)
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
