package epay

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"faka-site/internal/payment"
	"faka-site/internal/store"
	"golang.org/x/crypto/bcrypt"
)

// Handler serves the epay-gateway endpoints. cfg is a provider (read fresh on
// each request) so config edits made via the faka-site admin take effect
// without a restart. The payment registry supplies the real official-payment
// providers (alipay/wxpay).
type Handler struct {
	store    *store.Store
	cfg      func() Config
	registry *payment.Registry
}

func New(st *store.Store, cfg func() Config) *Handler {
	return &Handler{store: st, cfg: cfg, registry: payment.DefaultRegistry()}
}

func (h *Handler) writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func (h *Handler) writeError(w http.ResponseWriter, code int, msg string) {
	h.writeJSON(w, code, map[string]any{"code": 0, "msg": msg})
}

func (h *Handler) writeSuccess(w http.ResponseWriter, data map[string]any) {
	data["code"] = 1
	h.writeJSON(w, http.StatusOK, data)
}

func (h *Handler) WithAdminAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !h.requireAdmin(w, r) {
			return
		}
		next(w, r)
	}
}

func (h *Handler) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	c := h.cfg()
	if !c.HasAdminAuth() {
		http.Error(w, "admin auth not configured", http.StatusServiceUnavailable)
		return false
	}

	if _, err := bcrypt.Cost([]byte(c.Admin.PasswordHash)); err != nil {
		http.Error(w, "admin auth misconfigured", http.StatusServiceUnavailable)
		return false
	}

	username, password, ok := r.BasicAuth()
	if ok {
		userOK := subtle.ConstantTimeCompare([]byte(username), []byte(c.Admin.Username)) == 1
		passwordOK := bcrypt.CompareHashAndPassword([]byte(c.Admin.PasswordHash), []byte(password)) == nil
		if userOK && passwordOK {
			return true
		}
	}

	w.Header().Set("WWW-Authenticate", `Basic realm="EPay Gateway Admin"`)
	http.Error(w, "unauthorized", http.StatusUnauthorized)
	return false
}

func (h *Handler) validateSign(w http.ResponseWriter, r *http.Request) string {
	c := h.cfg()
	pidStr := r.FormValue("pid")
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid pid")
		return ""
	}
	key := c.FindMerchant(pid)
	if key == "" {
		h.writeError(w, http.StatusForbidden, "invalid pid")
		return ""
	}

	if r.Method == http.MethodPost {
		r.ParseForm()
	}

	params := r.URL.Query()
	if r.Method == http.MethodPost {
		for k, vs := range r.PostForm {
			if params.Get(k) == "" && len(vs) > 0 {
				params.Set(k, vs[0])
			}
		}
	}

	if !Verify(params, key) {
		h.writeError(w, http.StatusForbidden, "sign verification failed")
		return ""
	}
	return key
}

func (h *Handler) parseCreateParams(w http.ResponseWriter, r *http.Request) (*store.EpayOrder, string) {
	pid, _ := strconv.Atoi(r.FormValue("pid"))
	payType := r.FormValue("type")
	if payType == "" {
		payType = "alipay"
	}
	outTradeNo := r.FormValue("out_trade_no")
	notifyURL := r.FormValue("notify_url")
	returnURL := r.FormValue("return_url")
	name := r.FormValue("name")
	money := r.FormValue("money")
	param := r.FormValue("param")

	if outTradeNo == "" || notifyURL == "" || name == "" || money == "" {
		h.writeError(w, http.StatusBadRequest, "missing required parameters")
		return nil, ""
	}

	existing, err := h.store.EpayGetByOutTradeNo(pid, outTradeNo)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "system error")
		return nil, ""
	}
	if existing != nil && existing.Status == 0 {
		h.writeError(w, http.StatusBadRequest, "duplicate out_trade_no with unpaid order")
		return nil, ""
	}

	tradeNo := fmt.Sprintf("EP%d", time.Now().UnixNano()/1e6)

	return &store.EpayOrder{
		TradeNo:    tradeNo,
		OutTradeNo: outTradeNo,
		PID:        pid,
		Type:       payType,
		Name:       name,
		Money:      money,
		NotifyURL:  notifyURL,
		ReturnURL:  returnURL,
		Param:      param,
		CreatedAt:  time.Now(),
	}, payType
}

