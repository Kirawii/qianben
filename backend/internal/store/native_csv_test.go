package store

import "testing"

func TestNativeCSVHeaderContract(t *testing.T) {
	content := "微信支付账单明细\n交易时间,交易类型,交易对方,商品,收/支,金额(元),支付方式,当前状态,交易单号,商户单号,备注\n2026-09-30 12:00:00,商户消费,食堂,午餐,支出,￥12.34,零钱,支付成功,txn1,m1,/\n2026-09-30 13:00:00,转账,自己,/,不计收支,5.00,银行卡(1234),处理中,txn2,m2,/\n"
	r := NativeCSVRequest{Format: "WECHAT_PERSONAL_V1", Content: content}
	p, e := parseNativeCSV(r)
	if e != nil || len(p.Rows) != 2 || p.Rows[0].Amount != 1234 || p.Rows[1].Direction != "UNKNOWN" || p.Rows[1].Status != "处理中" || len(p.Methods) != 2 {
		t.Fatal(p, e)
	}
	_, offset := p.Rows[0].At.Zone()
	if offset != 8*3600 {
		t.Fatal("missing source timezone")
	}
	r.Content = content + "2026-09-30 12:00:00,商户消费,食堂,午餐,支出,12.34,零钱,支付成功,txn1,m1,/\n"
	if _, e = parseNativeCSV(r); e == nil {
		t.Fatal("duplicate transaction ID")
	}
	r.Content = "交易时间,交易分类,交易对方,对方账号,商品说明,收/支,金额,收/付款方式,交易状态,交易订单号,商家订单号,备注\n2026-09-30 12:00:00,餐饮,食堂,masked,午餐,支出,12.34,余额,交易成功,ali1,m1,/\n"
	r.Format = "ALIPAY_PERSONAL_V1"
	p, e = parseNativeCSV(r)
	if e != nil || p.Rows[0].Record != "ali1" || p.Rows[0].Method != "余额" {
		t.Fatal(p, e)
	}
	r.Content = string([]byte{0xff, 0xfe})
	if _, e = parseNativeCSV(r); e == nil {
		t.Fatal("invalid UTF8")
	}
}
