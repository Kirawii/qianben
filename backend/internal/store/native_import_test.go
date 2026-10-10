package store

import (
	"context"
	"encoding/json"
	"github.com/Kirawii/qianben/backend/internal/domain"
	"os"
	"strings"
	"testing"
	"time"
)

func TestNativeImportMappingAndPending(t *testing.T) {
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("real PostgreSQL required")
	}
	ctx := context.Background()
	admin, e := Open(ctx, os.Getenv("TEST_ADMIN_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	defer admin.Pool.Close()
	db, e := Open(ctx, os.Getenv("TEST_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Pool.Close()
	actor := domain.ID()
	if _, e = admin.Pool.Exec(ctx, `INSERT INTO qb.users(id,name) VALUES($1,'native CSV fixture')`, actor); e != nil {
		t.Fatal(e)
	}
	cut := time.Now().UTC().Truncate(time.Second).Add(-48 * time.Hour)
	l, e := db.CreateLedger(ctx, actor, CreateLedgerRequest{domain.ID(), "支付账单测试", cut})
	if e != nil {
		t.Fatal(e)
	}
	defer func() {
		if e := db.DeleteLedger(ctx, actor, l.ID, l.Name); e != nil {
			t.Error(e)
		}
	}()
	create := func(name string) domain.Account {
		raw, e := db.CreateAccount(ctx, actor, l.ID, AccountRequest{CommandID: domain.ID(), Name: name, Type: "ASSET", Cash: true})
		if e != nil {
			t.Fatal(e)
		}
		var a domain.Account
		json.Unmarshal(raw, &a)
		if _, e = db.SetOpening(ctx, actor, l.ID, OpeningRequest{CommandID: domain.ID(), AccountID: a.ID, Amount: 10000, AsOf: cut, Meaning: "BALANCE"}); e != nil {
			t.Fatal(e)
		}
		return a
	}
	one, two := create("零钱"), create("银行卡")
	at := cut.Add(time.Hour)
	sourceTime := at.In(time.FixedZone("CST", 8*3600)).Format("2006-01-02 15:04:05")
	content := "交易时间,交易类型,交易对方,商品,收/支,金额(元),支付方式,当前状态,交易单号,商户单号,备注\n" + sourceTime + ",商户消费,食堂,午餐,支出,10.00,零钱,支付成功,n1,m1,/\n" + sourceTime + ",商户消费,食堂,晚餐,支出,20.00,银行卡(1234),支付成功,n2,m2,/\n" + sourceTime + ",商户消费,食堂,待付,支出,30.00,零钱,处理中,n3,m3,/\n"
	r := NativeCSVImportRequest{CommandID: domain.ID(), AccountID: one.ID, Format: "WECHAT_PERSONAL_V1", Content: content, ConfirmedCNY: true, Mappings: map[string]string{"零钱": one.ID, "银行卡(1234)": two.ID}}
	if _, e = db.ImportNativeCSV(ctx, actor, l.ID, r); e != nil {
		t.Fatal(e)
	}
	report, e := db.Reports(ctx, actor, l.ID, cut, at.Add(time.Hour))
	if e != nil || report.CashChange != -3000 || report.Consumption != 0 || report.UnresolvedBalance != 3000 {
		t.Fatal(report, e)
	}
	accounts, e := db.Accounts(ctx, actor, l.ID)
	if e != nil {
		t.Fatal(e)
	}
	for _, a := range accounts {
		if a.ID == one.ID && a.Balance != 9000 || a.ID == two.ID && a.Balance != 8000 {
			t.Fatal("mapping ignored", a)
		}
	}
	events, e := db.Events(ctx, actor, l.ID)
	if e != nil || len(events) != 3 {
		t.Fatal(events, e)
	}
	for _, ev := range events {
		if ev.Facts.Amount == 3000 && ev.Posted {
			t.Fatal("pending status posted", ev)
		}
	}
	if _, e = db.ImportNativeCSV(ctx, actor, l.ID, r); e != nil {
		t.Fatal("receipt replay", e)
	}
	r.CommandID = domain.ID()
	if _, e = db.ImportNativeCSV(ctx, actor, l.ID, r); e != nil {
		t.Fatal("file dedup", e)
	}
	r.CommandID = domain.ID()
	r.Mappings["银行卡(1234)"] = one.ID
	if _, e = db.ImportNativeCSV(ctx, actor, l.ID, r); e == nil {
		t.Fatal("changed mapping silently reapplied")
	}
	report, e = db.Reports(ctx, actor, l.ID, cut, at.Add(time.Hour))
	if e != nil || report.CashChange != -3000 {
		t.Fatal("conflict was not atomic", report, e)
	}
	if _, e = db.ImportNativeCSV(ctx, domain.ID(), l.ID, r); e == nil {
		t.Fatal("cross-user import")
	}
	r.CommandID = domain.ID()
	r.Mappings["银行卡(1234)"] = domain.ID()
	if _, e = db.ImportNativeCSV(ctx, actor, l.ID, r); e == nil {
		t.Fatal("foreign account accepted")
	}
	r.CommandID = domain.ID()
	r.ConfirmedCNY = false
	r.Mappings["银行卡(1234)"] = two.ID
	r.Content = strings.NewReplacer("n1,", "u1,", "n2,", "u2,", "n3,", "u3,").Replace(content)
	if _, e = db.ImportNativeCSV(ctx, actor, l.ID, r); e != nil {
		t.Fatal(e)
	}
	report, e = db.Reports(ctx, actor, l.ID, cut, at.Add(time.Hour))
	if e != nil || report.CashChange != -3000 {
		t.Fatal("unconfirmed currency posted", report, e)
	}
}