// notifyURLForChannel returns the official callback URL for a channel, built
// from the configured public base. This is the URL we tell the official payment
// API to call after the buyer pays.
func (h *Handler) notifyURLForChannel(channel string) string {
	base := h.cfg().NotifyBase
	if base == "" {
		return ""
	}
	return base + "/notify/" + channel
}

var payPageTpl = template.Must(template.New("pay").Parse(`<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Name}} - 收款</title>
<script src="https://cdn.jsdelivr.net/npm/qrcodejs@1.0.0/qrcode.min.js"></script>
<style>
body{font-family:-apple-system,BlinkMacSystemFont,sans-serif;display:flex;justify-content:center;align-items:center;min-height:100vh;margin:0;background:#f5f5f5}
.card{background:#fff;border-radius:12px;padding:32px;text-align:center;box-shadow:0 2px 12px rgba(0,0,0,.1);max-width:360px;width:100%}
h2{margin:0 0 8px;color:#333}
.price{font-size:32px;color:#ff6a00;margin:12px 0}
.price small{font-size:16px}
#qrcode{display:flex;justify-content:center;margin:20px 0}
.tip{color:#999;font-size:14px}
.status{margin-top:16px;padding:8px;border-radius:6px;display:none}
.status.pending{display:block;background:#fff3cd;color:#856404}
.status.paid{display:block;background:#d4edda;color:#155724}
.status.error{display:block;background:#f8d7da;color:#721c24}
</style>
</head>
<body>
<div class="card">
<h2>{{.Name}}</h2>
<div class="price"><small>&yen;</small>{{.Money}}</div>
{{if .QRCode}}
<div id="qrcode"></div>
<p class="tip">请使用{{.PayTypeName}}扫码支付</p>
<div id="status" class="status pending">等待支付中...</div>
{{else}}
<div id="status" class="status error">{{.ErrMsg}}</div>
{{end}}
</div>
{{if .QRCode}}
<script>
new QRCode(document.getElementById("qrcode"),{text:"{{.QRCode}}",width:200,height:200});
var tradeNo="{{.TradeNo}}";
function checkStatus(){
 fetch("/api.php?act=order&pid={{.PID}}&key={{.Key}}&trade_no="+tradeNo)
 .then(function(r){return r.json()})
 .then(function(d){
  if(d.data && d.data.status===1){
   document.getElementById("status").className="status paid";
   document.getElementById("status").textContent="支付成功";
   setTimeout(function(){
    if("{{.ReturnURL}}"!=""){window.location.href="{{.ReturnURL}}";}
   },1500);
  }else{setTimeout(checkStatus,3000);}
 })
 .catch(function(){setTimeout(checkStatus,5000);});
}
setTimeout(checkStatus,3000);
</script>
{{end}}
</body>
</html>`))

// Submit renders the epay payment page (HTML) with a real official QR code.
func (h *Handler) Submit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	key := h.validateSign(w, r)
	if key == "" {
		return
	}

	order, payType := h.parseCreateParams(w, r)
	if order == nil {
		return
	}

	qrCode, errMsg := h.createOfficialPayment(r.Context(), order, payType)

	// Persist the order regardless: even on payment-create failure we keep a
	// record so the merchant can reconcile, and the page surfaces the error.
	if err := h.store.EpayCreate(order); err != nil {
		h.writeError(w, http.StatusInternalServerError, "create order failed")
		return
	}

	pid, _ := strconv.Atoi(r.FormValue("pid"))
	payTypeName := payType
	if payTypeName == "wxpay" {
		payTypeName = "微信"
	} else {
		payTypeName = "支付宝"
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	payPageTpl.Execute(w, map[string]string{
		"Name":        order.Name,
		"Money":       order.Money,
		"QRCode":      qrCode,
		"ErrMsg":      errMsg,
		"TradeNo":     order.TradeNo,
		"PID":         strconv.Itoa(pid),
		"Key":         key,
		"ReturnURL":   order.ReturnURL,
		"PayTypeName": payTypeName,
	})
}

