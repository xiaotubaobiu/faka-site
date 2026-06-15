package store

import (
	"context"
	"errors"
)

var ErrInsufficient = errors.New("insufficient balance")

// HoldForOrder: 原子校验+扣余额、建 pending 订单、记 purchase 流水。返回订单 id。
func (s *Store) HoldForOrder(ctx context.Context, userID int64, count int, quotaPerCode, total int64) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	ts := now()
	res, err := tx.ExecContext(ctx,
		`UPDATE users SET balance = balance - ?, updated_at = ? WHERE id = ? AND balance >= ?`,
		total, ts, userID, total)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if n == 0 {
		return 0, ErrInsufficient
	}

	var bal int64
	if err := tx.QueryRowContext(ctx, `SELECT balance FROM users WHERE id=?`, userID).Scan(&bal); err != nil {
		return 0, err
	}

	ord, err := tx.ExecContext(ctx,
		`INSERT INTO orders(user_id,code_count,quota_per_code,total_cost,status,created_at,updated_at)
		 VALUES(?,?,?,?,'pending',?,?)`,
		userID, count, quotaPerCode, total, ts, ts)
	if err != nil {
		return 0, err
	}
	orderID, _ := ord.LastInsertId()

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO balance_ledger(user_id,delta,balance_after,reason,order_id,created_at)
		 VALUES(?,?,?,?,?,?)`,
		userID, -total, bal, "purchase", orderID, ts); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return orderID, nil
}

func (s *Store) Refund(ctx context.Context, userID, orderID, amount int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	ts := now()
	if _, err := tx.ExecContext(ctx, `UPDATE users SET balance=balance+?, updated_at=? WHERE id=?`, amount, ts, userID); err != nil {
		return err
	}
	var bal int64
	if err := tx.QueryRowContext(ctx, `SELECT balance FROM users WHERE id=?`, userID).Scan(&bal); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO balance_ledger(user_id,delta,balance_after,reason,order_id,created_at) VALUES(?,?,?,?,?,?)`,
		userID, amount, bal, "refund", orderID, ts); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) AddBalance(ctx context.Context, userID, adminID, amount int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	ts := now()
	if _, err := tx.ExecContext(ctx, `UPDATE users SET balance=balance+?, updated_at=? WHERE id=?`, amount, ts, userID); err != nil {
		return err
	}
	var bal int64
	if err := tx.QueryRowContext(ctx, `SELECT balance FROM users WHERE id=?`, userID).Scan(&bal); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO balance_ledger(user_id,delta,balance_after,reason,admin_id,created_at) VALUES(?,?,?,?,?,?)`,
		userID, amount, bal, "admin_add", adminID, ts); err != nil {
		return err
	}
	return tx.Commit()
}
