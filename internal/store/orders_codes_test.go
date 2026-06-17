package store

import (
	"context"
	"testing"
)

func TestCodesForOrders_Batch(t *testing.T) {
	s, _ := OpenInMemory()
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	_, _ = s.db.Exec(`INSERT INTO order_codes(order_id,user_id,code,quota,created_at) VALUES(1,1,'AAA',500000,0)`)
	_, _ = s.db.Exec(`INSERT INTO order_codes(order_id,user_id,code,quota,created_at) VALUES(1,1,'BBB',500000,0)`)
	_, _ = s.db.Exec(`INSERT INTO order_codes(order_id,user_id,code,quota,created_at) VALUES(2,1,'CCC',500000,0)`)

	m, err := s.CodesForOrders(ctx, []int64{1, 2, 99})
	if err != nil {
		t.Fatal(err)
	}
	if len(m[1]) != 2 || m[1][0] != "AAA" || m[1][1] != "BBB" {
		t.Fatalf("order 1 codes = %v", m[1])
	}
	if len(m[2]) != 1 || m[2][0] != "CCC" {
		t.Fatalf("order 2 codes = %v", m[2])
	}
	if _, ok := m[99]; ok {
		t.Fatal("order 99 should be absent")
	}
	empty, err := s.CodesForOrders(ctx, nil)
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty case: %v %v", empty, err)
	}
}

func TestOrdersByUserFiltered_Q(t *testing.T) {
	s, _ := OpenInMemory()
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	mk := func(total int64, ts int64, st string) {
		_, _ = s.db.Exec(`INSERT INTO orders(user_id,code_count,quota_per_code,total_cost,status,created_at,updated_at) VALUES(1,1,500000,?,?,?,?)`, total, st, ts, ts)
	}
	mk(500000, 1, "completed")
	mk(1000000, 2, "partial")

	all, _ := s.OrdersByUserFiltered(ctx, 1, "")
	if len(all) != 2 {
		t.Fatalf("empty q -> want 2, got %d", len(all))
	}
	byStatus, _ := s.OrdersByUserFiltered(ctx, 1, "partial")
	if len(byStatus) != 1 || byStatus[0].Status != "partial" {
		t.Fatalf("q=partial -> %v", byStatus)
	}
	byID, _ := s.OrdersByUserFiltered(ctx, 1, "2")
	if len(byID) != 1 {
		t.Fatalf("q=2 -> %d", len(byID))
	}
}