// Mapi returns the epay machine API JSON response with the real QR code.
func (h *Handler) Mapi(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	key := h.validateSign(w, r)
	if key == "" {
		return
	}

	order, payType := h.parseCreateParams(w, r)
	if order == nil {
		return
	}

	qrCode, errMsg := h.createOfficialPayment(r.Context(), order, payType)
	if errMsg != "" {
		h.writeError(w, http.StatusBadRequest, errMsg)
		return
	}

	if err := h.store.EpayCreate(order); err != nil {
		h.writeError(w, http.StatusInternalServerError, "create order failed")
		return
	}

	_ = key
	h.writeSuccess(w, map[string]any{
		"trade_no": order.TradeNo,
		"qrcode":   qrCode,
	})
}

// createOfficialPayment asks the official provider (alipay/wxpay) for a real
// dynamic QR code for this order. On success returns the QR string; on failure
// returns an empty QRCode and a user-facing error message.
func (h *Handler) createOfficialPayment(ctx context.Context, order *store.EpayOrder, payType string) (qrCode, errMsg string) {
	provider, ok := h.registry.Get(payType)
	if !ok || !provider.Configured() {
		return "", "该支付渠道(" + payType + ")未配置,请联系管理员"
	}
	amountFen, err := yuanStringToFen(order.Money)
	if err != nil || amountFen <= 0 {
		return "", "订单金额无效"
	}
	timeout := h.cfg().OrderTimeout
	if timeout <= 0 {
		timeout = 5
	}
	res, err := provider.CreatePayment(ctx, payment.PaymentRequest{
		OutTradeNo:     order.OutTradeNo,
		AmountFen:      amountFen,
		Subject:        order.Name,
		NotifyURL:      h.notifyURLForChannel(payType),
		ReturnURL:      order.ReturnURL,
		TimeoutExpress: fmt.Sprintf("%dm", timeout),
	})
	if err != nil {
		log.Printf("epay: create payment failed otn=%s channel=%s: %v", order.OutTradeNo, payType, err)
		return "", "下单失败:" + err.Error()
	}
	return res.QRCode, ""
}

// OfficialNotify handles asynchronous callbacks from Alipay/WeChat. It delegates
// to the matching provider for signature verification + decryption, then marks
// the epay order paid and forwards a signed epay notification downstream.
func (h *Handler) OfficialNotify(channel string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		provider, ok := h.registry.Get(channel)
		if !ok || !provider.Configured() {
			log.Printf("notify/%s: channel not configured", channel)
			w.WriteHeader(http.StatusOK)
			return
		}
		info, err := provider.ParseNotify(r)
		if err != nil {
			log.Printf("notify/%s: parse/verify failed: %v", channel, err)
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(provider.NotifyOKResponse()))
			return
		}
		// Find the epay order by the merchant's out_trade_no. The official
		// payment was created with order.OutTradeNo as the official out_trade_no.
		order, err := h.store.EpayGetByOutTradeNoAny(info.OutTradeNo)
		if err != nil {
			log.Printf("notify/%s: load order otn=%s: %v", channel, info.OutTradeNo, err)
		}
		if order != nil {
			if err := h.store.EpayUpdatePaid(order.TradeNo, info.TradeNo); err != nil {
				log.Printf("notify/%s: mark paid otn=%s: %v", channel, info.OutTradeNo, err)
			} else {
				log.Printf("notify/%s: settled otn=%s trade=%s", channel, info.OutTradeNo, info.TradeNo)
				go h.notifyDownstream(order)
			}
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(provider.NotifyOKResponse()))
	}
}

