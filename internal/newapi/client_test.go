package newapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGenerateCodesBatching(t *testing.T) {
	var calls []int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" || r.Header.Get("New-Api-User") == "" {
			t.Fatal("missing auth headers")
		}
		var req redemptionReq
		json.NewDecoder(r.Body).Decode(&req)
		calls = append(calls, req.Count)
		codes := make([]string, req.Count)
		for i := range codes {
			codes[i] = "C"
		}
		json.NewEncoder(w).Encode(apiResp{Success: true, Data: codes})
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, AccessToken: "tok", AdminUserID: "1"})
	got, err := c.GenerateCodes(context.Background(), "fk-1", 500_000, 350)
	if err != nil {
		t.Fatalf("want nil err, got %v", err)
	}
	if len(got) != 350 {
		t.Fatalf("want 350 codes, got %d", len(got))
	}
	if len(calls) != 4 || calls[0] != 100 || calls[3] != 50 {
		t.Fatalf("batch split = %v, want [100 100 100 50]", calls)
	}
}

func TestGenerateCodesPartialFailure(t *testing.T) {
	call := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call++
		if call == 1 {
			codes := make([]string, 100)
			json.NewEncoder(w).Encode(apiResp{Success: true, Data: codes})
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, AccessToken: "tok", AdminUserID: "1"})
	got, err := c.GenerateCodes(context.Background(), "fk-1", 500_000, 150)
	if err == nil {
		t.Fatal("want error on partial failure")
	}
	if len(got) != 100 {
		t.Fatalf("want 100 partial codes, got %d", len(got))
	}
}

func TestGenerateCodesCompliance(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"success":false,"message":"payment compliance required"}`))
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL})
	_, err := c.GenerateCodes(context.Background(), "fk-1", 1, 1)
	if err != ErrCompliance {
		t.Fatalf("want ErrCompliance, got %v", err)
	}
}
