package payment

import (
	"context"
	"net/http"
)

// PaymentRequest 创建支付所需参数。AmountFen 为人民币分。
type PaymentRequest struct {
	OutTradeNo string
	AmountFen  int64
	Subject    string
	NotifyURL  string
	ReturnURL  string
}

// PaymentResult 返回支付展示信息。QRCode 为二维码内容(可为空),PayURL 为跳转地址(可为空)。
type PaymentResult struct {
	QRCode string
	PayURL string
}

// NotifyInfo 是验签后的回调信息。
type NotifyInfo struct {
	OutTradeNo string
	TradeNo    string
	AmountFen  int64
}

// PaymentProvider 抽象支付渠道,便于注入 Mock 做单测。
type PaymentProvider interface {
	Name() string // "alipay" | "wxpay"
	Configured() bool
	CreatePayment(ctx context.Context, req PaymentRequest) (PaymentResult, error)
	ParseNotify(r *http.Request) (NotifyInfo, error)         // 验签 + 解析回调
	NotifyOKResponse() string                                 // 回调成功应答体(支付宝 "success",微信 JSON)
}
