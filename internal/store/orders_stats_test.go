package store

import (
	"context"
	"testing"
)

func TestOrderStats_CountRecentSum(t *testing.T) {
	s, _ := OpenInMemory()
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	mk := func(total int64, ts int64) {
		_, err := s.db.Exec(`INSERT INTO orders(user_id,code_count,quota_per_code,total_cost,status,created_at,updated_at)
			VALUES(1,1,500000,?,'completed',?,?)`, total, ts, ts)
		if err != nil {
			t.Fatal(err)
		}
	}
	mk(500000, 1_700_000_000)
	mk(1000000, 1_700_000_010)
	mk(500000, 1_600_000_000)

	count, err := s.CountOrdersByUser(ctx, 1)
	if err != nil || count != 3 {
		t.Fatalf("count=%d err=%v want 3", count, err)
	}
	sum, err := s.SumUsedByUser(ctx, 1, 1_650_000_000)
	if err != nil || sum != 1500000 {
		t.Fatalf("sum=%d err=%v want 1500000", sum, err)
	}
	recent, err := s.RecentOrdersByUser(ctx, 1, 2)
	if err != nil || len(recent) != 2 || recent[0].TotalCost != 1000000 {
		t.Fatalf("recent=%v err=%v want 2 rows, first total=1000000", recent, err)
	}
}