func (h *Handler) API(w http.ResponseWriter, r *http.Request) {
	act := r.URL.Query().Get("act")
	switch act {
	case "order":
		h.queryOrder(w, r)
	case "orders":
		h.queryOrders(w, r)
	case "query":
		h.queryMerchant(w, r)
	default:
		h.writeError(w, http.StatusBadRequest, "unknown act")
	}
}

func (h *Handler) queryOrder(w http.ResponseWriter, r *http.Request) {
	pidStr := r.URL.Query().Get("pid")
	keyParam := r.URL.Query().Get("key")
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid pid")
		return
	}

	c := h.cfg()
	key := c.FindMerchant(pid)
	if key == "" || key != keyParam {
		h.writeError(w, http.StatusForbidden, "invalid pid or key")
		return
	}

	tradeNo := r.URL.Query().Get("trade_no")
	outTradeNo := r.URL.Query().Get("out_trade_no")

	var order *store.EpayOrder
	if tradeNo != "" {
		order, err = h.store.EpayGetByTradeNo(tradeNo)
	} else if outTradeNo != "" {
		order, err = h.store.EpayGetByOutTradeNo(pid, outTradeNo)
	} else {
		h.writeError(w, http.StatusBadRequest, "trade_no or out_trade_no required")
		return
	}

	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "query error")
		return
	}
	if order == nil {
		h.writeError(w, http.StatusNotFound, "order not found")
		return
	}

	result := map[string]any{
		"trade_no":     order.TradeNo,
		"out_trade_no": order.OutTradeNo,
		"api_trade_no": order.AlipayTradeNo,
		"type":         order.Type,
		"pid":          order.PID,
		"name":         order.Name,
		"money":        order.Money,
		"status":       order.Status,
		"param":        order.Param,
		"addtime":      order.CreatedAt.Format("2006-01-02 15:04:05"),
	}
	if order.PaidAt != nil {
		result["endtime"] = order.PaidAt.Format("2006-01-02 15:04:05")
	}
	h.writeSuccess(w, result)
}

func (h *Handler) queryOrders(w http.ResponseWriter, r *http.Request) {
	pidStr := r.URL.Query().Get("pid")
	keyParam := r.URL.Query().Get("key")
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid pid")
		return
	}

	c := h.cfg()
	key := c.FindMerchant(pid)
	if key == "" || key != keyParam {
		h.writeError(w, http.StatusForbidden, "invalid pid or key")
		return
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page <= 0 {
		page = 1
	}

	orders, err := h.store.EpayList(pid, page, limit)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "query error")
		return
	}

	type orderItem struct {
		TradeNo    string `json:"trade_no"`
		OutTradeNo string `json:"out_trade_no"`
		Type       string `json:"type"`
		Name       string `json:"name"`
		Money      string `json:"money"`
		Status     int    `json:"status"`
		CreatedAt  string `json:"addtime"`
	}

	items := make([]orderItem, 0, len(orders))
	for _, o := range orders {
		items = append(items, orderItem{
			TradeNo:    o.TradeNo,
			OutTradeNo: o.OutTradeNo,
			Type:       o.Type,
			Name:       o.Name,
			Money:      o.Money,
			Status:     o.Status,
			CreatedAt:  o.CreatedAt.Format("2006-01-02 15:04:05"),
		})
	}

	h.writeSuccess(w, map[string]any{
		"data": items,
	})
}

