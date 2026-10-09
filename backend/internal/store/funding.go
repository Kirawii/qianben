package store

import (
	"context"
	"encoding/json"
	"github.com/Kirawii/qianben/backend/internal/domain"
	"github.com/jackc/pgx/v5"
	"strings"
	"time"
)

type FundingPreferenceRequest struct {
	CommandID       string     `json:"command_id"`
	ExpectedVersion int64      `json:"expected_version"`
	Rail            string     `json:"rail"`
	AccountID       string     `json:"account_id"`
	EffectiveFrom   time.Time  `json:"effective_from"`
	EffectiveTo     *time.Time `json:"effective_to"`
	Note            string     `json:"note"`
}

// Preference points supersede earlier points; an expired or cleared point never revives one.
func (d *DB) SetFundingPreference(ctx context.Context, actor, ledger string, r FundingPreferenceRequest) (json.RawMessage, error) {
	r.EffectiveFrom = r.EffectiveFrom.Truncate(time.Microsecond)
	if r.EffectiveTo != nil {
		at := r.EffectiveTo.Truncate(time.Microsecond)
		r.EffectiveTo = &at
	}
	if (r.Rail != "WECHAT" && r.Rail != "ALIPAY" && r.Rail != "UNIONPAY") || r.EffectiveFrom.IsZero() || (r.EffectiveTo != nil && !r.EffectiveTo.After(r.EffectiveFrom)) || len(r.Note) > 500 || (r.AccountID != "" && !domain.IsUUID(r.AccountID)) {
		return nil, domain.Invalid("支付偏好字段无效")
	}
	return command(ctx, d, actor, ledger, r.CommandID, r, "FUNDING_PREFERENCE_SET", func(tx pgx.Tx, l domain.Ledger) (any, error) {
		if l.Version != r.ExpectedVersion {
			return nil, domain.Conflict("账本已更新，请刷新支付偏好后重试")
		}
		var account any
		if r.AccountID != "" {
			all, _, err := accountsTx(ctx, tx, l.ID)
			if err != nil {
				return nil, err
			}
			a, ok := all[r.AccountID]
			if !ok || !strings.HasPrefix(a.Code, "user.") {
				return nil, domain.Invalid("支付偏好须选本账本的实际账户")
			}
			account = r.AccountID
		}
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM qb.funding_relations WHERE ledger_id=$1 AND rail=$2 AND effective_from=$3 AND kind='DEFAULT_PREFERENCE')`, l.ID, r.Rail, r.EffectiveFrom).Scan(&exists); err != nil {
			return nil, err
		}
		if exists {
			return nil, domain.Conflict("该通道生效时点已有偏好记录，请选择不同时间")
		}
		id := domain.ID()
		_, err := tx.Exec(ctx, `INSERT INTO qb.funding_relations(id,ledger_id,kind,scope,account_id,rail,effective_from,effective_to,actor_id,source_version,note) VALUES($1,$2,'DEFAULT_PREFERENCE','RAIL_TIMELINE',$3,$4,$5,$6,$7,$8,$9)`, id, l.ID, account, r.Rail, r.EffectiveFrom, r.EffectiveTo, actor, l.Version+1, r.Note)
		return map[string]any{"id": id, "source_version": l.Version + 1}, err
	})
}

func recordUserFunding(ctx context.Context, tx pgx.Tx, l domain.Ledger, actor, event, obs string, f domain.Facts) error {
	all, _, err := accountsTx(ctx, tx, l.ID)
	if err != nil {
		return err
	}
	for leg, account := range map[string]string{"FUNDING": f.FundingAccount, "REPAYMENT": f.RepaymentAccount} {
		if account == "" {
			continue
		}
		a, ok := all[account]
		if !ok || !strings.HasPrefix(a.Code, "user.") {
			return domain.Invalid("确认的资金账户须为本账本实际账户")
		}
		_, err := tx.Exec(ctx, `INSERT INTO qb.funding_relations(id,ledger_id,kind,scope,account_id,event_id,event_revision_id,observation_id,leg,actor_id,source_version)
 SELECT $1,$2,'USER_CONFIRMED','EVENT_REVISION',$3,e.id,e.current_revision_id,$4,$5,$6,$7 FROM qb.events e WHERE e.ledger_id=$2 AND e.id=$8`, domain.ID(), l.ID, account, obs, leg, actor, l.Version+1, event)
		if err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) Funding(ctx context.Context, actor, ledger, event, rail string, at time.Time) (json.RawMessage, error) {
	if event != "" && !domain.IsUUID(event) {
		return nil, domain.Invalid("事件 ID 无效")
	}
	if rail != "" && rail != "WECHAT" && rail != "ALIPAY" && rail != "UNIONPAY" {
		return nil, domain.Invalid("支付通道无效")
	}
	if err := d.Authorized(ctx, actor, ledger); err != nil {
		return nil, err
	}
	if at.IsZero() {
		at = time.Now()
	}
	var result []byte
	err := d.Pool.QueryRow(ctx, `SELECT jsonb_build_object('source_version',l.version,'preferences',COALESCE((SELECT jsonb_agg(to_jsonb(p) ORDER BY p.effective_from DESC,p.id) FROM (SELECT f.id,f.rail,f.account_id,a.name AS account_name,f.effective_from,f.effective_to,f.note,f.source_version FROM qb.funding_relations f LEFT JOIN qb.accounts a ON a.id=f.account_id WHERE f.ledger_id=l.id AND f.kind='DEFAULT_PREFERENCE' ORDER BY f.effective_from DESC,f.id LIMIT 200) p),'[]'::jsonb),
 'candidate', (SELECT CASE WHEN p.account_id IS NOT NULL AND (p.effective_to IS NULL OR p.effective_to>$3) THEN jsonb_build_object('account_id',p.account_id,'preference_id',p.id,'kind','DEFAULT_PREFERENCE','proves_actual_funding',false) ELSE NULL END FROM qb.funding_relations p WHERE p.ledger_id=l.id AND p.kind='DEFAULT_PREFERENCE' AND p.rail=$4 AND p.effective_from<=$3 ORDER BY p.effective_from DESC LIMIT 1),
 'proofs',COALESCE((SELECT jsonb_agg(jsonb_build_object('id',f.id,'kind',f.kind,'scope',f.scope,'account_id',f.account_id,'account_name',a.name,'leg',f.leg,'revision',r.number,'observation_id',f.observation_id,'source_version',f.source_version,'created_at',f.created_at,'current_revision',f.event_revision_id=e.current_revision_id) ORDER BY r.number DESC,f.leg) FROM qb.funding_relations f JOIN qb.accounts a ON a.id=f.account_id JOIN qb.event_revisions r ON r.id=f.event_revision_id JOIN qb.events e ON e.id=f.event_id WHERE f.ledger_id=l.id AND f.event_id=$2::uuid),'[]'::jsonb)) FROM qb.ledgers l WHERE l.id=$1`, ledger, nullableUUID(event), at, rail).Scan(&result)
	return json.RawMessage(result), err
}

func nullableUUID(id string) any {
	if id == "" {
		return nil
	}
	return id
}
