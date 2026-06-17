package payment

import (
	"context"
	"fmt"
	"net/http"
)

// MockProvider 用于本地开发与单测:无真实签名,直接信任回调参数。
type MockProvider struct{}

func (MockProvider) Name() string             { return "mock" }
func (MockProvider) Configured() bool         { return true }
func (MockProvider) NotifyOKResponse() string { return "success" }

func (MockProvider) CreatePayment(_ context.Context, req PaymentRequest) (PaymentResult, error) {
	return PaymentResult{QRCode: fmt.Sprintf("mock://%s?fen=%d", req.OutTradeNo, req.AmountFen)}, nil
}

func (MockProvider) ParseNotify(r *http.Request) (NotifyInfo, error) {
	_ = r.ParseForm()
	return NotifyInfo{
		OutTradeNo: r.FormValue("out_trade_no"),
		TradeNo:    r.FormValue("trade_no"),
		AmountFen:  parseFen(r.FormValue("total")),
	}, nil
}

func parseFen(s string) int64 {
	var n int64
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int64(c-'0')
	}
	return n
}
