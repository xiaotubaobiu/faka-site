package web

import (
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"

	"faka-site/internal/auth"
	"faka-site/internal/newapi"
	"faka-site/internal/service"
	"faka-site/internal/store"
)

func currentUser(r *http.Request) *auth.Session {
	s, _ := r.Context().Value(ctxSessionKey).(*auth.Session)
	return s
}

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sess := currentUser(r)
	uid := sess.UserID
	u, _ := s.store.UserByID(uid)
	var balance int64
	if u != nil {
		balance = u.Balance
	}
	orderCount, _ := s.store.CountOrdersByUser(ctx, uid)
	since := s.now().AddDate(0, 0, -30).Unix()
	monthlyUsed, _ := s.store.SumUsedByUser(ctx, uid, since)
	recent, _ := s.store.RecentOrdersByUser(ctx, uid, 5)

	d := map[string]any{
		"Balance":      balance,
		"OrderCount":   orderCount,
		"MonthlyUsed":  monthlyUsed,
		"RecentOrders": recent,
	}
	if sess.Role == "admin" {
		uc, _ := s.store.UserStats()
		tb, _ := s.store.TotalBalance()
		d["UserCount"] = uc
		d["PlatformBalance"] = tb
	}
	s.render(w, r, "dashboard.html", ViewData{Title: "概览", Data: d})
}

func (s *Server) getBuy(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, "buy.html", ViewData{Title: "购买"})
}

func (s *Server) postBuy(w http.ResponseWriter, r *http.Request) {
	count, _ := strconv.Atoi(r.PostFormValue("count"))
	quota, _ := usdToQuota(r.PostFormValue("quota"))
	cfg := s.mustConfig()
	if cfg.AccessToken == "" {
		s.render(w, r, "buy.html", ViewData{Title: "购买", Data: map[string]any{"error": "管理员尚未配置 NewAPI"}})
		return
	}
	issuer := newapi.New(newapi.Config{BaseURL: cfg.BaseURL, AccessToken: cfg.AccessToken, AdminUserID: cfg.AdminUserID})
	bs := &service.BuyService{Store: s.store, Issuer: issuer}
	res, err := bs.Buy(r.Context(), currentUser(r).UserID, count, quota)
	d := map[string]any{"result": res}
	if err != nil {
		log.Printf("buy failed: userID=%d count=%d quota=%d: %v", currentUser(r).UserID, count, quota, err)
		d["error"] = friendlyBuyError(err)
	}
	s.render(w, r, "buy.html", ViewData{Title: "购买", Data: d})
}

func (s *Server) orders(w http.ResponseWriter, r *http.Request) {
	uid := currentUser(r).UserID
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	list, _ := s.store.OrdersByUserFiltered(r.Context(), uid, q)
	ids := make([]int64, 0, len(list))
	for _, o := range list {
		ids = append(ids, o.ID)
	}
	codes, _ := s.store.CodesForOrders(r.Context(), ids)
	data := ViewData{Title: "订单", Data: map[string]any{"orders": list, "codes": codes, "q": q}}
	if r.Header.Get("HX-Request") == "true" {
		s.renderBlock(w, "orders.html", "orders_list", data)
		return
	}
	s.render(w, r, "orders.html", data)
}

func (s *Server) orderDetail(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	o, err := s.store.Order(r.Context(), id)
	if err != nil || o.UserID != currentUser(r).UserID {
		http.NotFound(w, r)
		return
	}
	codes, _ := s.store.OrderCodes(r.Context(), id)
	s.render(w, r, "order.html", ViewData{Title: "订单详情", Data: map[string]any{"order": o, "codes": codes}})
}

// friendlyBuyError 把内部错误映射成给用户的友好文案,原始错误仅落服务端日志。
func friendlyBuyError(err error) string {
	switch {
	case errors.Is(err, store.ErrInsufficient):
		return "余额不足,请联系管理员充值"
	case errors.Is(err, newapi.ErrCompliance):
		return "服务暂不可用(兑换码功能未开启)"
	case errors.Is(err, newapi.ErrUnauthorized):
		return "服务暂不可用(配置异常)"
	case errors.Is(err, newapi.ErrUpstream):
		return "生成失败,请稍后重试"
	default:
		return "购买失败,请稍后重试"
	}
}
