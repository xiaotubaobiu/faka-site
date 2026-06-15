package store

import (
	"database/sql"
	"errors"
	"time"
)

type User struct {
	ID           int64
	Email        string
	PasswordHash string
	Role         string
	Balance      int64
	Status       int
	CreatedAt    int64
}

var ErrNotFound = errors.New("not found")

func now() int64 { return time.Now().Unix() }

func (s *Store) CreateUser(email, passwordHash, role string) (int64, error) {
	ts := now()
	res, err := s.db.Exec(
		`INSERT INTO users(email,password_hash,role,balance,status,created_at,updated_at) VALUES(?,?,?,?,1,?,?)`,
		email, passwordHash, role, 0, ts, ts)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UserByEmail(email string) (*User, error) {
	u := &User{}
	err := s.db.QueryRow(
		`SELECT id,email,password_hash,role,balance,status FROM users WHERE email=?`, email).
		Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Role, &u.Balance, &u.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (s *Store) UserByID(id int64) (*User, error) {
	u := &User{}
	err := s.db.QueryRow(
		`SELECT id,email,password_hash,role,balance,status FROM users WHERE id=?`, id).
		Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Role, &u.Balance, &u.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (s *Store) ListUsers() ([]User, error) {
	rows, err := s.db.Query(`SELECT id,email,role,balance,status FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Email, &u.Role, &u.Balance, &u.Status); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) SetUserStatus(id int64, status int) error {
	_, err := s.db.Exec(`UPDATE users SET status=?, updated_at=? WHERE id=?`, status, now(), id)
	return err
}

func (s *Store) SetUserPassword(id int64, hash string) error {
	_, err := s.db.Exec(`UPDATE users SET password_hash=?, updated_at=? WHERE id=?`, hash, now(), id)
	return err
}
