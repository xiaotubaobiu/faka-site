package web

import (
	"net/http"
	"strconv"

	"faka-site/internal/auth"
	"faka-site/internal/newapi"
	"faka-site/internal/service"
)

func currentUser(r *http.Request) *auth.Session {
	s, _ := r.Context().Value(ctxSessionKey).(*auth.Session)
	return s
}

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, "dashboard.html", ViewData{Title: "首页"})
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
		d["error"] = "购买失败:" + err.Error()
	}
	s.render(w, r, "buy.html", ViewData{Title: "购买", Data: d})
}

func (s *Server) orders(w http.ResponseWriter, r *http.Request) {
	list, _ := s.store.OrdersByUser(r.Context(), currentUser(r).UserID)
	s.render(w, r, "orders.html", ViewData{Title: "订单", Data: map[string]any{"orders": list}})
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
