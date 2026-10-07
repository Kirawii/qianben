package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Kirawii/qianben/backend/internal/domain"
	"github.com/Kirawii/qianben/backend/internal/evidence"
	"github.com/jackc/pgx/v5"
)

var AllowedPackages = map[string]bool{"com.tencent.mm": true, "com.eg.android.AlipayGphone": true, "com.unionpay": true, "com.icbc": true, "com.chinamworld.main": true, "com.chinamworld.bocmbci": true, "com.android.bankabc": true, "cmb.pb": true, "com.bankcomm.Bankcomm": true, "com.psbc.mbank": true}

func (d *DB) Ingest(ctx context.Context, actor, ledger string, batch []domain.Delivery) ([]domain.Ack, error) {
	if len(batch) == 0 || len(batch) > 50 {
		return nil, domain.Invalid("每批应包含 1 至 50 条通知")
	}
	acks := []domain.Ack{}
	for _, item := range batch {
		ack, e := d.ingestOne(ctx, actor, ledger, item)
		if e != nil {
			var f *domain.Fault
			if errors.As(e, &f) && f.Status == 422 {
				acks = append(acks, domain.Ack{DeliveryID: item.DeliveryID, Status: "REJECTED", Reason: f.Message})
				continue
			}
			return nil, e
		}
		acks = append(acks, ack)
	}
	return acks, nil
}
func (d *DB) ingestOne(ctx context.Context, actor, ledger string, item domain.Delivery) (domain.Ack, error) {
	ack := domain.Ack{DeliveryID: item.DeliveryID}
	if !domain.IsUUID(ledger) || !textLimit(item.DeviceID, 128) || !textLimit(item.DeliveryID, 128) || !textLimit(item.SourceObjectKey, 512) || !textLimit(item.SnapshotKey, 128) || item.CaptureSequence < 1 || item.ObservedAt.IsZero() || len(item.Title)+len(item.Text)+len(item.BigText) > 24000 || !AllowedPackages[item.Package] || item.SourceIdentity != "android.notification:"+item.Package {
		return ack, domain.Invalid("通知来源或字段无效")
	}
	if !evidence.Candidate(item) {
		ack.Status = "IGNORED"
		return ack, d.Authorized(ctx, actor, ledger)
	}
	payload := domain.Hashable(item)
	hash := Hash(payload)
	snapshotHash := Hash(domain.Hashable(map[string]any{"package": item.Package, "title": item.Title, "text": item.Text, "big_text": item.BigText, "posted_at": item.NotificationPostedAt, "availability": item.Availability, "summary": item.GroupSummary}))
	tx, e := d.Pool.Begin(ctx)
	if e != nil {
		return ack, e
	}
	defer tx.Rollback(ctx)
	if _, e = loadLedger(ctx, tx, actor, ledger, "FOR SHARE"); e != nil {
		return ack, e
	}
	var priorHash, id string
	e = tx.QueryRow(ctx, `SELECT payload_hash,observation_id::text FROM qb.deliveries WHERE ledger_id=$1 AND device_id=$2 AND delivery_id=$3`, ledger, item.DeviceID, item.DeliveryID).Scan(&priorHash, &id)
	if e == nil {
		if hash != priorHash {
			return ack, domain.Invalid("同一 delivery_id 的内容发生改变")
		}
		ack.Status = "ACK"
		ack.ObservationID = id
		return ack, nil
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return ack, e
	}
	source := domain.ID()
	_, e = tx.Exec(ctx, `INSERT INTO qb.source_events(id,ledger_id,source_identity,object_key) VALUES($1,$2,$3,$4) ON CONFLICT(ledger_id,source_identity,object_key) DO NOTHING`, source, ledger, item.SourceIdentity, item.DeviceID+":"+item.SourceObjectKey)
	if e != nil {
		return ack, e
	}
	var version int64
	e = tx.QueryRow(ctx, `SELECT id::text,version FROM qb.source_events WHERE ledger_id=$1 AND source_identity=$2 AND object_key=$3 FOR UPDATE`, ledger, item.SourceIdentity, item.DeviceID+":"+item.SourceObjectKey).Scan(&source, &version)
	if e != nil {
		return ack, e
	}
	e = tx.QueryRow(ctx, `SELECT payload_hash,observation_id::text FROM qb.deliveries WHERE ledger_id=$1 AND device_id=$2 AND delivery_id=$3`, ledger, item.DeviceID, item.DeliveryID).Scan(&priorHash, &id)
	if e == nil {
		if hash != priorHash {
			return ack, domain.Invalid("同一 delivery_id 的内容发生改变")
		}
		ack.Status = "ACK"
		ack.ObservationID = id
		return ack, nil
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return ack, e
	}
	e = tx.QueryRow(ctx, `SELECT id::text,payload_hash FROM qb.observations WHERE source_event_id=$1 AND snapshot_key=$2`, source, item.SnapshotKey).Scan(&id, &priorHash)
	if errors.Is(e, pgx.ErrNoRows) {
		id = domain.ID()
		version++
		_, e = tx.Exec(ctx, `INSERT INTO qb.observations(id,ledger_id,source_event_id,snapshot_key,version,payload_hash,payload) VALUES($1,$2,$3,$4,$5,$6,$7)`, id, ledger, source, item.SnapshotKey, version, snapshotHash, payload)
		if e != nil {
			return ack, e
		}
		_, e = tx.Exec(ctx, `UPDATE qb.source_events SET version=$2 WHERE id=$1`, source, version)
		if e != nil {
			return ack, e
		}
		_, e = tx.Exec(ctx, `INSERT INTO qb.outbox(ledger_id,kind,payload) VALUES($1,'EVIDENCE_RECEIVED',$2)`, ledger, domain.Hashable(map[string]string{"observation_id": id}))
		if e != nil {
			return ack, e
		}
	} else if e != nil {
		return ack, e
	} else if snapshotHash != priorHash {
		return ack, domain.Invalid("同一 snapshot_key 的内容发生改变")
	}
	_, e = tx.Exec(ctx, `INSERT INTO qb.deliveries(ledger_id,device_id,delivery_id,payload_hash,observation_id) VALUES($1,$2,$3,$4,$5)`, ledger, item.DeviceID, item.DeliveryID, hash, id)
	if e != nil {
		return ack, e
	}
	if e = tx.Commit(ctx); e != nil {
		return ack, e
	}
	ack.Status = "ACK"
	ack.ObservationID = id
	return ack, nil
}

