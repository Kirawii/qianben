package ondevice

import (
	"github.com/Kirawii/qianben/backend/internal/domain"
	"strconv"
	"time"
)

func (b *Book) record(ev domain.Event) {
	if b.HistoryTimes == nil {
		b.HistoryTimes = map[string][]time.Time{}
		b.HistoryVersions = map[string][]int64{}
	}
	// Early development snapshots may not have audit metadata. Keep their missing
	// values explicit; never reconstruct system time from economic time.
	for len(b.HistoryTimes[ev.ID]) < len(b.History[ev.ID]) {
		b.HistoryTimes[ev.ID] = append(b.HistoryTimes[ev.ID], time.Time{})
		b.HistoryVersions[ev.ID] = append(b.HistoryVersions[ev.ID], 0)
	}
	b.History[ev.ID] = append(b.History[ev.ID], ev)
	b.HistoryTimes[ev.ID] = append(b.HistoryTimes[ev.ID], time.Now().UTC())
	b.HistoryVersions[ev.ID] = append(b.HistoryVersions[ev.ID], b.Ledger.Version+1)
}
func (b *Book) history(id, before string) (any, error) {
	if !domain.IsUUID(id) {
		return nil, domain.Invalid("事件 ID 无效")
	}
	max := int64(0)
	if before != "" {
		var e error
		max, e = strconv.ParseInt(before, 10, 64)
		if e != nil || max <= 0 {
			return nil, domain.Invalid("修订游标无效")
		}
	}
	out := []map[string]any{}
	all := b.History[id]
	for index := len(all) - 1; index >= 0 && len(out) < 100; index-- {
		ev := all[index]
		if max > 0 && ev.Revision >= max {
			continue
		}
		created := ""
		version := int64(0)
		if index < len(b.HistoryTimes[id]) && !b.HistoryTimes[id][index].IsZero() {
			created = b.HistoryTimes[id][index].Format(time.RFC3339Nano)
			version = b.HistoryVersions[id][index]
		}
		journals := []map[string]any{}
		var accountingRevision any
		for _, j := range b.Journals {
			if j.Event != id {
				continue
			}
			if j.Revision <= ev.Revision && j.ReversalOf == "" && ev.Posted {
				accountingRevision = j.Revision
			}
			if j.Revision != ev.Revision {
				continue
			}
			kind := "ECONOMIC"
			if j.ReversalOf != "" {
				kind = "REVERSAL"
			}
			var reversedBy any
			for _, next := range b.Journals {
				if next.ReversalOf == j.ID {
					reversedBy = next.ID
					break
				}
			}
			entries := []map[string]any{}
			for _, line := range j.Entries {
				entries = append(entries, map[string]any{"account": b.Accounts[line.AccountID].Name, "debit_minor": line.Debit, "credit_minor": line.Credit})
			}
			journals = append(journals, map[string]any{"id": j.ID, "kind": kind, "reversed_by": reversedBy, "entries": entries})
		}
		out = append(out, map[string]any{"revision": ev.Revision, "created_at": created, "source_version": version, "facts": ev.Facts, "evidence_ids": ev.EvidenceIDs, "accounting_revision": accountingRevision, "journals": journals})
	}
	return out, nil
}
