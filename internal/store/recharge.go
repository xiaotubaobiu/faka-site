package store

import "context"

type RechargeOrder struct {
	ID         int64
	UserID     int64
	Provider   string
	OutTradeNo string
	AmountFen  int64
	Quota      int64
	TradeNo    string
	Status     string
	CreatedAt  int64
	PaidAt     int64 // 0 if unpaid
}

func (s *Store) CreateRechargeOrder(ctx context.Context, userID int64, provider, outTradeNo string, amountFen, quota int64) (*RechargeOrder, error) {
	ts := now()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO recharge_orders(user_id,provider,out_trade_no,amount_fen,quota,status,created_at)
		 VALUES(?,?,?, ?, ?,'pending', ?)`,
		userID, provider, outTradeNo, amountFen, quota, ts)
	if err != nil {
		return nil, err
	}
	return s.RechargeOrderByTradeNo(ctx, outTradeNo)
}

func (s *Store) RechargeOrder(ctx context.Context, id int64) (*RechargeOrder, error) {
	o := &RechargeOrder{}
	var paidAt int64
	err := s.db.QueryRowContext(ctx,
		`SELECT id,user_id,provider,out_trade_no,amount_fen,quota,COALESCE(trade_no,''),status,created_at,COALESCE(paid_at,0) FROM recharge_orders WHERE id=?`,
		id).
		Scan(&o.ID, &o.UserID, &o.Provider, &o.OutTradeNo, &o.AmountFen, &o.Quota, &o.TradeNo, &o.Status, &o.CreatedAt, &paidAt)
	if err != nil {
		return nil, err
	}
	o.PaidAt = paidAt
	return o, nil
}

func (s *Store) RechargeOrderByTradeNo(ctx context.Context, outTradeNo string) (*RechargeOrder, error) {
	o := &RechargeOrder{}
	var paidAt int64
	err := s.db.QueryRowContext(ctx,
		`SELECT id,user_id,provider,out_trade_no,amount_fen,quota,COALESCE(trade_no,''),status,created_at,COALESCE(paid_at,0) FROM recharge_orders WHERE out_trade_no=?`,
		outTradeNo).
		Scan(&o.ID, &o.UserID, &o.Provider, &o.OutTradeNo, &o.AmountFen, &o.Quota, &o.TradeNo, &o.Status, &o.CreatedAt, &paidAt)
	if err != nil {
		return nil, err
	}
	o.PaidAt = paidAt
	return o, nil
}

// ExpireStaleRecharges marks pending recharge orders created before cutoff
// (unix seconds) as expired, so unpaid orders stop showing as 待支付 and drop
// out of the user's recharge list. Returns the number expired.
func (s *Store) ExpireStaleRecharges(ctx context.Context, cutoff int64) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE recharge_orders SET status='expired' WHERE status='pending' AND created_at < ?`,
		cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *Store) RechargeOrdersByUser(ctx context.Context, userID int64) ([]RechargeOrder, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id,user_id,provider,out_trade_no,amount_fen,quota,COALESCE(trade_no,''),status,created_at,COALESCE(paid_at,0) FROM recharge_orders WHERE user_id=? AND status != 'expired' ORDER BY id DESC`,
		userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RechargeOrder
	for rows.Next() {
		var o RechargeOrder
		if err := rows.Scan(&o.ID, &o.UserID, &o.Provider, &o.OutTradeNo, &o.AmountFen, &o.Quota, &o.TradeNo, &o.Status, &o.CreatedAt, &o.PaidAt); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// SettleRecharge 幂等结算:仅 pending→paid 时加余额并记 ledger,返回是否本次到账。
// 重复回调(已 paid)返回 false,不重复加钱。金额以订单创建时记录的 quota 为准(不信任回调金额)。
func (s *Store) SettleRecharge(ctx context.Context, outTradeNo, tradeNo string) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	ts := now()
	res, err := tx.ExecContext(ctx,
		`UPDATE recharge_orders SET status='paid', trade_no=?, paid_at=? WHERE out_trade_no=? AND status='pending'`,
		tradeNo, ts, outTradeNo)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if n == 0 {
		return false, nil // 已支付或不存在
	}
	var userID, quota int64
	if err := tx.QueryRowContext(ctx, `SELECT user_id, quota FROM recharge_orders WHERE out_trade_no=?`, outTradeNo).Scan(&userID, &quota); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE users SET balance=balance+?, updated_at=? WHERE id=?`, quota, ts, userID); err != nil {
		return false, err
	}
	var bal int64
	if err := tx.QueryRowContext(ctx, `SELECT balance FROM users WHERE id=?`, userID).Scan(&bal); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO balance_ledger(user_id,delta,balance_after,reason,created_at) VALUES(?,?,?,?,?)`,
		userID, quota, bal, "recharge", ts); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}
