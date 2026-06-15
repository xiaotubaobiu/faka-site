package service

import (
	"context"
	"errors"
	"testing"

	"faka-site/internal/newapi"
	"faka-site/internal/store"
)

type fakeIssuer struct {
	codes []string
	err   error
}

func (f fakeIssuer) GenerateCodes(ctx context.Context, name string, quota int64, count int) ([]string, error) {
	return f.codes, f.err
}

func setup(t *testing.T) (*store.Store, int64) {
	t.Helper()
	s, _ := store.OpenInMemory()
	s.Migrate()
	uid, _ := s.CreateUser("u@x.com", "h", "user")
	s.AddBalance(context.Background(), uid, 1, 10_000_000)
	return s, uid
}

func TestBuyCompleted(t *testing.T) {
	s, uid := setup(t)
	defer s.Close()
	bs := &BuyService{Store: s, Issuer: fakeIssuer{codes: []string{"A", "B", "C"}}}
	r, err := bs.Buy(context.Background(), uid, 3, 1_000_000)
	if err != nil || r.Status != "completed" || len(r.Codes) != 3 {
		t.Fatalf("unexpected: %+v %v", r, err)
	}
	u, _ := s.UserByID(uid)
	if u.Balance != 7_000_000 {
		t.Fatalf("balance=%d want 7000000", u.Balance)
	}
}

func TestBuyPartial(t *testing.T) {
	s, uid := setup(t)
	defer s.Close()
	bs := &BuyService{Store: s, Issuer: fakeIssuer{codes: []string{"A", "B"}, err: newapi.ErrUpstream}}
	r, err := bs.Buy(context.Background(), uid, 3, 1_000_000)
	if err == nil {
		t.Fatal("want err on partial")
	}
	if r.Status != "partial" || r.Failed != 1 || r.Refunded != 1_000_000 {
		t.Fatalf("unexpected partial: %+v", r)
	}
	u, _ := s.UserByID(uid)
	if u.Balance != 8_000_000 {
		t.Fatalf("balance=%d want 8000000", u.Balance)
	}
}

func TestBuyFailed(t *testing.T) {
	s, uid := setup(t)
	defer s.Close()
	bs := &BuyService{Store: s, Issuer: fakeIssuer{err: errors.New("boom")}}
	r, _ := bs.Buy(context.Background(), uid, 2, 1_000_000)
	if r.Status != "failed" || r.Refunded != 2_000_000 {
		t.Fatalf("unexpected failed: %+v", r)
	}
	u, _ := s.UserByID(uid)
	if u.Balance != 10_000_000 {
		t.Fatalf("balance=%d want 10000000 (full refund)", u.Balance)
	}
}
