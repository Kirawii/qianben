package store

import (
	"github.com/Kirawii/qianben/backend/internal/domain"
	"testing"
)

func TestCSVContract(t *testing.T) {
	r := CSVRequest{AccountID: domain.ID(), Content: "record_id,occurred_at,amount_minor,kind,merchant,category\na,2026-10-02T12:00:00+08:00,1234,EXPENSE,食堂,餐饮\n"}
	lines, e := parseCSV(r)
	if e != nil || len(lines) != 1 || lines[0].Facts.Amount != 1234 {
		t.Fatal(lines, e)
	}
	r.Content += "a,2026-10-02T12:00:00+08:00,1234,EXPENSE,食堂,餐饮\n"
	if _, e = parseCSV(r); e == nil {
		t.Fatal("duplicate record id accepted")
	}
}
