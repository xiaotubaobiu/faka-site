package store

import (
	"context"
	"testing"
)

func TestOpenAndMigrate(t *testing.T) {
	s, err := OpenInMemory()
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()
	if err := s.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	var n int
	for _, tbl := range []string{"users", "orders", "order_codes", "balance_ledger", "config"} {
		if err := s.db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?", tbl).Scan(&n); err != nil {
			t.Fatalf("query %s: %v", tbl, err)
		}
		if n != 1 {
			t.Fatalf("table %s missing", tbl)
		}
	}
}

func TestUsersCRUD(t *testing.T) {
	s, _ := OpenInMemory()
	defer s.Close()
	s.Migrate()

	id, err := s.CreateUser("a@b.com", "hash", "user")
	if err != nil {
		t.Fatal(err)
	}
	u, err := s.UserByEmail("a@b.com")
	if err != nil || u.ID != id || u.Balance != 0 || u.Status != 1 {
		t.Fatalf("unexpected user: %+v %v", u, err)
	}
	if _, err := s.UserByEmail("missing@b.com"); err != ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if err := s.SetUserStatus(id, 0); err != nil {
		t.Fatal(err)
	}
	u, _ = s.UserByID(id)
	if u.Status != 0 {
		t.Fatal("status not updated")
	}
}

func TestHoldInsufficientAndRefund(t *testing.T) {
	ctx := context.Background()
	s, _ := OpenInMemory()
	defer s.Close()
	s.Migrate()
	uid, _ := s.CreateUser("u@x.com", "h", "user")
	_ = s.AddBalance(ctx, uid, 1, 1_000_000)

	if _, err := s.HoldForOrder(ctx, uid, 1, 2_000_000, 2_000_000); err != ErrInsufficient {
		t.Fatalf("want ErrInsufficient, got %v", err)
	}

	oid, err := s.HoldForOrder(ctx, uid, 1, 500_000, 500_000)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := s.UserByID(uid)
	if u.Balance != 500_000 {
		t.Fatalf("balance after hold = %d, want 500000", u.Balance)
	}

	if err := s.Refund(ctx, uid, oid, 200_000); err != nil {
		t.Fatal(err)
	}
	u, _ = s.UserByID(uid)
	if u.Balance != 700_000 {
		t.Fatalf("balance after refund = %d, want 700000", u.Balance)
	}

	var n int
	s.db.QueryRow("SELECT count(*) FROM balance_ledger WHERE user_id=?", uid).Scan(&n)
	if n != 3 {
		t.Fatalf("ledger rows = %d, want 3", n)
	}
}

func TestSettleOrderCompleted(t *testing.T) {
	ctx := context.Background()
	s, _ := OpenInMemory()
	defer s.Close()
	s.Migrate()
	uid, _ := s.CreateUser("u2@x.com", "h", "user")
	s.AddBalance(ctx, uid, 1, 1_000_000)
	oid, _ := s.HoldForOrder(ctx, uid, 2, 100_000, 200_000)

	codes := []string{"CODE111", "CODE222"}
	o := Order{ID: oid, UserID: uid, QuotaPerCode: 100_000, Status: "completed", SucceededCount: 2}
	if err := s.SettleOrder(ctx, o, codes); err != nil {
		t.Fatal(err)
	}
	got, _ := s.OrderCodes(ctx, oid)
	if len(got) != 2 {
		t.Fatalf("want 2 codes, got %d", len(got))
	}
	ord, _ := s.Order(ctx, oid)
	if ord.Status != "completed" || ord.SucceededCount != 2 {
		t.Fatalf("order not settled: %+v", ord)
	}
	u, _ := s.UserByID(uid)
	if u.Balance != 800_000 {
		t.Fatalf("balance=%d want 800000", u.Balance)
	}
}

func TestConfigKV(t *testing.T) {
	ctx := context.Background()
	s, _ := OpenInMemory()
	defer s.Close()
	s.Migrate()
	if _, err := s.GetConfig(ctx, "x"); err != ErrConfigMissing {
		t.Fatalf("want ErrConfigMissing, got %v", err)
	}
	s.SetConfig(ctx, "x", "1")
	s.SetConfig(ctx, "x", "2")
	v, _ := s.GetConfig(ctx, "x")
	if v != "2" {
		t.Fatalf("want 2, got %s", v)
	}
}
