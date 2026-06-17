package web

import (
	"embed"
	"html/template"
	"net/http"
	"strings"

	"faka-site/internal/auth"
)

//go:embed templates/*.html static/app.css static/htmx.min.js
var assets embed.FS

type ViewUser struct {
	Email      string
	Role       string
	BalanceFmt string
}

type ViewData struct {
	Title string
	User  *ViewUser
	CSRF  string
	Nonce string
	Data  map[string]any
}

var pages map[string]*template.Template

var pageNames = []string{
	"login.html", "forgot.html", "dashboard.html", "buy.html", "orders.html", "order.html",
	"admin_users.html", "admin_create.html", "admin_reset.html", "admin_balance.html", "admin_config.html",
}

// pagePartials:某页面需要额外引入的共享块文件(与 layout 一起 parse)。
var pagePartials = map[string][]string{
	"orders.html":    {"orders_list.html"},
	"dashboard.html": {"orders_list.html"},
}

func initTemplates() {
	pages = map[string]*template.Template{}
	for _, name := range pageNames {
		base := template.Must(template.New("base").Funcs(template.FuncMap{
			"usd":       usd,
			"joinCodes": joinCodes,
		}).ParseFS(assets, "templates/layout.html"))
		files := []string{"templates/" + name}
		for _, p := range pagePartials[name] {
			files = append(files, "templates/"+p)
		}
		t := template.Must(base.ParseFS(assets, files...))
		pages[name] = t
	}
}

// joinCodes 把码切片按换行拼接,供「复制全部」。
func joinCodes(ss []string) string { return strings.Join(ss, "\n") }

// renderBlock 仅渲染指定块(无 layout),用于 HTMX 片段。
func (s *Server) renderBlock(w http.ResponseWriter, page, block string, data ViewData) {
	if pages == nil {
		initTemplates()
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if data.Data == nil {
		data.Data = map[string]any{}
	}
	t, ok := pages[page]
	if !ok {
		http.Error(w, "unknown page", http.StatusInternalServerError)
		return
	}
	if err := t.ExecuteTemplate(w, block, data); err != nil {
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, page string, data ViewData) {
	if pages == nil {
		initTemplates()
	}
	if data.Data == nil {
		data.Data = map[string]any{}
	}
	if sess, ok := r.Context().Value(ctxSessionKey).(*auth.Session); ok && sess != nil {
		if u, err := s.store.UserByID(sess.UserID); err == nil {
			data.User = &ViewUser{Email: u.Email, Role: u.Role, BalanceFmt: usd(u.Balance)}
		}
		data.CSRF = sess.CSRF
	}
	data.Nonce = nonceFromContext(r)
	if data.Title == "" {
		data.Title = "发卡站"
	}
	t, ok := pages[page]
	if !ok {
		http.Error(w, "unknown page", http.StatusInternalServerError)
		return
	}
	if err := t.ExecuteTemplate(w, "layout", data); err != nil {
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}