// Work consumes one durable outbox item entirely inside a transaction. There is
// no external side effect or lease: crash rollback makes the item claimable again.
func (d *DB) Work(ctx context.Context) (bool, error) {
	var outbox, ledger, actor string
	e := d.Pool.QueryRow(ctx, `SELECT o.id::text,o.ledger_id::text,l.user_id::text FROM qb.outbox o JOIN qb.ledgers l ON l.id=o.ledger_id WHERE NOT o.dispatched ORDER BY o.created_at LIMIT 1`).Scan(&outbox, &ledger, &actor)
	if errors.Is(e, pgx.ErrNoRows) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	tx, e := d.Pool.Begin(ctx)
	if e != nil {
		return false, e
	}
	defer tx.Rollback(ctx)
	if e = actorTx(ctx, tx, actor); e != nil {
		return false, e
	}
	l, e := loadLedger(ctx, tx, actor, ledger, "FOR UPDATE")
	if e != nil {
		var fault *domain.Fault
		if errors.As(e, &fault) && (fault.Status == 404 || fault.Status == 409) {
			return true, nil
		}
		return false, e
	}
	var kind string
	var payload []byte
	e = tx.QueryRow(ctx, `SELECT kind,payload FROM qb.outbox WHERE id=$1 AND NOT dispatched FOR UPDATE SKIP LOCKED`, outbox).Scan(&kind, &payload)
	if errors.Is(e, pgx.ErrNoRows) {
		return true, nil
	}
	if e != nil {
		return false, e
	}
	if kind == "EVIDENCE_RECEIVED" {
		var body map[string]string
		if e = json.Unmarshal(payload, &body); e != nil {
			return false, e
		}
		observation := body["observation_id"]
		var source string
		var raw []byte
		e = tx.QueryRow(ctx, `SELECT source_event_id::text,payload FROM qb.observations WHERE ledger_id=$1 AND id=$2`, ledger, observation).Scan(&source, &raw)
		if e != nil {
			return false, e
		}
		var delivery domain.Delivery
		if e = json.Unmarshal(raw, &delivery); e != nil {
			return false, e
		}
		f, _ := evidence.Parse(delivery)
		var category string
		var ruleVersion int64
		ruleErr := tx.QueryRow(ctx, `SELECT category,version FROM qb.rules WHERE ledger_id=$1 AND merchant=$2 AND active AND effective_from<=$3 ORDER BY version DESC LIMIT 1`, ledger, f.Merchant, delivery.ObservedAt).Scan(&category, &ruleVersion)
		if ruleErr == nil {
			f.Category = category
		} else if !errors.Is(ruleErr, pgx.ErrNoRows) {
			return false, ruleErr
		}
		id := domain.ID()
		var revision int64
		ids := []string{observation}
		e = tx.QueryRow(ctx, `SELECT event_id::text FROM qb.evidence_links WHERE source_event_id=$1 LIMIT 1`, source).Scan(&id)
		existing := e == nil
		terminalStatus := ""
		if existing {
			old, err := eventTx(ctx, tx, ledger, id)
			if err != nil {
				return false, err
			}
			revision = old.Revision
			if old.Status == "SPLIT" || old.Status == "MERGED" {
				terminalStatus = old.Status
			}
			f = old.Facts
			ids = append(old.EvidenceIDs, observation)
		} else if errors.Is(e, pgx.ErrNoRows) {
			_, e = tx.Exec(ctx, `INSERT INTO qb.events(id,ledger_id) VALUES($1,$2)`, id, ledger)
		}
		if e != nil && !existing {
			return false, e
		}
		ev, err := writeEvent(ctx, tx, l, id, revision, f, ids, fmt.Sprintf("core-v1;parser=%s;mapping=%d", evidence.ParserVersion, ruleVersion))
		if err != nil {
			return false, err
		}
		if existing {
			status := "REVIEW_REQUIRED"
			if terminalStatus != "" {
				status = terminalStatus
			}
			_, e = tx.Exec(ctx, `UPDATE qb.events SET status=$2,review_reason='NEW_EVIDENCE_REVIEW' WHERE id=$1`, id, status)
			if e != nil {
				return false, e
			}
			ev.Status = status
			ev.Reason = "NEW_EVIDENCE_REVIEW"
		}
		_, e = tx.Exec(ctx, `INSERT INTO qb.evidence_links(ledger_id,source_event_id,event_id,observation_id) VALUES($1,$2,$3,$4)`, ledger, source, id, observation)
		if e != nil {
			return false, e
		}
		_, e = tx.Exec(ctx, `UPDATE qb.ledgers SET version=version+1 WHERE id=$1`, ledger)
		if e != nil {
			return false, e
		}
		_, e = tx.Exec(ctx, `INSERT INTO qb.changes(ledger_id,version,command_id,kind,data) VALUES($1,$2,$3,'EVIDENCE_INTERPRETED',$4)`, ledger, l.Version+1, "evidence:"+observation, domain.Hashable(ev))
		if e != nil {
			return false, e
		}
	}
	_, e = tx.Exec(ctx, `UPDATE qb.outbox SET dispatched=true WHERE id=$1`, outbox)
	if e != nil {
		return false, e
	}
	return true, tx.Commit(ctx)
}
