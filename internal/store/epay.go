package store

import (
	"database/sql"
	"fmt"
	"time"
)

// EpayOrder mirrors epay-gateway's model.Order: an incoming payment order
// placed by an upstream merchant (pid) against this gateway.
type EpayOrder struct {
	ID            int64
	TradeNo       string
	OutTradeNo    string
	PID           int
	Type          string
	Name          string
	Money         string
	Status        int
	NotifyURL     string
	ReturnURL     string
	Param         string
	AlipayTradeNo string
	CreatedAt     time.Time
	PaidAt        *time.Time
	NotifyCount   int
	Notified      bool
}

func (s *Store) EpayCreate(o *EpayOrder) error {
	_, err := s.db.Exec(
		`INSERT INTO epay_orders (trade_no, out_trade_no, pid, type, name, money, status, notify_url, return_url, param, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, 0, ?, ?, ?, ?)`,
		o.TradeNo, o.OutTradeNo, o.PID, o.Type, o.Name, o.Money, o.NotifyURL, o.ReturnURL, o.Param, o.CreatedAt,
	)
	return err
}

func (s *Store) EpayGetByTradeNo(tradeNo string) (*EpayOrder, error) {
	return s.epayGetOne("SELECT * FROM epay_orders WHERE trade_no = ?", tradeNo)
}

func (s *Store) EpayGetByOutTradeNo(pid int, outTradeNo string) (*EpayOrder, error) {
	return s.epayGetOne("SELECT * FROM epay_orders WHERE pid = ? AND out_trade_no = ?", pid, outTradeNo)
}

// EpayGetByOutTradeNoAny looks up an order by out_trade_no across all merchants.
// Used by official payment callbacks (alipay/wxpay), which only know the
// out_trade_no they were given at creation time — not the epay pid.
func (s *Store) EpayGetByOutTradeNoAny(outTradeNo string) (*EpayOrder, error) {
	return s.epayGetOne("SELECT * FROM epay_orders WHERE out_trade_no = ? ORDER BY id DESC LIMIT 1", outTradeNo)
}

func (s *Store) EpayUpdatePaid(tradeNo string, alipayTradeNo string) error {
	now := time.Now()
	res, err := s.db.Exec(
		`UPDATE epay_orders SET status = 1, alipay_trade_no = ?, paid_at = ? WHERE trade_no = ? AND status = 0`,
		alipayTradeNo, now, tradeNo,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("order not found or already paid: %s", tradeNo)
	}
	return nil
}

func (s *Store) EpayMarkNotified(tradeNo string) error {
	_, err := s.db.Exec(`UPDATE epay_orders SET notified = 1 WHERE trade_no = ?`, tradeNo)
	return err
}

func (s *Store) EpayIncrementNotifyCount(tradeNo string) error {
	_, err := s.db.Exec(`UPDATE epay_orders SET notify_count = notify_count + 1 WHERE trade_no = ?`, tradeNo)
	return err
}

func (s *Store) EpayListAll(limit int) ([]*EpayOrder, error) {
	rows, err := s.db.Query(
		`SELECT * FROM epay_orders ORDER BY id DESC LIMIT ?`, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return epayScanOrders(rows)
}

func (s *Store) EpayList(pid int, page, limit int) ([]*EpayOrder, error) {
	offset := (page - 1) * limit
	rows, err := s.db.Query(
		`SELECT * FROM epay_orders WHERE pid = ? ORDER BY id DESC LIMIT ? OFFSET ?`,
		pid, limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return epayScanOrders(rows)
}

func (s *Store) EpayCount(pid int) (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM epay_orders WHERE pid = ?`, pid).Scan(&count)
	return count, err
}

func (s *Store) epayGetOne(query string, args ...any) (*EpayOrder, error) {
	row := s.db.QueryRow(query, args...)
	var o EpayOrder
	var paidAt sql.NullTime
	err := row.Scan(
		&o.ID, &o.TradeNo, &o.OutTradeNo, &o.PID, &o.Type, &o.Name, &o.Money,
		&o.Status, &o.NotifyURL, &o.ReturnURL, &o.Param, &o.AlipayTradeNo,
		&o.CreatedAt, &paidAt, &o.NotifyCount, &o.Notified,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if paidAt.Valid {
		o.PaidAt = &paidAt.Time
	}
	return &o, nil
}

func epayScanOrders(rows *sql.Rows) ([]*EpayOrder, error) {
	var orders []*EpayOrder
	for rows.Next() {
		var o EpayOrder
		var paidAt sql.NullTime
		err := rows.Scan(
			&o.ID, &o.TradeNo, &o.OutTradeNo, &o.PID, &o.Type, &o.Name, &o.Money,
			&o.Status, &o.NotifyURL, &o.ReturnURL, &o.Param, &o.AlipayTradeNo,
			&o.CreatedAt, &paidAt, &o.NotifyCount, &o.Notified,
		)
		if err != nil {
			return nil, err
		}
		if paidAt.Valid {
			o.PaidAt = &paidAt.Time
		}
		orders = append(orders, &o)
	}
	return orders, nil
}
