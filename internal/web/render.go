package web

import (
	"embed"
	"html/template"
	"net/http"

	"faka-site/internal/auth"
)

//go:embed templates/*.html static/style.css static/pico.min.css
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
	Data  map[string]any
}

var pages map[string]*template.Template

var pageNames = []string{
	"login.html", "dashboard.html", "buy.html", "orders.html", "order.html",
	"admin_users.html", "admin_create.html", "admin_balance.html", "admin_config.html",
}

func initTemplates() {
	pages = map[string]*template.Template{}
	for _, name := range pageNames {
		base := template.Must(template.New("base").Funcs(template.FuncMap{"usd": usd}).ParseFS(assets, "templates/layout.html"))
		t := template.Must(base.ParseFS(assets, "templates/"+name))
		pages[name] = t
	}
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, page string, data ViewData) {
	if data.Data == nil {
		data.Data = map[string]any{}
	}
	if sess, ok := r.Context().Value(ctxSessionKey).(*auth.Session); ok && sess != nil {
		if u, err := s.store.UserByID(sess.UserID); err == nil {
			data.User = &ViewUser{Email: u.Email, Role: u.Role, BalanceFmt: usd(u.Balance)}
		}
		data.CSRF = sess.CSRF
	}
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
