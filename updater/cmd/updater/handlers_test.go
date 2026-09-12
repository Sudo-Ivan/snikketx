package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleServices(t *testing.T) {
	s := &server{token: "tok", tokenBytes: []byte("tok")}

	rec := httptest.NewRecorder()
	s.auth(s.handleServices)(rec, httptest.NewRequest(http.MethodGet, "/services", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauth code=%d", rec.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/services", nil)
	req.Header.Set("Authorization", "Bearer tok")
	rec = httptest.NewRecorder()
	s.auth(s.handleServices)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d", rec.Code)
	}
	var got []logService
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != len(allowedLogServices) {
		t.Fatalf("len=%d want %d", len(got), len(allowedLogServices))
	}
	if got[0].ID != "snikket_server" || got[0].Label == "" {
		t.Fatalf("first=%+v", got[0])
	}
}
