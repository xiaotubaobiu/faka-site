package store

import "testing"

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
