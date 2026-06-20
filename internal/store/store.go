package store

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(on)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func OpenInMemory() (*Store, error) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return nil, err
	}
	// Each connection to a :memory: database gets its OWN private database, so
	// a goroutine (e.g. an async payment downstream-notify) that happens to
	// grab a different connection would see an empty store and silently lose
	// writes. Pin to a single connection so all access shares one database.
	// The file-backed Open() already does the same thing.
	db.SetMaxOpenConns(1)
	return &Store{db: db}, nil
}

func (s *Store) Migrate() error {
	_, err := s.db.Exec(schema)
	return err
}

func (s *Store) Close() error { return s.db.Close() }
