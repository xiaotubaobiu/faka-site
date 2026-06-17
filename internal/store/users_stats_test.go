package store

import "testing"

func TestUserStats_CountAndTotalBalance(t *testing.T) {
	s, _ := OpenInMemory()
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	_, _ = s.CreateUser("a@x.com", "h1", "user")
	_, _ = s.CreateUser("b@x.com", "h2", "user")
	_, _ = s.db.Exec(`UPDATE users SET balance=balance+? WHERE email=?`, 500000, "a@x.com")
	_, _ = s.db.Exec(`UPDATE users SET balance=balance+? WHERE email=?`, 500000, "b@x.com")

	count, err := s.UserStats()
	if err != nil || count != 2 {
		t.Fatalf("count=%d err=%v want 2", count, err)
	}
	total, err := s.TotalBalance()
	if err != nil || total != 1000000 {
		t.Fatalf("total=%d err=%v want 1000000", total, err)
	}
}
