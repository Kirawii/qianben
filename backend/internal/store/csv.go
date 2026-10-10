package store

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"github.com/Kirawii/qianben/backend/internal/domain"
	"github.com/jackc/pgx/v5"
	"io"
	"strconv"
	"strings"
	"time"
)

type CSVRequest struct {
	CommandID string `json:"command_id"`
	AccountID string `json:"account_id"`
	Content   string `json:"content"`
}
type csvLine struct {
	Record string
	Facts  domain.Facts
	Raw    []string
}

func parseCSV(r CSVRequest) ([]csvLine, error) {
	if !domain.IsUUID(r.AccountID) || len(r.Content) > 800000 {
		return nil, domain.Invalid("CSV 账户或文件大小无效")
	}
	reader := csv.NewReader(strings.NewReader(strings.TrimPrefix(r.Content, "\ufeff")))
	header, e := reader.Read()
	if e != nil {
		return nil, domain.Invalid("CSV 表头无效")
	}
	expected := []string{"record_id", "occurred_at", "amount_minor", "kind", "merchant", "category"}
	if strings.Join(header, ",") != strings.Join(expected, ",") {
		return nil, domain.Invalid("CSV 表头须为 record_id,occurred_at,amount_minor,kind,merchant,category")
	}
	result := []csvLine{}
	seen := map[string]bool{}
	for {
		row, e := reader.Read()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, domain.Invalid("CSV 行格式无效")
		}
		if len(result) >= 2000 || !textLimit(row[0], 128) || seen[row[0]] {
			return nil, domain.Invalid("CSV 超过 2000 行或 record_id 重复")
		}
		seen[row[0]] = true
		at, e := time.Parse(time.RFC3339, row[1])
		if e != nil {
			return nil, domain.Invalid(fmt.Sprintf("第 %d 行时间无效", len(result)+2))
		}
		n, e := strconv.ParseInt(row[2], 10, 64)
		if e != nil || n <= 0 || n > 9000000000000000 {
			return nil, domain.Invalid("CSV 金额须为正整数分")
		}
		switch row[3] {
		case "EXPENSE", "INCOME", "CASH_OUT", "CASH_IN", "TRANSFER_OUT", "TRANSFER_IN", "ADVANCE", "REIMBURSEMENT":
		default:
			return nil, domain.Invalid("CSV 类型不支持，请将退款或资产先导入为待查款")
		}
		if len(row[4]) > 200 || len(row[5]) > 100 {
			return nil, domain.Invalid("CSV 商户或分类过长")
		}
		result = append(result, csvLine{row[0], domain.Facts{Kind: row[3], Amount: domain.Amount(n), Currency: "CNY", OccurredAt: &at, TimePrecision: "EXACT", FundingAccount: r.AccountID, Merchant: row[4], Category: row[5]}, row})
	}
	if len(result) == 0 {
		return nil, domain.Invalid("CSV 没有数据行")
	}
	return result, nil
}
func (d *DB) ImportCSV(ctx context.Context, actor, ledger string, r CSVRequest) (json.RawMessage, error) {
	lines, e := parseCSV(r)
	if e != nil {
		return nil, e
	}
	return d.importCSVLines(ctx, actor, ledger, r, lines, r, "QIANBEN_CSV_V1", "", Hash([]byte(r.Content)))
}

