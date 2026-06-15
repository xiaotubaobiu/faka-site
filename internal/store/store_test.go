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
