package epay

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"faka-site/internal/store"
	"golang.org/x/crypto/bcrypt"
)

// Handler serves the epay-gateway endpoints. cfg is a provider (read fresh on
// each request) so config edits made via the faka-site admin take effect
// without a restart.
type Handler struct {
	store *store.Store
	cfg   func() Config
}

func New(st *store.Store, cfg func() Config) *Handler {
	return &Handler{store: st, cfg: cfg}
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
		for k, vs := range r.Form {
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

func isMobile(ua string) bool {
	keywords := []string{"Android", "iPhone", "iPad", "iPod", "Mobile"}
	for _, kw := range keywords {
		if strings.Contains(ua, kw) {
			return true
		}
	}
	return false
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
</style>
</head>
<body>
<div class="card">
<h2>{{.Name}}</h2>
<div class="price"><small>&yen;</small>{{.Money}}</div>
<div id="qrcode"></div>
<p class="tip">请使用{{.PayTypeName}}扫码支付</p>
<div id="status" class="status pending">等待支付中...</div>
</div>
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
</body>
</html>`))

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

	c := h.cfg()
	qr, ok := c.QRCodes[payType]
	if !ok {
		qr = c.QRCodes["alipay"]
	}

	if err := h.store.EpayCreate(order); err != nil {
		h.writeError(w, http.StatusInternalServerError, "create order failed")
		return
	}

	pid, _ := strconv.Atoi(r.FormValue("pid"))
	payTypeName := qr.Name
	if payTypeName == "" {
		if payType == "wxpay" {
			payTypeName = "微信"
		} else {
			payTypeName = "支付宝"
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	payPageTpl.Execute(w, map[string]string{
		"Name":        order.Name,
		"Money":       order.Money,
		"QRCode":      qr.URL,
		"TradeNo":     order.TradeNo,
		"PID":         strconv.Itoa(pid),
		"Key":         key,
		"ReturnURL":   order.ReturnURL,
		"PayTypeName": payTypeName,
	})
}

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

	c := h.cfg()
	qr, ok := c.QRCodes[payType]
	if !ok {
		qr = c.QRCodes["alipay"]
	}

	if err := h.store.EpayCreate(order); err != nil {
		h.writeError(w, http.StatusInternalServerError, "create order failed")
		return
	}

	h.writeSuccess(w, map[string]any{
		"trade_no": order.TradeNo,
		"qrcode":   qr.URL,
	})
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

// SmsForwarder POST webhook body
type smsBody struct {
	From    string `json:"from"`
	Content string `json:"content"`
	Secret  string `json:"secret"`
}

var moneyRegex = regexp.MustCompile(`([\d]+\.?\d*)元`)

func (h *Handler) SmsNotify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var body smsBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		log.Printf("sms notify: decode error: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	c := h.cfg()

	// verify secret
	if c.SMSSecret != "" && body.Secret != c.SMSSecret {
		log.Printf("sms notify: invalid secret")
		w.WriteHeader(http.StatusForbidden)
		return
	}

	// extract amount from notification text
	matches := moneyRegex.FindStringSubmatch(body.Content)
	if len(matches) < 2 {
		log.Printf("sms notify: no amount found in: %s", body.Content)
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
		return
	}
	amount := matches[1]

	// find matching unpaid order
	order, err := h.store.EpayFindUnpaidByAmount(amount)
	if err != nil {
		log.Printf("sms notify: query error: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if order == nil {
		log.Printf("sms notify: no unpaid order matching amount %s", amount)
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
		return
	}

	// check order not expired
	timeout := time.Duration(c.OrderTimeout) * time.Minute
	if time.Since(order.CreatedAt) > timeout {
		log.Printf("sms notify: order %s expired", order.TradeNo)
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
		return
	}

	// mark as paid
	if err := h.store.EpayUpdatePaid(order.TradeNo, "SMS_"+order.TradeNo); err != nil {
		log.Printf("sms notify: update paid failed: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	log.Printf("sms notify: order %s paid (amount=%s)", order.TradeNo, amount)

	// notify downstream
	go h.notifyDownstream(order)

	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "ok")
}

// Confirm is a manual confirm endpoint for testing.
func (h *Handler) Confirm(w http.ResponseWriter, r *http.Request) {
	tradeNo := r.URL.Query().Get("trade_no")
	if tradeNo == "" {
		h.writeError(w, http.StatusBadRequest, "trade_no required")
		return
	}

	order, err := h.store.EpayGetByTradeNo(tradeNo)
	if err != nil || order == nil {
		h.writeError(w, http.StatusNotFound, "order not found")
		return
	}
	if order.Status == 1 {
		h.writeError(w, http.StatusBadRequest, "order already paid")
		return
	}

	if err := h.store.EpayUpdatePaid(order.TradeNo, "MANUAL"); err != nil {
		h.writeError(w, http.StatusInternalServerError, "update failed")
		return
	}

	go h.notifyDownstream(order)
	h.writeSuccess(w, map[string]any{"trade_no": tradeNo, "status": 1})
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
.form-row{display:flex;gap:12px;margin-bottom:12px;align-items:center}
.form-row label{width:120px;text-align:right;font-size:14px;color:#666;flex-shrink:0}
.form-row input,.form-row select,.form-row textarea{flex:1;padding:6px 10px;border:1px solid #d9d9d9;border-radius:4px;font-size:14px}
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
<a href="/epay/admin?q=setting">配置</a>
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
<tr><td>今日订单</td><td>{{.TodayOrders}}</td></tr>
<tr><td>订单超时</td><td>{{.OrderTimeout}} 分钟</td></tr>
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
<label>回调地址</label>
<input type="text" readonly value="{{.BaseURL}}/sms/notify" style="background:#f5f5f5" />
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

	data := map[string]any{
		"Tab":             tab,
		"PrimaryMerchant": primaryMerchant,
		"OrderTimeout":    c.OrderTimeout,
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
