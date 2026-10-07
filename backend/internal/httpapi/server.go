package httpapi

import (
	"encoding/json"
	"errors"
	"github.com/Kirawii/qianben/backend/internal/domain"
	"github.com/Kirawii/qianben/backend/internal/store"
	"io"
	"net/http"
	"strings"
	"time"
)

type Server struct{ DB *store.DB }

func (s Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200); w.Write([]byte(`{"status":"ok"}`)) })
	mux.HandleFunc("/v1/", s.serve)
	return mux
}
func decode(w http.ResponseWriter, r *http.Request, out any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(out); e != nil {
		return domain.Invalid("请求 JSON 无效")
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return domain.Invalid("请求只允许一个 JSON 对象")
	}
	return nil
}
func respond(w http.ResponseWriter, v any, e error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if e != nil {
		var f *domain.Fault
		if errors.As(e, &f) {
			w.WriteHeader(f.Status)
			json.NewEncoder(w).Encode(map[string]string{"code": f.Code, "message": f.Message})
		} else {
			w.WriteHeader(500)
			json.NewEncoder(w).Encode(map[string]string{"code": "internal_error", "message": "操作未完成，请重试"})
		}
		return
	}
	json.NewEncoder(w).Encode(v)
}
func (s Server) serve(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
		respond(w, nil, domain.Unauthorized())
		return
	}
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	actor, e := s.DB.Actor(r.Context(), token)
	if e != nil {
		respond(w, nil, e)
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 2 || parts[1] != "ledgers" {
		respond(w, nil, domain.Missing("接口不存在"))
		return
	}
	if len(parts) == 2 {
		switch r.Method {
		case "GET":
			v, e := s.DB.Ledgers(r.Context(), actor)
			respond(w, v, e)
		case "POST":
			var req store.CreateLedgerRequest
			if e = decode(w, r, &req); e != nil {
				respond(w, nil, e)
				return
			}
			v, e := s.DB.CreateLedger(r.Context(), actor, req)
			respond(w, v, e)
		default:
			w.WriteHeader(405)
		}
		return
	}
	if len(parts) != 4 || !domain.IsUUID(parts[2]) {
		respond(w, nil, domain.Missing("接口不存在"))
		return
	}
	ledger := parts[2]
	switch parts[3] {
	case "balance-checks":
		switch r.Method {
		case "GET":
			v, e := s.DB.BalanceChecks(r.Context(), actor, ledger)
			respond(w, v, e)
		case "POST":
			var req store.CheckRequest
			if e = decode(w, r, &req); e != nil {
				respond(w, nil, e)
				return
			}
			v, e := s.DB.CheckBalance(r.Context(), actor, ledger, req)
			respond(w, v, e)
		default:
			w.WriteHeader(405)
		}
	case "rules":
		if r.Method != "GET" {
			w.WriteHeader(405)
			return
		}
		v, e := s.DB.Rules(r.Context(), actor, ledger)
		respond(w, v, e)
	case "revoke-rule":
		if r.Method != "POST" {
			w.WriteHeader(405)
			return
		}
		var req store.RevokeRuleRequest
		if e = decode(w, r, &req); e != nil {
			respond(w, nil, e)
			return
		}
		v, e := s.DB.RevokeRule(r.Context(), actor, ledger, req)
		respond(w, v, e)
	case "relations":
		if r.Method != "GET" {
			w.WriteHeader(405)
			return
		}
		v, e := s.DB.Relations(r.Context(), actor, ledger, r.URL.Query().Get("event_id"))
		respond(w, v, e)
	case "unlink-transfer":
		if r.Method != "POST" {
			w.WriteHeader(405)
			return
		}
		var req store.UnlinkRequest
		if e = decode(w, r, &req); e != nil {
			respond(w, nil, e)
			return
		}
		v, e := s.DB.UnlinkTransfer(r.Context(), actor, ledger, req)
		respond(w, v, e)
	case "evidence":
		if r.Method != "GET" {
			w.WriteHeader(405)
			return
		}
		v, e := s.DB.Evidence(r.Context(), actor, ledger, r.URL.Query().Get("event_id"))
		respond(w, v, e)
	case "assets":
		if r.Method != "GET" {
			w.WriteHeader(405)
			return
		}
		v, e := s.DB.Assets(r.Context(), actor, ledger)
		respond(w, v, e)
	case "csv":
		if r.Method != "POST" {
			w.WriteHeader(405)
			return
		}
		var req store.CSVRequest
		if e = decode(w, r, &req); e != nil {
			respond(w, nil, e)
			return
		}
		v, e := s.DB.ImportCSV(r.Context(), actor, ledger, req)
		respond(w, v, e)
	case "merge":
		if r.Method != "POST" {
			w.WriteHeader(405)
			return
		}
		var req store.MergeRequest
		if e = decode(w, r, &req); e != nil {
			respond(w, nil, e)
			return
		}
		v, e := s.DB.Merge(r.Context(), actor, ledger, req)
		respond(w, v, e)
	case "split":
		if r.Method != "POST" {
			w.WriteHeader(405)
			return
		}
		var req store.SplitRequest
		if e = decode(w, r, &req); e != nil {
			respond(w, nil, e)
			return
		}
		v, e := s.DB.Split(r.Context(), actor, ledger, req)
		respond(w, v, e)
	case "transfer":
		if r.Method != "POST" {
			w.WriteHeader(405)
			return
		}
		var req store.TransferRequest
		if e = decode(w, r, &req); e != nil {
			respond(w, nil, e)
			return
		}
		v, e := s.DB.LinkTransfer(r.Context(), actor, ledger, req)
		respond(w, v, e)
	case "delete":
		if r.Method != "POST" {
			w.WriteHeader(405)
			return
		}
		var req struct {
			Confirm string `json:"confirm_name"`
		}
		if e = decode(w, r, &req); e != nil {
			respond(w, nil, e)
			return
		}
		e = s.DB.DeleteLedger(r.Context(), actor, ledger, req.Confirm)
		respond(w, map[string]bool{"deleted": e == nil}, e)
	case "observations":
		if r.Method != "POST" {
			w.WriteHeader(405)
			return
		}
		var req struct {
			Items []domain.Delivery `json:"items"`
		}
		if e = decode(w, r, &req); e != nil {
			respond(w, nil, e)
			return
		}
		v, e := s.DB.Ingest(r.Context(), actor, ledger, req.Items)
		respond(w, v, e)
	case "accounts":
		switch r.Method {
		case "GET":
			v, e := s.DB.Accounts(r.Context(), actor, ledger)
			respond(w, v, e)
		case "POST":
			var req store.AccountRequest
			if e = decode(w, r, &req); e != nil {
				respond(w, nil, e)
				return
			}
			v, e := s.DB.CreateAccount(r.Context(), actor, ledger, req)
			respond(w, v, e)
		default:
			w.WriteHeader(405)
		}
	case "opening":
		if r.Method != "POST" {
			w.WriteHeader(405)
			return
		}
		var req store.OpeningRequest
		if e = decode(w, r, &req); e != nil {
			respond(w, nil, e)
			return
		}
		v, e := s.DB.SetOpening(r.Context(), actor, ledger, req)
		respond(w, v, e)
	case "events":
		switch r.Method {
		case "GET":
			v, e := s.DB.EventsPage(r.Context(), actor, ledger, r.URL.Query().Get("before"))
			respond(w, v, e)
		case "POST":
			var req store.EventRequest
			if e = decode(w, r, &req); e != nil {
				respond(w, nil, e)
				return
			}
			v, e := s.DB.SaveEvent(r.Context(), actor, ledger, req)
			respond(w, v, e)
		default:
			w.WriteHeader(405)
		}
	case "reports":
		if r.Method != "GET" {
			w.WriteHeader(405)
			return
		}
		from, err := time.Parse(time.RFC3339, r.URL.Query().Get("from"))
		to, err2 := time.Parse(time.RFC3339, r.URL.Query().Get("to"))
		if err != nil || err2 != nil {
			respond(w, nil, domain.Invalid("from/to 须为 RFC3339 时间"))
			return
		}
		v, e := s.DB.Reports(r.Context(), actor, ledger, from, to)
		respond(w, v, e)
	default:
		respond(w, nil, domain.Missing("接口不存在"))
	}
}