func (h *Handler) queryMerchant(w http.ResponseWriter, r *http.Request) {
	pidStr := r.URL.Query().Get("pid")
	keyParam := r.URL.Query().Get("key")
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid pid")
		return
	}

	c := h.cfg()
	key := c.FindMerchant(pid)
	if key == "" || key != keyParam {
		h.writeError(w, http.StatusForbidden, "invalid pid or key")
		return
	}

	count, _ := h.store.EpayCount(pid)

	h.writeSuccess(w, map[string]any{
		"pid":    pid,
		"key":    keyParam,
		"active": 1,
		"money":  "0.00",
		"orders": count,
	})
}

func (h *Handler) notifyDownstream(o *store.EpayOrder) {
	c := h.cfg()
	key := c.FindMerchant(o.PID)
	if key == "" {
		return
	}

	params := url.Values{}
	params.Set("pid", strconv.Itoa(o.PID))
	params.Set("trade_no", o.TradeNo)
	params.Set("out_trade_no", o.OutTradeNo)
	params.Set("type", o.Type)
	params.Set("name", o.Name)
	params.Set("money", o.Money)
	params.Set("trade_status", "TRADE_SUCCESS")
	params.Set("param", o.Param)
	params.Set("sign", Sign(params, key))
	params.Set("sign_type", "MD5")

	notifyURL := o.NotifyURL + "?" + params.Encode()

	for i := 0; i < 5; i++ {
		resp, err := http.Get(notifyURL)
		if err != nil {
			log.Printf("notify downstream error (attempt %d): %v", i+1, err)
			time.Sleep(time.Duration(i+1) * 3 * time.Second)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode == http.StatusOK && string(body) == "success" {
			h.store.EpayMarkNotified(o.TradeNo)
			log.Printf("downstream notified: %s", o.TradeNo)
			return
		}

		log.Printf("downstream returned: %s (attempt %d)", string(body), i+1)
		time.Sleep(time.Duration(i+1) * 3 * time.Second)
	}

	h.store.EpayIncrementNotifyCount(o.TradeNo)
	log.Printf("downstream notification failed: %s", o.TradeNo)
}

var adminTpl = template.Must(template.New("admin").Funcs(template.FuncMap{
	"formatTime": func(t time.Time) string { return t.Format("2006-01-02 15:04:05") },
	"formatPtrTime": func(t *time.Time) string {
		if t == nil {
			return "-"
		}
		return t.Format("2006-01-02 15:04:05")
	},
}).Parse(`<!DOCTYPE html>
<html lang="zh">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>EPay Gateway 管理</title>
<style>
*{margin:0;padding:0;box-sizing:border-box}
body{font-family:-apple-system,BlinkMacSystemFont,sans-serif;background:#f0f2f5;color:#333}
.nav{background:#fff;border-bottom:1px solid #e8e8e8;padding:0 24px;display:flex;align-items:center;height:48px}
.nav h1{font-size:16px;font-weight:600}
.nav .links{margin-left:auto;display:flex;gap:16px}
.nav a{color:#666;text-decoration:none;font-size:14px}
.nav a:hover{color:#1677ff}
.container{max-width:960px;margin:24px auto;padding:0 16px}
.card{background:#fff;border-radius:8px;padding:24px;margin-bottom:16px;box-shadow:0 1px 4px rgba(0,0,0,.08)}
.card h2{font-size:16px;margin-bottom:16px;padding-bottom:8px;border-bottom:1px solid #f0f0f0}
table{width:100%;border-collapse:collapse;font-size:13px}
th,td{padding:8px 12px;text-align:left;border-bottom:1px solid #f0f0f0}
th{background:#fafafa;font-weight:500;color:#666}
.status-0{color:#fa8c16}
.status-1{color:#52c41a}
.status-2{color:#999}
.empty{text-align:center;color:#999;padding:24px}
</style>
</head>
<body>
<div class="nav">
<h1>EPay Gateway</h1>
<div class="links">
<a href="/epay/admin">首页</a>
<a href="/epay/admin?q=orders">订单</a>
</div>
</div>
<div class="container">

{{if eq .Tab "orders"}}
<div class="card">
<h2>订单列表</h2>
{{if .Orders}}
<table>
<tr><th>订单号</th><th>商户订单号</th><th>商品</th><th>金额</th><th>状态</th><th>创建时间</th><th>支付时间</th></tr>
{{range .Orders}}
<tr>
<td>{{.TradeNo}}</td>
<td>{{.OutTradeNo}}</td>
<td>{{.Name}}</td>
<td>{{.Money}}</td>
<td class="status-{{.Status}}">{{if eq .Status 0}}待支付{{else if eq .Status 1}}已支付{{else}}已过期{{end}}</td>
<td>{{.CreatedAt | formatTime}}</td>
<td>{{.PaidAt | formatPtrTime}}</td>
</tr>
{{end}}
</table>
{{else}}
<div class="empty">暂无订单</div>
{{end}}
</div>
{{end}}

{{if eq .Tab "home"}}
<div class="card">
<h2>系统状态</h2>
<table>
<tr><th>项目</th><th>值</th></tr>
<tr><td>服务地址</td><td>{{.BaseURL}}</td></tr>
<tr><td>主商户 PID</td><td>{{if .PrimaryMerchant.PID}}{{.PrimaryMerchant.PID}}{{else}}未配置{{end}}</td></tr>
<tr><td>支付宝渠道</td><td>{{if .AlipayReady}}已配置{{else}}未配置{{end}}</td></tr>
<tr><td>微信渠道</td><td>{{if .WxpayReady}}已配置{{else}}未配置{{end}}</td></tr>
<tr><td>今日订单</td><td>{{.TodayOrders}}</td></tr>
</table>
</div>

<div class="card">
<h2>对接信息</h2>
<div class="form-row">
<label>支付网关</label>
<input type="text" readonly value="{{.BaseURL}}" style="background:#f5f5f5" />
</div>
<div class="form-row">
<label>商户 PID</label>
<input type="text" readonly value="{{if .PrimaryMerchant.PID}}{{.PrimaryMerchant.PID}}{{end}}" style="background:#f5f5f5" />
</div>
<div class="form-row">
<label>商户密钥</label>
<input type="text" readonly value="{{.PrimaryMerchant.Key}}" style="background:#f5f5f5" />
</div>
<div class="form-row">
<label>支付宝回调</label>
<input type="text" readonly value="{{.BaseURL}}/notify/alipay" style="background:#f5f5f5" />
</div>
<div class="form-row">
<label>微信回调</label>
<input type="text" readonly value="{{.BaseURL}}/notify/wxpay" style="background:#f5f5f5" />
</div>
</div>
{{end}}

</div>
</body>
</html>`))

func (h *Handler) Admin(w http.ResponseWriter, r *http.Request) {
	c := h.cfg()
	tab := r.URL.Query().Get("q")
	if tab == "" {
		tab = "home"
	}

	primaryMerchant := c.PrimaryMerchant()
	aliProv, _ := h.registry.Get("alipay")
	wxProv, _ := h.registry.Get("wxpay")
	aliReady := aliProv != nil && aliProv.Configured()
	wxReady := wxProv != nil && wxProv.Configured()

	data := map[string]any{
		"Tab":             tab,
		"PrimaryMerchant": primaryMerchant,
		"AlipayReady":     aliReady,
		"WxpayReady":      wxReady,
		"BaseURL":         getBaseURL(r),
		"TodayOrders":     0,
	}

	if tab == "orders" {
		orders, err := h.store.EpayListAll(50)
		if err == nil {
			data["Orders"] = orders
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	adminTpl.Execute(w, data)
}

func getBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// yuanStringToFen parses a "12.34" money string to integer fen.
func yuanStringToFen(s string) (int64, error) {
	var f float64
	_, err := fmt.Sscanf(s, "%f", &f)
	if err != nil {
		return 0, err
	}
	return int64(f * 100), nil
}
