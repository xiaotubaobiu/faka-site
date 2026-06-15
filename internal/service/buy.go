package service

import (
	"context"
	"fmt"
	"log"

	"faka-site/internal/newapi"
	"faka-site/internal/store"
)

type BuyResult struct {
	OrderID   int64
	Status    string // completed|partial|failed
	Codes     []string
	Succeeded int
	Failed    int
	Refunded  int64
}

type BuyService struct {
	Store  *store.Store
	Issuer newapi.Issuer
}

// Buy 扣费→发码→结算。count≥1, quotaPerCode>0。
func (b *BuyService) Buy(ctx context.Context, userID int64, count int, quotaPerCode int64) (*BuyResult, error) {
	if count < 1 || count > 10000 || quotaPerCode <= 0 || quotaPerCode > 1_000_000_000 {
		return nil, fmt.Errorf("invalid count or quota")
	}
	total := int64(count) * quotaPerCode
	if total/quotaPerCode != int64(count) {
		return nil, fmt.Errorf("order too large")
	}

	orderID, err := b.Store.HoldForOrder(ctx, userID, count, quotaPerCode, total)
	if err != nil {
		return nil, err
	}
	name := fmt.Sprintf("fk-%d", orderID)

	codes, genErr := b.Issuer.GenerateCodes(ctx, name, quotaPerCode, count)

	res := &BuyResult{OrderID: orderID, Codes: codes, Succeeded: len(codes)}
	switch {
	case genErr != nil && len(codes) == 0:
		res.Status = "failed"
		res.Failed = count
		if err := b.Store.SettleOrder(ctx, store.Order{ID: orderID, UserID: userID, QuotaPerCode: quotaPerCode, Status: "failed", FailedCount: count, RefundedAmount: total}, nil); err != nil {
			log.Printf("settle failed: orderID=%d status=%s refund=%d: %v", orderID, res.Status, res.Refunded, err)
		}
		res.Refunded = total
		return res, genErr
	case genErr != nil:
		failed := count - len(codes)
		refund := int64(failed) * quotaPerCode
		res.Status = "partial"
		res.Failed = failed
		res.Refunded = refund
		if err := b.Store.SettleOrder(ctx, store.Order{ID: orderID, UserID: userID, QuotaPerCode: quotaPerCode, Status: "partial", SucceededCount: len(codes), FailedCount: failed, RefundedAmount: refund}, codes); err != nil {
			log.Printf("settle failed: orderID=%d status=%s refund=%d: %v", orderID, res.Status, res.Refunded, err)
		}
		return res, genErr
	default:
		res.Status = "completed"
		if err := b.Store.SettleOrder(ctx, store.Order{ID: orderID, UserID: userID, QuotaPerCode: quotaPerCode, Status: "completed", SucceededCount: count}, codes); err != nil {
			log.Printf("settle failed: orderID=%d status=%s refund=%d: %v", orderID, res.Status, res.Refunded, err)
		}
		return res, nil
	}
}