func (d *DB) importCSVLines(ctx context.Context, actor, ledger string, r CSVRequest, lines []csvLine, request any, format, sourceIdentity, fileHash string) (json.RawMessage, error) {
	return command(ctx, d, actor, ledger, r.CommandID, request, "CSV_IMPORTED", func(tx pgx.Tx, l domain.Ledger) (any, error) {
		all, _, e := accountsTx(ctx, tx, l.ID)
		if e != nil {
			return nil, e
		}
		a, ok := all[r.AccountID]
		if !ok || !strings.HasPrefix(a.Code, "user.") {
			return nil, domain.Invalid("导入账户无效")
		}
		if native, ok := request.(NativeCSVImportRequest); ok {
			for _, account := range native.Mappings {
				mapped, exists := all[account]
				if !exists || !strings.HasPrefix(mapped.Code, "user.") {
					return nil, domain.Invalid("支付方式映射账户无效")
				}
			}
		}
		importID := domain.ID()
		var exists string
		e = tx.QueryRow(ctx, `SELECT id::text FROM qb.statement_imports WHERE ledger_id=$1 AND account_id=$2 AND file_hash=$3`, l.ID, a.ID, fileHash).Scan(&exists)
		if e == nil {
			return map[string]any{"import_id": exists, "duplicate": true}, nil
		}
		if e != pgx.ErrNoRows {
			return nil, e
		}
		_, e = tx.Exec(ctx, `INSERT INTO qb.statement_imports(id,ledger_id,account_id,file_hash,format,rows_count) VALUES($1,$2,$3,$4,$5,$6)`, importID, l.ID, a.ID, fileHash, format, len(lines))
		if e != nil {
			return nil, e
		}
		imported := 0
		for _, line := range lines {
			identity := "csv.account:" + a.ID
			if sourceIdentity != "" {
				identity = sourceIdentity
			}
			if line.Facts.FundingAccount != "" {
				mapped, exists := all[line.Facts.FundingAccount]
				if !exists || !strings.HasPrefix(mapped.Code, "user.") {
					return nil, domain.Invalid("支付方式映射账户无效")
				}
			}
			var source, id, oldHash string
			raw := domain.Hashable(line.Raw)
			e = tx.QueryRow(ctx, `SELECT s.id::text,o.payload_hash,el.event_id::text FROM qb.source_events s JOIN qb.observations o ON o.source_event_id=s.id JOIN qb.evidence_links el ON el.observation_id=o.id WHERE s.ledger_id=$1 AND s.source_identity=$2 AND s.object_key=$3`, l.ID, identity, line.Record).Scan(&source, &oldHash, &id)
			if e == nil {
				if oldHash != Hash(raw) {
					return nil, domain.Conflict("CSV 同一 record_id 的内容不同")
				}
			} else if e == pgx.ErrNoRows {
				source = domain.ID()
				obs := domain.ID()
				id = domain.ID()
				_, e = tx.Exec(ctx, `INSERT INTO qb.source_events(id,ledger_id,source_identity,object_key,version) VALUES($1,$2,$3,$4,1)`, source, l.ID, identity, line.Record)
				if e != nil {
					return nil, e
				}
				_, e = tx.Exec(ctx, `INSERT INTO qb.observations(id,ledger_id,source_event_id,snapshot_key,version,payload_hash,payload) VALUES($1,$2,$3,'1',1,$4,$5)`, obs, l.ID, source, Hash(raw), raw)
				if e != nil {
					return nil, e
				}
				_, e = tx.Exec(ctx, `INSERT INTO qb.events(id,ledger_id) VALUES($1,$2)`, id, l.ID)
				if e != nil {
					return nil, e
				}
				_, e = writeEvent(ctx, tx, l, id, 0, line.Facts, []string{obs})
				if e != nil {
					return nil, e
				}
				_, e = tx.Exec(ctx, `INSERT INTO qb.evidence_links(ledger_id,source_event_id,event_id,observation_id) VALUES($1,$2,$3,$4)`, l.ID, source, id, obs)
				if e != nil {
					return nil, e
				}
				imported++
			} else {
				return nil, e
			}
			_, e = tx.Exec(ctx, `INSERT INTO qb.statement_lines(id,ledger_id,import_id,source_record_id,event_id,raw_data) VALUES($1,$2,$3,$4,$5,$6)`, domain.ID(), l.ID, importID, line.Record, id, raw)
			if e != nil {
				return nil, e
			}
		}
		return map[string]any{"import_id": importID, "imported": imported, "rows": len(lines)}, nil
	})
}
