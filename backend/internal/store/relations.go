package store

import (
	"context"
	"encoding/json"
	"github.com/Kirawii/qianben/backend/internal/domain"
	"github.com/jackc/pgx/v5"
)

type MergeRequest struct {
	CommandID      string `json:"command_id"`
	SourceID       string `json:"source_id"`
	TargetID       string `json:"target_id"`
	SourceRevision int64  `json:"source_revision"`
	TargetRevision int64  `json:"target_revision"`
}

func unlinked(ctx context.Context, tx pgx.Tx, l, id string) error {
	var yes bool
	e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM qb.relations WHERE ledger_id=$1 AND active AND type IN ('REFUND_OF','TRANSFER_LEG_OF','REIMBURSES') AND (from_event=$2 OR to_event=$2))`, l, id).Scan(&yes)
	if e != nil {
		return e
	}
	if yes {
		return domain.Conflict("事件有退款或划转关系，请先处理关联")
	}
	return nil
}
func (d *DB) Merge(ctx context.Context, actor, ledger string, r MergeRequest) (json.RawMessage, error) {
	if !domain.IsUUID(r.SourceID) || !domain.IsUUID(r.TargetID) || r.SourceID == r.TargetID {
		return nil, domain.Invalid("合并事件 ID 无效")
	}
	return command(ctx, d, actor, ledger, r.CommandID, r, "EVENT_MERGED", func(tx pgx.Tx, l domain.Ledger) (any, error) {
		source, e := eventTx(ctx, tx, l.ID, r.SourceID)
		if e != nil {
			return nil, e
		}
		target, e := eventTx(ctx, tx, l.ID, r.TargetID)
		if e != nil {
			return nil, e
		}
		if source.Revision != r.SourceRevision || target.Revision != r.TargetRevision || source.Status == "MERGED" || source.Status == "SPLIT" || target.Status == "MERGED" || target.Status == "SPLIT" {
			return nil, domain.Conflict("事件版本已改变")
		}
		for _, id := range []string{source.ID, target.ID} {
			if e = unlinked(ctx, tx, l.ID, id); e != nil {
				return nil, e
			}
		}
		f := source.Facts
		f.Kind = "UNKNOWN"
		if _, e = writeEvent(ctx, tx, l, source.ID, source.Revision, f, source.EvidenceIDs); e != nil {
			return nil, e
		}
		ev, e := writeEvent(ctx, tx, l, target.ID, target.Revision, target.Facts, append(target.EvidenceIDs, source.EvidenceIDs...))
		if e != nil {
			return nil, e
		}
		_, e = tx.Exec(ctx, `UPDATE qb.events SET status='MERGED',canonical_id=$3,review_reason='' WHERE ledger_id=$1 AND id=$2`, l.ID, source.ID, target.ID)
		if e != nil {
			return nil, e
		}
		_, e = tx.Exec(ctx, `UPDATE qb.evidence_links SET event_id=$3 WHERE ledger_id=$1 AND event_id=$2`, l.ID, source.ID, target.ID)
		if e != nil {
			return nil, e
		}
		_, e = tx.Exec(ctx, `INSERT INTO qb.relations(id,ledger_id,from_event,to_event,type,committed_version) VALUES($1,$2,$3,$4,'MERGED_INTO',$5)`, domain.ID(), l.ID, source.ID, target.ID, l.Version+1)
		return ev, e
	})
}

type SplitRequest struct {
	CommandID        string         `json:"command_id"`
	SourceID         string         `json:"source_id"`
	ExpectedRevision int64          `json:"expected_revision"`
	Children         []domain.Facts `json:"children"`
}

func (d *DB) Split(ctx context.Context, actor, ledger string, r SplitRequest) (json.RawMessage, error) {
	if !domain.IsUUID(r.SourceID) || len(r.Children) < 2 || len(r.Children) > 20 {
		return nil, domain.Invalid("拆分须包含 2 至 20 个子事件")
	}
	return command(ctx, d, actor, ledger, r.CommandID, r, "EVENT_SPLIT", func(tx pgx.Tx, l domain.Ledger) (any, error) {
		source, e := eventTx(ctx, tx, l.ID, r.SourceID)
		if e != nil {
			return nil, e
		}
		if source.Revision != r.ExpectedRevision || source.Status == "MERGED" || source.Status == "SPLIT" {
			return nil, domain.Conflict("事件版本已改变")
		}
		if e = unlinked(ctx, tx, l.ID, source.ID); e != nil {
			return nil, e
		}
		var sum domain.Amount
		for _, f := range r.Children {
			if f.Amount <= 0 {
				return nil, domain.Invalid("子事件金额须为正数")
			}
			sum, e = domain.Add(sum, f.Amount)
			if e != nil {
				return nil, e
			}
		}
		if sum != source.Facts.Amount {
			return nil, domain.Invalid("拆分金额合计须等于原金额")
		}
		f := source.Facts
		f.Kind = "UNKNOWN"
		if _, e = writeEvent(ctx, tx, l, source.ID, source.Revision, f, source.EvidenceIDs); e != nil {
			return nil, e
		}
		result := []domain.Event{}
		for _, child := range r.Children {
			id := domain.ID()
			_, e = tx.Exec(ctx, `INSERT INTO qb.events(id,ledger_id) VALUES($1,$2)`, id, l.ID)
			if e != nil {
				return nil, e
			}
			ev, err := writeEvent(ctx, tx, l, id, 0, child, source.EvidenceIDs)
			if err != nil {
				return nil, err
			}
			if !ev.Posted {
				return nil, domain.Invalid("子事件须具备完整入账信息")
			}
			result = append(result, ev)
			_, e = tx.Exec(ctx, `INSERT INTO qb.relations(id,ledger_id,from_event,to_event,type,allocation,committed_version) VALUES($1,$2,$3,$4,'SPLIT_FROM',$5,$6)`, domain.ID(), l.ID, id, source.ID, int64(child.Amount), l.Version+1)
			if e != nil {
				return nil, e
			}
		}
		_, e = tx.Exec(ctx, `UPDATE qb.events SET status='SPLIT',review_reason='' WHERE ledger_id=$1 AND id=$2`, l.ID, source.ID)
		return result, e
	})
}

type TransferRequest struct {
	CommandID   string `json:"command_id"`
	OutID       string `json:"out_id"`
	InID        string `json:"in_id"`
	OutRevision int64  `json:"out_revision"`
	InRevision  int64  `json:"in_revision"`
}

func (d *DB) LinkTransfer(ctx context.Context, actor, ledger string, r TransferRequest) (json.RawMessage, error) {
	if !domain.IsUUID(r.OutID) || !domain.IsUUID(r.InID) || r.OutID == r.InID {
		return nil, domain.Invalid("转账事件 ID 无效")
	}
	return command(ctx, d, actor, ledger, r.CommandID, r, "TRANSFER_LINKED", func(tx pgx.Tx, l domain.Ledger) (any, error) {
		a, e := eventTx(ctx, tx, l.ID, r.OutID)
		if e != nil {
			return nil, e
		}
		b, e := eventTx(ctx, tx, l.ID, r.InID)
		if e != nil {
			return nil, e
		}
		if a.Revision != r.OutRevision || b.Revision != r.InRevision {
			return nil, domain.Conflict("事件版本已改变")
		}
		if !a.Posted || !b.Posted || a.Facts.Kind != "TRANSFER_OUT" || b.Facts.Kind != "TRANSFER_IN" || a.Facts.Amount != b.Facts.Amount || a.Facts.FundingAccount == b.Facts.FundingAccount {
			return nil, domain.Invalid("转账须有不同账户的等额转出转入")
		}
		for _, id := range []string{a.ID, b.ID} {
			if e = unlinked(ctx, tx, l.ID, id); e != nil {
				return nil, e
			}
		}
		id := domain.ID()
		_, e = tx.Exec(ctx, `INSERT INTO qb.relations(id,ledger_id,from_event,to_event,type,allocation,committed_version) VALUES($1,$2,$3,$4,'TRANSFER_LEG_OF',$5,$6)`, id, l.ID, a.ID, b.ID, int64(a.Facts.Amount), l.Version+1)
		return map[string]string{"relation_id": id}, e
	})
}
func (d *DB) DeleteLedger(ctx context.Context, actor, ledger, confirm string) error {
	if !domain.IsUUID(ledger) {
		return domain.Invalid("账本 ID 无效")
	}
	tx, e := d.Pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if e = actorTx(ctx, tx, actor); e != nil {
		return e
	}
	l, e := loadLedger(ctx, tx, actor, ledger, "FOR UPDATE")
	if e != nil {
		return e
	}
	if confirm != l.Name {
		return domain.Invalid("请输入完整账本名称确认删除")
	}
	_, e = tx.Exec(ctx, `SELECT qb.purge_ledger($1)`, ledger)
	if e != nil {
		return e
	}
	return tx.Commit(ctx)
}
