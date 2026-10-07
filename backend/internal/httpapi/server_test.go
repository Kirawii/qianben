package httpapi

import (
	"bytes"
	"encoding/json"
	"github.com/Kirawii/qianben/backend/internal/domain"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthAndAuthorizationBoundary(t *testing.T) {
	h := Server{}.Handler()
	r := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	r = httptest.NewRequest("GET", "/v1/ledgers", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("missing Bearer header bypassed auth")
	}
}
func TestStrictCommandJSON(t *testing.T) {
	for _, body := range []string{`{"amount_minor":100}`, `{"amount_minor":"100","unexpected":true}`, `{"amount_minor":"100"} {}`} {
		r := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
		w := httptest.NewRecorder()
		var req struct {
			Amount domain.Amount `json:"amount_minor"`
		}
		if e := decode(w, r, &req); e == nil {
			t.Fatal("invalid command accepted", body)
		}
	}
	var a domain.Amount
	if e := json.Unmarshal([]byte(`"9007199254740993"`), &a); e != nil || a != 9007199254740993 {
		t.Fatal("wire precision", a, e)
	}
}
