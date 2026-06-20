package payment

import (
	"context"
	"net/http"
	"sync"
)

// PaymentRequest 创建支付所需参数。AmountFen 为人民币分。
type PaymentRequest struct {
	OutTradeNo string
	AmountFen  int64
	Subject    string
	NotifyURL  string
	ReturnURL  string
	// TimeoutExpress 可选,支付宝当面付订单超时时长(如 "5m"),到点未付自动关闭交易。
	TimeoutExpress string
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

// PaymentProvider 抽象官方支付渠道(支付宝/微信)。每个渠道自己负责下单、
// 验签和解析官方异步回调,使得 epay 网关无需关心具体协议差异。
type PaymentProvider interface {
	Name() string // "alipay" | "wxpay"
	Configured() bool
	CreatePayment(ctx context.Context, req PaymentRequest) (PaymentResult, error)
	ParseNotify(r *http.Request) (NotifyInfo, error) // 验签 + 解析官方回调
	NotifyOKResponse() string                        // 官方回调成功应答体(支付宝 "success",微信 JSON)
}

// Registry holds the active providers keyed by channel name. It is rebuilt only
// when the underlying credentials actually change (see SetProvidersIfChanged),
// so a high-frequency caller like the per-request config loader can invoke it
// on every request without re-parsing PEM keys or introducing races between a
// concurrent precreate and a poll.
type Registry struct {
	mu        sync.RWMutex
	providers map[string]PaymentProvider
	// fingerprint holds an opaque digest of the inputs that produced the
	// current providers. SetProvidersIfChanged compares against it to skip
	// redundant rebuilds.
	fingerprint string
}

var defaultRegistry = &Registry{providers: map[string]PaymentProvider{}}

// DefaultRegistry returns the process-wide registry that handlers consult.
func DefaultRegistry() *Registry { return defaultRegistry }

// SetProviders replaces all registered providers atomically. Pass nil/empty
// values for channels that aren't configured yet — Get will then report them
// as unavailable and return ErrNotConfigured on use.
//
// The fingerprint is forced to change, so this always rebuilds. Use
// SetProvidersIfChanged from hot paths to avoid redundant work.
func (r *Registry) SetProviders(ps ...PaymentProvider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers = map[string]PaymentProvider{}
	for _, p := range ps {
		if p == nil {
			continue
		}
		r.providers[p.Name()] = p
	}
	r.fingerprint = "" // next SetProvidersIfChanged("") with non-equal fp rebuilds
}

// SetProvidersIfChanged rebuilds the registry only when fingerprint differs
// from the one used to build the current providers. Returns true if a rebuild
// actually happened. Callers should compute the fingerprint from the same
// inputs they build the providers from (e.g. a hash of the relevant config
// map + key-file contents), so that identical inputs short-circuit here and
// avoid re-parsing PEM keys on every request.
func (r *Registry) SetProvidersIfChanged(fingerprint string, build func() []PaymentProvider) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if fingerprint == r.fingerprint {
		return false
	}
	r.providers = map[string]PaymentProvider{}
	for _, p := range build() {
		if p == nil {
			continue
		}
		r.providers[p.Name()] = p
	}
	r.fingerprint = fingerprint
	return true
}

// Get returns the provider for a channel ("alipay"|"wxpay"). The returned
// provider may be unconfigured (Configured()==false) if credentials are
// missing; callers should check before issuing payments.
func (r *Registry) Get(name string) (PaymentProvider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[name]
	return p, ok
}

// MustGet returns the provider for a channel, or a stub that reports
// unconfigured if no implementation is registered for that channel. This lets
// callers always call Configured() uniformly.
func (r *Registry) MustGet(name string) PaymentProvider {
	if p, ok := r.Get(name); ok {
		return p
	}
	return unconfiguredChannel{name}
}

// ConfiguredChannels reports which channels currently have valid credentials.
func (r *Registry) ConfiguredChannels() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []string
	for _, p := range r.providers {
		if p.Configured() {
			out = append(out, p.Name())
		}
	}
	return out
}

// unconfiguredChannel is a placeholder returned by MustGet when no real
// implementation is registered. Every method signals "not configured".
type unconfiguredChannel struct{ name string }

func (u unconfiguredChannel) Name() string     { return u.name }
func (u unconfiguredChannel) Configured() bool { return false }
func (u unconfiguredChannel) CreatePayment(context.Context, PaymentRequest) (PaymentResult, error) {
	return PaymentResult{}, ErrNotConfigured
}
func (u unconfiguredChannel) ParseNotify(*http.Request) (NotifyInfo, error) {
	return NotifyInfo{}, ErrNotConfigured
}
func (u unconfiguredChannel) NotifyOKResponse() string { return "success" }

// ErrNotConfigured indicates a channel has no credentials configured.
var ErrNotConfigured = errWrap("payment channel not configured")

type errString string

func (e errString) Error() string { return string(e) }
func errWrap(s string) error      { return errString(s) }
