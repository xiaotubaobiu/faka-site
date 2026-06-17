package store

import (
	"testing"
	"time"
)

func newTestEpayOrder() *EpayOrder {
	return &EpayOrder{
		TradeNo:    "EP20240101000001",
		OutTradeNo: "M001",
		PID:        1001,
		Type:       "alipay",
		Name:       "Test Product",
		Money:      "1.00",
		NotifyURL:  "https://example.com/notify",
		ReturnURL:  "https://example.com/return",
		CreatedAt:  time.Now(),
	}
}

func TestStore_EpayCreateAndGet(t *testing.T) {
	s, err := OpenInMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}

	if err := s.EpayCreate(newTestEpayOrder()); err != nil {
		t.Fatal(err)
	}

	got, err := s.EpayGetByTradeNo("EP20240101000001")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("order not found")
	}
	if got.OutTradeNo != "M001" {
		t.Errorf("OutTradeNo = %s, want M001", got.OutTradeNo)
	}
	if got.Status != 0 {
		t.Errorf("Status = %d, want 0", got.Status)
	}
	if got.Type != "alipay" {
		t.Errorf("Type = %s, want alipay", got.Type)
	}
	if got.CreatedAt.IsZero() {
		t.Error("CreatedAt not scanned")
	}
	if got.PaidAt != nil {
		t.Error("PaidAt should be nil for unpaid order")
	}
}

func TestStore_EpayGetByTradeNo_NotFound(t *testing.T) {
	s, _ := OpenInMemory()
	defer s.Close()
	s.Migrate()

	got, err := s.EpayGetByTradeNo("nope")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

func TestStore_EpayUpdatePaid(t *testing.T) {
	s, _ := OpenInMemory()
	defer s.Close()
	s.Migrate()

	if err := s.EpayCreate(newTestEpayOrder()); err != nil {
		t.Fatal(err)
	}

	if err := s.EpayUpdatePaid("EP20240101000001", "ALI123"); err != nil {
		t.Fatal(err)
	}

	got, _ := s.EpayGetByTradeNo("EP20240101000001")
	if got.Status != 1 {
		t.Errorf("Status = %d, want 1", got.Status)
	}
	if got.AlipayTradeNo != "ALI123" {
		t.Errorf("AlipayTradeNo = %s, want ALI123", got.AlipayTradeNo)
	}
	if got.PaidAt == nil {
		t.Error("PaidAt should be set after UpdatePaid")
	}

	// Second call is idempotent: should error "already paid".
	if err := s.EpayUpdatePaid("EP20240101000001", "ALI456"); err == nil {
		t.Error("expected error on duplicate UpdatePaid, got nil")
	}

	got2, _ := s.EpayGetByTradeNo("EP20240101000001")
	if got2.AlipayTradeNo != "ALI123" {
		t.Errorf("AlipayTradeNo changed after second UpdatePaid: %s", got2.AlipayTradeNo)
	}
}

func TestStore_EpayUpdatePaid_NotFound(t *testing.T) {
	s, _ := OpenInMemory()
	defer s.Close()
	s.Migrate()

	if err := s.EpayUpdatePaid("missing", "X"); err == nil {
		t.Error("expected error for unknown order, got nil")
	}
}

func TestStore_EpayFindUnpaidByAmount(t *testing.T) {
	s, _ := OpenInMemory()
	defer s.Close()
	s.Migrate()

	if err := s.EpayCreate(newTestEpayOrder()); err != nil {
		t.Fatal(err)
	}

	// Paid order with the same amount should be ignored.
	paid := newTestEpayOrder()
	paid.TradeNo = "EP_PAID"
	paid.OutTradeNo = "M_PAID"
	if err := s.EpayCreate(paid); err != nil {
		t.Fatal(err)
	}
	if err := s.EpayUpdatePaid("EP_PAID", "ALI"); err != nil {
		t.Fatal(err)
	}

	got, err := s.EpayFindUnpaidByAmount("1.00")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected an unpaid order, got nil")
	}
	if got.TradeNo != "EP20240101000001" {
		t.Errorf("TradeNo = %s, want EP20240101000001", got.TradeNo)
	}
	if got.Status != 0 {
		t.Errorf("Status = %d, want 0 (unpaid)", got.Status)
	}
}

func TestStore_EpayFindUnpaidByAmount_None(t *testing.T) {
	s, _ := OpenInMemory()
	defer s.Close()
	s.Migrate()

	got, err := s.EpayFindUnpaidByAmount("9.99")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

func TestStore_EpayNotifiedCounters(t *testing.T) {
	s, _ := OpenInMemory()
	defer s.Close()
	s.Migrate()

	if err := s.EpayCreate(newTestEpayOrder()); err != nil {
		t.Fatal(err)
	}
	if err := s.EpayIncrementNotifyCount("EP20240101000001"); err != nil {
		t.Fatal(err)
	}
	if err := s.EpayIncrementNotifyCount("EP20240101000001"); err != nil {
		t.Fatal(err)
	}
	if err := s.EpayMarkNotified("EP20240101000001"); err != nil {
		t.Fatal(err)
	}

	got, _ := s.EpayGetByTradeNo("EP20240101000001")
	if got.NotifyCount != 2 {
		t.Errorf("NotifyCount = %d, want 2", got.NotifyCount)
	}
	if !got.Notified {
		t.Error("Notified = false, want true")
	}
}

func TestStore_EpayListAndCount(t *testing.T) {
	s, _ := OpenInMemory()
	defer s.Close()
	s.Migrate()

	for i := 0; i < 5; i++ {
		o := newTestEpayOrder()
		o.TradeNo = "EP" + string(rune('A'+i))
		o.OutTradeNo = "M" + string(rune('A'+i))
		o.PID = 1001
		if err := s.EpayCreate(o); err != nil {
			t.Fatal(err)
		}
	}
	// Different pid.
	other := newTestEpayOrder()
	other.TradeNo = "EP_OTHER"
	other.OutTradeNo = "M_OTHER"
	other.PID = 2002
	s.EpayCreate(other)

	all, err := s.EpayListAll(100)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 6 {
		t.Errorf("EpayListAll len = %d, want 6", len(all))
	}

	list, err := s.EpayList(1001, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Errorf("EpayList page len = %d, want 2", len(list))
	}

	page2, err := s.EpayList(1001, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page2) != 2 {
		t.Errorf("EpayList page2 len = %d, want 2", len(page2))
	}

	page3, err := s.EpayList(1001, 3, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page3) != 1 {
		t.Errorf("EpayList page3 len = %d, want 1 (remainder)", len(page3))
	}

	count, err := s.EpayCount(1001)
	if err != nil {
		t.Fatal(err)
	}
	if count != 5 {
		t.Errorf("EpayCount = %d, want 5", count)
	}
}
