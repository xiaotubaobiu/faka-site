package store

import (
	"context"
	"testing"
)

func TestRechargeOrder_CreateAndSettle(t *testing.T) {
	s, _ := OpenInMemory()
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	uid, _ := s.CreateUser("u@x.com", "h", "user")

	o, err := s.CreateRechargeOrder(ctx, uid, "alipay", "RC1", 1000, 500000)
	if err != nil {
		t.Fatal(err)
	}
	if o.ID == 0 || o.Status != "pending" || o.Quota != 500000 {
		t.Fatalf("unexpected order: %+v", o)
	}

	got, err := s.RechargeOrderByTradeNo(ctx, "RC1")
	if err != nil || got.OutTradeNo != "RC1" {
		t.Fatalf("lookup failed: %v %+v", err, got)
	}

	// first settle -> credits once
	credited, err := s.SettleRecharge(ctx, "RC1", "ALI123")
	if err != nil || !credited {
		t.Fatalf("first settle should credit: %v %v", credited, err)
	}
	u, _ := s.UserByID(uid)
	if u.Balance != 500000 {
		t.Fatalf("balance after settle = %d, want 500000", u.Balance)
	}

	// second settle (e.g. duplicate notify) -> NO double credit
	credited2, err := s.SettleRecharge(ctx, "RC1", "ALI123")
	if err != nil || credited2 {
		t.Fatalf("second settle must NOT credit again: %v %v", credited2, err)
	}
	u2, _ := s.UserByID(uid)
	if u2.Balance != 500000 {
		t.Fatalf("balance changed on duplicate: %d", u2.Balance)
	}
}

func TestRechargeOrdersByUser(t *testing.T) {
	s, _ := OpenInMemory()
	s.Migrate()
	ctx := context.Background()
	uid, _ := s.CreateUser("u@x.com", "h", "user")
	s.CreateRechargeOrder(ctx, uid, "alipay", "A1", 1000, 500000)
	s.CreateRechargeOrder(ctx, uid, "wxpay", "W1", 500, 250000)
	list, _ := s.RechargeOrdersByUser(ctx, uid)
	if len(list) != 2 {
		t.Fatalf("want 2 orders, got %d", len(list))
	}
}
