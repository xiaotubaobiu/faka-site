package store

import "context"

type Order struct {
	ID             int64
	UserID         int64
	CodeCount      int
	QuotaPerCode   int64
	TotalCost      int64
	Status         string
	SucceededCount int
	FailedCount    int
	RefundedAmount int64
}

// SettleOrder 结算:更新订单状态/计数、写入成功码;refund>0 时退余额并记 refund 流水。
func (s *Store) SettleOrder(ctx context.Context, o Order, codes []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	ts := now()

	if _, err := tx.ExecContext(ctx,
		`UPDATE orders SET status=?, succeeded_count=?, failed_count=?, refunded_amount=?, updated_at=? WHERE id=?`,
		o.Status, o.SucceededCount, o.FailedCount, o.RefundedAmount, ts, o.ID); err != nil {
		return err
	}
	for _, c := range codes {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO order_codes(order_id,user_id,code,quota,created_at) VALUES(?,?,?,?,?)`,
			o.ID, o.UserID, c, o.QuotaPerCode, ts); err != nil {
			return err
		}
	}
	if o.RefundedAmount > 0 {
		if _, err := tx.ExecContext(ctx, `UPDATE users SET balance=balance+?, updated_at=? WHERE id=?`, o.RefundedAmount, ts, o.UserID); err != nil {
			return err
		}
		var bal int64
		if err := tx.QueryRowContext(ctx, `SELECT balance FROM users WHERE id=?`, o.UserID).Scan(&bal); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO balance_ledger(user_id,delta,balance_after,reason,order_id,created_at) VALUES(?,?,?,?,?,?)`,
			o.UserID, o.RefundedAmount, bal, "refund", o.ID, ts); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) Order(ctx context.Context, id int64) (*Order, error) {
	o := &Order{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id,user_id,code_count,quota_per_code,total_cost,status,succeeded_count,failed_count,refunded_amount FROM orders WHERE id=?`, id).
		Scan(&o.ID, &o.UserID, &o.CodeCount, &o.QuotaPerCode, &o.TotalCost, &o.Status, &o.SucceededCount, &o.FailedCount, &o.RefundedAmount)
	if err != nil {
		return nil, err
	}
	return o, nil
}

func (s *Store) OrderCodes(ctx context.Context, orderID int64) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT code FROM order_codes WHERE order_id=? ORDER BY id`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) OrdersByUser(ctx context.Context, userID int64) ([]Order, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id,user_id,code_count,quota_per_code,total_cost,status,succeeded_count,failed_count,refunded_amount FROM orders WHERE user_id=? ORDER BY id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Order
	for rows.Next() {
		var o Order
		if err := rows.Scan(&o.ID, &o.UserID, &o.CodeCount, &o.QuotaPerCode, &o.TotalCost, &o.Status, &o.SucceededCount, &o.FailedCount, &o.RefundedAmount); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// CountOrdersByUser 返回某用户的订单总数。
func (s *Store) CountOrdersByUser(ctx context.Context, userID int64) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM orders WHERE user_id=?`, userID).Scan(&n)
	return n, err
}

// RecentOrdersByUser 返回某用户最近的 limit 条订单(id 倒序)。
func (s *Store) RecentOrdersByUser(ctx context.Context, userID int64, limit int) ([]Order, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id,user_id,code_count,quota_per_code,total_cost,status,succeeded_count,failed_count,refunded_amount
		 FROM orders WHERE user_id=? ORDER BY created_at DESC, id DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Order
	for rows.Next() {
		var o Order
		if err := rows.Scan(&o.ID, &o.UserID, &o.CodeCount, &o.QuotaPerCode, &o.TotalCost, &o.Status, &o.SucceededCount, &o.FailedCount, &o.RefundedAmount); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// SumUsedByUser 返回某用户 created_at>=since 的订单 total_cost 之和(无则 0)。
func (s *Store) SumUsedByUser(ctx context.Context, userID, since int64) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(total_cost),0) FROM orders WHERE user_id=? AND created_at>=?`, userID, since).Scan(&n)
	return n, err
}
