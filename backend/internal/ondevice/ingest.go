package ondevice

import (
	"encoding/json"
	"github.com/Kirawii/qianben/backend/internal/domain"
	"github.com/Kirawii/qianben/backend/internal/evidence"
)

func (b *Book) ingest(body json.RawMessage) (any, error) {
	var batch struct {
		Items []domain.Delivery `json:"items"`
	}
	if e := json.Unmarshal(body, &batch); e != nil {
		return nil, e
	}
	if len(batch.Items) == 0 || len(batch.Items) > 50 {
		return nil, domain.Invalid("通知批次无效")
	}
	if b.Observations == nil {
		b.Observations = map[string]domain.Delivery{}
		b.Snapshots = map[string]string{}
		b.Sources = map[string]string{}
	}
	allowed := map[string]bool{"com.tencent.mm": true, "com.eg.android.AlipayGphone": true, "com.unionpay": true, "com.icbc": true, "com.chinamworld.main": true, "com.chinamworld.bocmbci": true, "com.android.bankabc": true, "cmb.pb": true, "com.bankcomm.Bankcomm": true, "com.psbc.mbank": true}
	acks := []domain.Ack{}
	changed := false
	for _, d := range batch.Items {
		ack := domain.Ack{DeliveryID: d.DeliveryID}
		if !allowed[d.Package] || !evidence.Candidate(d) {
			ack.Status = "IGNORED"
			acks = append(acks, ack)
			continue
		}
		if !domain.IsUUID(d.DeliveryID) || d.DeviceID == "" || d.SourceObjectKey == "" || d.SnapshotKey == "" {
			return nil, domain.Invalid("本机通知身份无效")
		}
		obsKey := d.DeviceID + ":" + d.DeliveryID
		if old, ok := b.Observations[obsKey]; ok {
			if string(domain.Hashable(old)) != string(domain.Hashable(d)) {
				return nil, domain.Conflict("同一通知 delivery 的内容不同")
			}
			ack.Status = "ACK"
			ack.ObservationID = obsKey
			acks = append(acks, ack)
			continue
		}
		source := d.DeviceID + ":" + d.SourceIdentity + ":" + d.SourceObjectKey
		snapshot := source + ":" + d.SnapshotKey
		if oldKey := b.Snapshots[snapshot]; oldKey != "" {
			old := b.Observations[oldKey]
			// Delivery retry envelopes may differ; the immutable snapshot may not.
			old.DeliveryID = ""
			old.ObservedAt = d.ObservedAt
			compare := d
			compare.DeliveryID = ""
			if string(domain.Hashable(old)) != string(domain.Hashable(compare)) {
				return nil, domain.Conflict("同一通知快照内容不同")
			}
			b.Observations[obsKey] = d
			ack.Status = "ACK"
			ack.ObservationID = oldKey
			acks = append(acks, ack)
			changed = true
			continue
		}
		id := b.Sources[source]
		if id == "" {
			id = domain.ID()
			facts, _ := evidence.Parse(d)
			ev, e := b.save(id, 0, facts)
			if e != nil {
				return nil, e
			}
			ev.EvidenceIDs = []string{obsKey}
			b.Events[id] = ev
			b.History[id][0] = ev
			b.Sources[source] = id
		} else {
			// A source update cannot silently replace user-accepted financial facts.
			ev := b.Events[id]
			ev.Revision++
			ev.EvidenceIDs = append(ev.EvidenceIDs, obsKey)
			ev.Status = "REVIEW_REQUIRED"
			ev.Reason = "EVIDENCE_CHANGED"
			b.Events[id] = ev
			b.record(ev)
		}
		b.Observations[obsKey] = d
		b.Snapshots[snapshot] = obsKey
		changed = true
		ack.Status = "ACK"
		ack.ObservationID = obsKey
		acks = append(acks, ack)
	}
	if changed {
		b.Ledger.Version++
	}
	return acks, nil
}
