package store

import (
	"context"
	"encoding/csv"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Kirawii/qianben/backend/internal/domain"
	"github.com/jackc/pgx/v5"
)

type NativeCSVRequest struct {
	Format  string `json:"format"`
	Content string `json:"content"`
}
type NativeCSVRow struct {
	Record    string        `json:"record_id"`
	At        time.Time     `json:"source_time"`
	Amount    domain.Amount `json:"amount_minor"`
	Merchant  string        `json:"merchant"`
	Method    string        `json:"payment_method"`
	Direction string        `json:"direction"`
	Type      string        `json:"source_type"`
	Status    string        `json:"source_status"`
}
type NativeCSVPreview struct {
	Format  string         `json:"format"`
	Version string         `json:"parser_version"`
	Rows    []NativeCSVRow `json:"rows"`
	Methods []string       `json:"payment_methods"`
}

// Recognition is an explicit header contract, not a claim of provider authenticity.
func parseNativeCSV(r NativeCSVRequest) (NativeCSVPreview, error) {
	p := NativeCSVPreview{Format: r.Format, Version: "native-preview-v1", Rows: []NativeCSVRow{}, Methods: []string{}}
	if len(r.Content) > 800000 || !utf8.ValidString(r.Content) {
		return p, domain.Invalid("账单须为有效 UTF-8 且不超过 800 KB")
	}
	var fields []string
	switch r.Format {
	case "WECHAT_PERSONAL_V1":
		fields = []string{"交易时间", "交易类型", "交易对方", "商品", "收/支", "金额(元)", "支付方式", "当前状态", "交易单号", "商户单号", "备注"}
	case "ALIPAY_PERSONAL_V1":
		fields = []string{"交易时间", "交易分类", "交易对方", "对方账号", "商品说明", "收/支", "金额", "收/付款方式", "交易状态", "交易订单号", "商家订单号", "备注"}
	default:
		return p, domain.Invalid("未知个人账单格式")
	}
	reader := csv.NewReader(strings.NewReader(strings.TrimPrefix(r.Content, "\ufeff")))
	reader.FieldsPerRecord = -1
	clean := func(s string) string { return strings.TrimSpace(strings.TrimSuffix(s, "\t")) }
	var indices map[string]int
	for n := 0; n < 60; n++ {
		header, e := reader.Read()
		if e != nil {
			return p, domain.Invalid("没有匹配的个人账单表头")
		}
		if len(header) != len(fields) {
			continue
		}
		candidate := map[string]int{}
		for i, v := range header {
			candidate[clean(v)] = i
		}
		valid := len(candidate) == len(fields)
		for _, f := range fields {
			if _, ok := candidate[f]; !ok {
				valid = false
			}
		}
		if valid {
			indices = candidate
			break
		}
	}
	if indices == nil {
		return p, domain.Invalid("没有匹配的个人账单表头")
	}
	seen := map[string]bool{}
	methods := map[string]bool{}
	for {
		row, e := reader.Read()
		if e == io.EOF {
			break
		}
		if e != nil {
			return p, domain.Invalid("账单 CSV 行格式无效")
		}
		if len(row) != len(fields) {
			return p, domain.Invalid("账单数据行字段数不匹配；请移除表尾统计后重试")
		}
		if len(p.Rows) >= 2000 {
			return p, domain.Invalid("账单超过 2000 行")
		}
		get := func(f string) string { return clean(row[indices[f]]) }
		recordField, methodField, typeField, statusField, amountField := "交易单号", "支付方式", "交易类型", "当前状态", "金额(元)"
		if r.Format == "ALIPAY_PERSONAL_V1" {
			recordField, methodField, typeField, statusField, amountField = "交易订单号", "收/付款方式", "交易分类", "交易状态", "金额"
		}
		id := get(recordField)
		if !textLimit(id, 128) || seen[id] {
			return p, domain.Invalid("交易号缺失、重复或过长")
		}
		seen[id] = true
		at, e := time.ParseInLocation("2006-01-02 15:04:05", get("交易时间"), time.FixedZone("Asia/Shanghai", 8*3600))
		if e != nil {
			return p, domain.Invalid("账单交易时间无效")
		}
		decimal := strings.TrimPrefix(strings.TrimPrefix(get(amountField), "¥"), "￥")
		parts := strings.Split(decimal, ".")
		if len(parts) > 2 || len(parts[0]) == 0 || len(parts[0]) > 13 {
			return p, domain.Invalid("账单金额无效")
		}
		minor := parts[0] + "00"
		if len(parts) == 2 {
			if len(parts[1]) != 2 {
				return p, domain.Invalid("账单金额须精确到两位小数")
			}
			minor = parts[0] + parts[1]
		}
		for _, c := range minor {
			if c < '0' || c > '9' {
				return p, domain.Invalid("账单金额无效")
			}
		}
		n, e := strconv.ParseInt(minor, 10, 64)
		if e != nil || n <= 0 || n > 9000000000000000 {
			return p, domain.Invalid("账单金额须为有效正数")
		}
		direction := "UNKNOWN"
		switch get("收/支") {
		case "支出":
			direction = "OUT"
		case "收入":
			direction = "IN"
		}
		merchant, method := get("交易对方"), get(methodField)
		if len(merchant) > 200 || len(method) > 200 {
			return p, domain.Invalid("账单对方或支付方式过长")
		}
		if !methods[method] {
			methods[method] = true
			p.Methods = append(p.Methods, method)
		}
		p.Rows = append(p.Rows, NativeCSVRow{id, at, domain.Amount(n), merchant, method, direction, get(typeField), get(statusField)})
	}
	if len(p.Rows) == 0 {
		return p, domain.Invalid("账单没有交易行")
	}
	return p, nil
}

func (d *DB) PreviewNativeCSV(ctx context.Context, actor, ledger string, r NativeCSVRequest) (NativeCSVPreview, error) {
	tx, e := d.Pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if e != nil {
		return NativeCSVPreview{}, e
	}
	defer tx.Rollback(ctx)
	if _, e = loadLedger(ctx, tx, actor, ledger, ""); e != nil {
		return NativeCSVPreview{}, e
	}
	return parseNativeCSV(r)
}
