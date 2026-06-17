# 发卡站 前端重构(Tailwind + daisyUI + HTMX)+ 订单码展示 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把前端从 Pico+手写 CSS 切到 Tailwind+daisyUI+HTMX(简洁现代、手机/桌面自适应),并在历史订单直接展示兑换码 + 复制按钮。

**Architecture:** Go stdlib `html/template` 不变;新增 Node 构建步骤生成 `app.css`(`//go:embed`,提交进仓);HTMX 自托管用于订单搜索局部刷新;码用批量查询 `CodesForOrders` 注入订单列表;复制/主题走 nonce 脚本事件委托。

**Tech Stack:** Go 1.25、`html/template`、Tailwind CSS v3 + daisyUI v4、HTMX v2、`modernc.org/sqlite`、`//go:embed`、Makefile + 多阶段 Dockerfile。

**Spec:** `docs/specs/2026-06-17-faka-tailwind-htmx-design.md`

---

## File Map

| 文件 | 责任 | 动作 |
|---|---|---|
| `package.json`、`tailwind.config.js`、`src/input.css`、`Makefile` | 构建 toolchain | 新增 |
| `internal/web/static/app.css` | Tailwind+daisyUI 产物(提交) | 新增(生成) |
| `internal/web/static/htmx.min.js` | HTMX(提交) | 新增(下载) |
| `internal/web/static/pico.min.css`、`style.css` | 旧样式 | 删除 |
| `internal/web/render.go` | embed 切换 + `joinCodes` FuncMap + `renderBlock` + pagePartials | 改 |
| `internal/web/user.go` | orders handler:HTMX 片段 + codes + q | 改 |
| `internal/store/orders.go` | `CodesForOrders`、`OrdersByUserFiltered` | 改 |
| `internal/web/templates/layout.html` | daisyUI shell(navbar/drawer/主题/复制脚本) | 重写 |
| `internal/web/templates/orders_list.html` | 订单卡片 partial(码+复制) | 新增 |
| `internal/web/templates/orders.html`、`dashboard.html`、`buy.html`、`order.html`、`login.html`、`forgot.html`、`admin_*.html` | daisyUI 重写 | 重写 |
| `Dockerfile` | 多阶段(node→go→alpine) | 重写 |
| `.gitignore` | `node_modules/` | 改 |

测试:`internal/store/orders_codes_test.go`(新增)。

---

## Task 1: 构建工具链与静态资源

**Files:** Create `package.json`、`tailwind.config.js`、`src/input.css`、`Makefile`;modify `.gitignore`;generate `internal/web/static/app.css`;download `internal/web/static/htmx.min.js`.

- [ ] **Step 1: 新建 `package.json`**

```json
{
  "name": "faka-site-assets",
  "private": true,
  "scripts": { "css": "tailwindcss -i src/input.css -o internal/web/static/app.css --minify" },
  "devDependencies": {
    "tailwindcss": "^3.4.17",
    "daisyui": "^4.12.23"
  }
}
```

- [ ] **Step 2: 新建 `tailwind.config.js`**

```js
/** @type {import('tailwindcss').Config} */
module.exports = {
  content: ["./internal/web/templates/**/*.html", "./internal/web/**/*.go"],
  theme: { extend: {} },
  plugins: [require("daisyui")],
  daisyui: { themes: ["emerald", "dark"], darkTheme: "dark" },
};
```

- [ ] **Step 3: 新建 `src/input.css`**

```css
@tailwind base;
@tailwind components;
@tailwind utilities;
```

- [ ] **Step 4: 新建 `Makefile`**

```make
.PHONY: css build run dev clean
css:
	npx tailwindcss -i src/input.css -o internal/web/static/app.css --minify
build: css
	go build -o faka-site .
run: build
	FAKA_DB=./data/faka.db FAKA_LISTEN=:8090 SESSION_SECRET=devsecret COOKIE_SECURE=false ./faka-site
dev: css
	go run .
clean:
	rm -f faka-site
```

- [ ] **Step 5: `.gitignore` 增 `node_modules/`**

在 `.gitignore` 末尾追加一行:
```
/node_modules
```

- [ ] **Step 6: 安装依赖 + 生成 app.css**

Run:
```bash
cd /home/lisa/matrix/faka-site
npm install
npx tailwindcss -i src/input.css -o internal/web/static/app.css --minify
```
Expected:`internal/web/static/app.css` 生成(几十 KB),无报错。验证含 daisyUI:
```bash
head -c 200 internal/web/static/app.css
grep -c "btn\|card\|navbar" internal/web/static/app.css
```

- [ ] **Step 7: 下载 HTMX**

```bash
curl -fsSL -o internal/web/static/htmx.min.js https://cdn.jsdelivr.net/npm/htmx.org@2.0.4/dist/htmx.min.js
wc -c internal/web/static/htmx.min.js   # 应约 49KB(±)
```

- [ ] **Step 8: `go build ./...` 仍通过(embed 还没改,引用的是旧 pico/style)**

Run: `go build ./... && go test ./...` — Expected:全 PASS(app.css/htmx 尚未被引用,不影响)。

- [ ] **Step 9: 提交**

```bash
git add package.json package-lock.json tailwind.config.js src/input.css Makefile .gitignore internal/web/static/app.css internal/web/static/htmx.min.js
git commit -m "build: 引入 Tailwind+daisyUI 构建链与 HTMX 静态资源"
```

---

## Task 2: store 批量取码 + 订单过滤(TDD)

**Files:** Modify `internal/store/orders.go`(append 2 methods,加 `"strings"` import);Test: create `internal/store/orders_codes_test.go`.

- [ ] **Step 1: 写失败测试**

新建 `internal/store/orders_codes_test.go`:
```go
package store

import (
	"context"
	"testing"
)

func TestCodesForOrders_Batch(t *testing.T) {
	s, _ := OpenInMemory()
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	// order_codes 无 FK,直接插
	_, _ = s.db.Exec(`INSERT INTO order_codes(order_id,user_id,code,quota,created_at) VALUES(1,1,'AAA',500000,0)`)
	_, _ = s.db.Exec(`INSERT INTO order_codes(order_id,user_id,code,quota,created_at) VALUES(1,1,'BBB',500000,0)`)
	_, _ = s.db.Exec(`INSERT INTO order_codes(order_id,user_id,code,quota,created_at) VALUES(2,1,'CCC',500000,0)`)

	m, err := s.CodesForOrders(ctx, []int64{1, 2, 99})
	if err != nil {
		t.Fatal(err)
	}
	if len(m[1]) != 2 || m[1][0] != "AAA" || m[1][1] != "BBB" {
		t.Fatalf("order 1 codes = %v", m[1])
	}
	if len(m[2]) != 1 || m[2][0] != "CCC" {
		t.Fatalf("order 2 codes = %v", m[2])
	}
	if _, ok := m[99]; ok {
		t.Fatal("order 99 should be absent")
	}

	empty, err := s.CodesForOrders(ctx, nil)
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty case: %v %v", empty, err)
	}
}

func TestOrdersByUserFiltered_Q(t *testing.T) {
	s, _ := OpenInMemory()
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	mk := func(total int64, ts int64, st string) {
		_, _ = s.db.Exec(`INSERT INTO orders(user_id,code_count,quota_per_code,total_cost,status,created_at,updated_at) VALUES(1,1,500000,?, ?, ?)`, total, ts, st, ts)
	}
	mk(500000, 1, "completed")
	mk(1000000, 2, "partial")

	all, _ := s.OrdersByUserFiltered(ctx, 1, "")
	if len(all) != 2 {
		t.Fatalf("empty q -> want 2, got %d", len(all))
	}
	byStatus, _ := s.OrdersByUserFiltered(ctx, 1, "partial")
	if len(byStatus) != 1 || byStatus[0].Status != "partial" {
		t.Fatalf("q=partial -> %v", byStatus)
	}
	byID, _ := s.OrdersByUserFiltered(ctx, 1, "2")
	if len(byID) != 1 {
		t.Fatalf("q=2 -> %d", len(byID))
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/store/ -run 'TestCodesForOrders|TestOrdersByUserFiltered' -v`
Expected: FAIL(`undefined: CodesForOrders`)。

- [ ] **Step 3: 实现**

`internal/store/orders.go` 顶部 import 当前是 `import "context"`(单行)。改为:
```go
import (
	"context"
	"strings"
)
```
末尾追加:
```go
// CodesForOrders 批量取多订单的码,返回 orderID->codes(按 order_id,id 顺序)。空入参返回空 map。
func (s *Store) CodesForOrders(ctx context.Context, orderIDs []int64) (map[int64][]string, error) {
	out := map[int64][]string{}
	if len(orderIDs) == 0 {
		return out, nil
	}
	placeholders := make([]string, len(orderIDs))
	args := make([]any, 0, len(orderIDs))
	for i, id := range orderIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}
	q := "SELECT order_id, code FROM order_codes WHERE order_id IN (" + strings.Join(placeholders, ",") + ") ORDER BY order_id, id"
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var oid int64
		var code string
		if err := rows.Scan(&oid, &code); err != nil {
			return nil, err
		}
		out[oid] = append(out[oid], code)
	}
	return out, rows.Err()
}

// OrdersByUserFiltered 返回某用户的订单;q 为空等价 OrdersByUser;q 非空按 id(文本)/status 模糊匹配。
func (s *Store) OrdersByUserFiltered(ctx context.Context, userID int64, q string) ([]Order, error) {
	if q == "" {
		return s.OrdersByUser(ctx, userID)
	}
	like := "%" + q + "%"
	rows, err := s.db.QueryContext(ctx,
		`SELECT id,user_id,code_count,quota_per_code,total_cost,status,succeeded_count,failed_count,refunded_amount
		 FROM orders WHERE user_id=? AND (CAST(id AS TEXT) LIKE ? OR status LIKE ?) ORDER BY id DESC`,
		userID, like, like)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Order
	for rows.Next() {
		var o Order
		if err := rows.Scan(&o.ID, &o.UserID, &o.CodeCount, &o.QuotaPerCode, &o.TotalCost, &o.Status, &o.SucceededCount, &o.FailedCount, &o.RefundedAmount); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/store/ -run 'TestCodesForOrders|TestOrdersByUserFiltered' -v`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add internal/store/orders.go internal/store/orders_codes_test.go
git commit -m "feat(store): 批量取码 CodesForOrders + 订单过滤 OrdersByUserFiltered"
```

---

## Task 3: render 接线 + orders handler + orders_list 片段

**Files:** Modify `internal/web/render.go`; Modify `internal/web/user.go`; Create `internal/web/templates/orders_list.html`.

- [ ] **Step 1: render.go —— embed 暂不改,加 FuncMap/pagePartials/renderBlock**

当前 `render.go` 关键片段:
```go
//go:embed templates/*.html static/style.css static/pico.min.css
var assets embed.FS
```
**本任务暂不动 embed**(Task 4 再切到 app.css/htmx,避免中间态 404)。

把 `initTemplates` 当前实现:
```go
func initTemplates() {
	pages = map[string]*template.Template{}
	for _, name := range pageNames {
		base := template.Must(template.New("base").Funcs(template.FuncMap{"usd": usd}).ParseFS(assets, "templates/layout.html"))
		t := template.Must(base.ParseFS(assets, "templates/"+name))
		pages[name] = t
	}
}
```
替换为(加 pagePartials 支持,让 orders.html 能引入 orders_list 块):
```go
// pagePartials:某页面需要额外引入的共享块文件(同 layout 一起 parse)。
var pagePartials = map[string][]string{
	"orders.html": {"orders_list.html"},
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
```
render.go import 增 `"strings"`。最终 import:
```go
import (
	"embed"
	"html/template"
	"net/http"
	"strings"

	"faka-site/internal/auth"
)
```

- [ ] **Step 2: 注册 orders_list.html**

`pageNames` 当前(含此前加的 forgot/admin_reset):
```go
var pageNames = []string{
	"login.html", "forgot.html", "dashboard.html", "buy.html", "orders.html", "order.html",
	"admin_users.html", "admin_create.html", "admin_reset.html", "admin_balance.html", "admin_config.html",
}
```
`orders_list.html` **不**加入 pageNames(它不是独立整页,仅作为 orders.html 的 partial 经 pagePartials 引入;HTMX 片段通过 renderBlock 渲染 orders.html 集合里的 "orders_list" 块)。无需改动 pageNames。

- [ ] **Step 3: 创建 `internal/web/templates/orders_list.html`**

```html
{{define "orders_list"}}
{{if .Data.orders}}
<div class="grid gap-3">
  {{range .Data.orders}}
  {{$codes := index $.Data.codes .ID}}
  <div class="card bg-base-100 shadow border border-base-200">
    <div class="card-body p-4 gap-2">
      <div class="flex flex-wrap items-center gap-2 justify-between">
        <a href="/orders/{{.ID}}" class="font-semibold link link-hover">#{{.ID}}</a>
        <span class="text-sm opacity-70">{{.CodeCount}} 个 · 每 {{usd .QuotaPerCode}}</span>
        <span class="badge badge-sm {{if eq .Status "completed"}}badge-success{{else if eq .Status "partial"}}badge-warning{{else}}badge-error{{end}}">{{.Status}}</span>
      </div>
      {{if $codes}}
      <div class="flex flex-wrap items-center gap-1.5">
        {{range $i, $c := $codes}}{{if lt $i 3}}<span class="kbd kbd-sm cursor-pointer" data-copy="{{$c}}">{{$c}}</span>{{end}}{{end}}
        {{if gt (len $codes) 3}}
        <details class="inline">
          <summary class="btn btn-ghost btn-xs">展开全部({{len $codes}})</summary>
          <div class="flex flex-wrap gap-1.5 mt-2">{{range $c := $codes}}<span class="kbd kbd-sm cursor-pointer" data-copy="{{$c}}">{{$c}}</span>{{end}}</div>
        </details>
        {{end}}
        <button class="btn btn-ghost btn-xs" data-copy-all="{{joinCodes $codes}}">复制全部</button>
      </div>
      {{else}}<span class="text-sm opacity-50">无码</span>{{end}}
    </div>
  </div>
  {{end}}
</div>
{{else}}<p class="opacity-60">暂无订单,<a class="link" href="/buy">去购买</a>。</p>{{end}}
{{end}}
```

- [ ] **Step 4: orders handler 支持 HTMX + codes + q**

`internal/web/user.go` 当前 `orders`:
```go
func (s *Server) orders(w http.ResponseWriter, r *http.Request) {
	list, _ := s.store.OrdersByUser(r.Context(), currentUser(r).UserID)
	s.render(w, r, "orders.html", ViewData{Title: "订单", Data: map[string]any{"orders": list}})
}
```
替换为:
```go
func (s *Server) orders(w http.ResponseWriter, r *http.Request) {
	uid := currentUser(r).UserID
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	list, _ := s.store.OrdersByUserFiltered(r.Context(), uid, q)
	ids := make([]int64, 0, len(list))
	for _, o := range list {
		ids = append(ids, o.ID)
	}
	codes, _ := s.store.CodesForOrders(r.Context(), ids)
	data := ViewData{Title: "订单", Data: map[string]any{"orders": list, "codes": codes}}
	if r.Header.Get("HX-Request") == "true" {
		s.renderBlock(w, "orders.html", "orders_list", data)
		return
	}
	s.render(w, r, "orders.html", data)
}
```
`user.go` import 当前:
```go
import (
	"errors"
	"log"
	"net/http"
	"strconv"

	"faka-site/internal/auth"
	"faka-site/internal/newapi"
	"faka-site/internal/service"
	"faka-site/internal/store"
)
```
增 `"strings"`:
```go
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
```

- [ ] **Step 5: 编译 + 全量测试**

Run: `go build ./... && go test ./...`
Expected:全 PASS(orders_list 块被 orders.html 集合引入,renderBlock 可用)。

- [ ] **Step 6: 提交**

```bash
git add internal/web/render.go internal/web/user.go internal/web/templates/orders_list.html
git commit -m "feat(web): orders 支持 HTMX 片段 + 批量注入码 + joinCodes/renderBlock"
```

---

## Task 4: 全模板 daisyUI 重写 + embed 切换 + 删旧 CSS

**Files:** Modify `internal/web/render.go`(embed);Rewrite `layout.html`、`dashboard.html`、`buy.html`、`orders.html`、`order.html`、`login.html`、`forgot.html`、`admin_users.html`、`admin_create.html`、`admin_reset.html`、`admin_balance.html`、`admin_config.html`;Delete `pico.min.css`、`style.css`。

> 这是最大的任务:把整套模板从 Pico 切到 daisyUI。先切 embed + 写 layout(确立外壳),再逐页重写。所有页保持现有 handler 数据契约(`.Data.*` 字段不变)。

- [ ] **Step 1: render.go 切换 embed**

把:
```go
//go:embed templates/*.html static/style.css static/pico.min.css
var assets embed.FS
```
改为:
```go
//go:embed templates/*.html static/app.css static/htmx.min.js
var assets embed.FS
```

- [ ] **Step 2: 重写 `layout.html`**

```html
{{define "layout"}}<!doctype html>
<html lang="zh" data-theme="emerald"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}}</title>
<link rel="stylesheet" href="/static/app.css">
<script src="/static/htmx.min.js" defer></script>
</head>
<body class="bg-base-200 min-h-screen">
<div class="drawer lg:drawer-open">
  <input id="nav-drawer" type="checkbox" class="drawer-toggle">

  <div class="drawer-content flex flex-col min-h-screen">
    <div class="navbar bg-base-100 border-b border-base-200 sticky top-0 z-10">
      <label for="nav-drawer" class="btn btn-ghost btn-square lg:hidden">☰</label>
      <a class="btn btn-ghost text-xl" href="/">🎫 发卡站</a>
      <div class="menu menu-horizontal hidden lg:flex ml-2">
        {{if .User}}
        <a href="/" class="{{if eq .Title "概览"}}menu-active{{end}}">概览</a>
        <a href="/buy" class="{{if eq .Title "购买"}}menu-active{{end}}">买码</a>
        <a href="/orders" class="{{if eq .Title "订单"}}menu-active{{end}}">订单</a>
        {{if eq .User.Role "admin"}}
        <a href="/admin/users" class="{{if eq .Title "用户管理"}}menu-active{{end}}">用户</a>
        <a href="/admin/create" class="{{if eq .Title "建账户"}}menu-active{{end}}">建账户</a>
        <a href="/admin/config" class="{{if eq .Title "配置"}}menu-active{{end}}">配置</a>
        {{end}}
        {{end}}
      </div>
      <div class="ml-auto flex items-center gap-2">
        {{if .User}}<span class="badge badge-outline">余额 {{.User.BalanceFmt}}</span>{{end}}
        <button class="btn btn-ghost btn-circle btn-sm" data-theme-toggle aria-label="切换主题">🌓</button>
      </div>
    </div>

    <main class="container mx-auto px-4 py-6 w-full max-w-3xl">{{template "content" .}}</main>
  </div>

  {{if .User}}<div class="drawer-side z-20">
    <label for="nav-drawer" class="drawer-overlay"></label>
    <aside class="bg-base-100 w-64 min-h-full p-4 flex flex-col">
      <div class="font-bold text-lg mb-4">🎫 发卡站</div>
      <nav class="menu menu-vertical gap-1">
        <a href="/" class="{{if eq .Title "概览"}}menu-active{{end}}">概览</a>
        <a href="/buy" class="{{if eq .Title "购买"}}menu-active{{end}}">买码</a>
        <a href="/orders" class="{{if eq .Title "订单"}}menu-active{{end}}">订单</a>
        {{if eq .User.Role "admin"}}
        <div class="menu-title opacity-60 mt-2">管理</div>
        <a href="/admin/users" class="{{if eq .Title "用户管理"}}menu-active{{end}}">用户</a>
        <a href="/admin/create" class="{{if eq .Title "建账户"}}menu-active{{end}}">建账户</a>
        <a href="/admin/config" class="{{if eq .Title "配置"}}menu-active{{end}}">配置</a>
        {{end}}
      </nav>
      <div class="mt-auto pt-4 border-t border-base-200 text-sm">
        <div class="opacity-70 break-all">{{.User.Email}}</div>
        <div class="text-emerald-500 font-bold my-1">余额 {{.User.BalanceFmt}}</div>
        <span class="badge badge-ghost badge-sm">{{if eq .User.Role "admin"}}管理员{{else}}普通用户{{end}}</span>
        <a href="/logout" class="btn btn-ghost btn-xs btn-block mt-2">退出</a>
      </div>
    </aside>
  </div>{{end}}
</div>

<script nonce="{{.Nonce}}">
(function(){
  var d = document.documentElement;
  var saved = localStorage.getItem('theme');
  if (saved) d.setAttribute('data-theme', saved);
  document.querySelectorAll('[data-theme-toggle]').forEach(function(el){
    el.addEventListener('click', function(){
      var nt = d.getAttribute('data-theme') === 'dark' ? 'emerald' : 'dark';
      d.setAttribute('data-theme', nt); localStorage.setItem('theme', nt);
    });
  });
  function flash(el){ var o = el.textContent; el.textContent = '已复制 ✓'; setTimeout(function(){ el.textContent = o; }, 900); }
  document.body.addEventListener('click', function(e){
    var c = e.target.closest('[data-copy]'); if (c) { navigator.clipboard.writeText(c.getAttribute('data-copy')); flash(c); return; }
    var a = e.target.closest('[data-copy-all]'); if (a) { navigator.clipboard.writeText(a.getAttribute('data-copy-all')); flash(a); return; }
  });
  // 移动端点导航后关抽屉
  document.querySelectorAll('.drawer-side a').forEach(function(a){ a.addEventListener('click', function(){ var cb = document.getElementById('nav-drawer'); if (cb) cb.checked = false; }); });
})();
</script>
</body></html>{{end}}
```
(注意:CSP `script-src 'self' 'nonce-{{.Nonce}}'` 已覆盖此内联脚本;`/static/htmx.min.js` 同源放行;drawer 用 daisyUI checkbox 纯 CSS 开合,无需 JS。)

- [ ] **Step 3: 重写 `dashboard.html`**

```html
{{define "content"}}
<div class="stats stats-vertical sm:stats-horizontal shadow w-full mb-4">
  <div class="stat"><div class="stat-title">当前余额</div><div class="stat-value text-emerald-500">{{usd .Data.Balance}}</div></div>
  <div class="stat"><div class="stat-title">总订单</div><div class="stat-value">{{.Data.OrderCount}}</div></div>
  <div class="stat"><div class="stat-title">近 30 天消费</div><div class="stat-value">{{usd .Data.MonthlyUsed}}</div></div>
  {{if .Data.UserCount}}<div class="stat"><div class="stat-title">启用用户</div><div class="stat-value">{{.Data.UserCount}}</div></div>
  <div class="stat"><div class="stat-title">平台总余额</div><div class="stat-value">{{usd .Data.PlatformBalance}}</div></div>{{end}}
</div>
<a href="/buy" class="btn btn-primary btn-block sm:btn-wide mb-4">＋ 购买兑换码</a>
<h3 class="text-lg font-semibold mb-2">最近订单</h3>
{{template "orders_list" .}}
{{end}}
```
(注意:dashboard handler 传的是 `.Data.orders`/`.Data.codes`,但最近订单需走 orders_list 块(要 codes)。**为此 dashboard handler 也要注入 codes**:见 Step 8。)

- [ ] **Step 4: 重写 `buy.html`**

```html
{{define "content"}}
<h2 class="text-xl font-semibold mb-3">购买兑换码</h2>
{{if .Data.error}}<div class="alert alert-error mb-3"><span>{{.Data.error}}</span></div>{{end}}
{{if .Data.result}}
<div class="alert alert-success mb-3">
  <span>订单 #{{.Data.result.OrderID}}:{{.Data.result.Status}},生成 {{.Data.result.Succeeded}} 个{{if gt .Data.result.Failed 0}},失败 {{.Data.result.Failed}} 已退款 {{usd .Data.result.Refunded}}{{end}}</span>
</div>
{{if .Data.result.Codes}}
<div class="card bg-base-100 shadow border border-base-200 mb-4"><div class="card-body">
  <div class="flex flex-wrap gap-1.5">{{range .Data.result.Codes}}<span class="kbd kbd-sm cursor-pointer" data-copy="{{.}}">{{.}}</span>{{end}}</div>
  <button class="btn btn-ghost btn-sm mt-2" data-copy-all="{{joinCodes .Data.result.Codes}}">复制全部</button>
</div></div>{{end}}
{{end}}
<form method="post" action="/buy" class="card bg-base-100 shadow border border-base-200 p-4 grid gap-3 max-w-md">
  <input name="csrf" type="hidden" value="{{.CSRF}}">
  <label class="form-control"><span class="label-text mb-1">数量</span><input name="count" type="number" min="1" value="1" class="input input-bordered"></label>
  <label class="form-control"><span class="label-text mb-1">每码金额($)</span><input name="quota" type="number" min="0.01" step="0.01" value="1" class="input input-bordered"></label>
  <p class="text-xs opacity-60">合计 = 数量 × 每码金额($)(1$ = 500000 额度)</p>
  <button class="btn btn-primary">购买</button>
</form>
{{end}}
```

- [ ] **Step 5: 重写 `orders.html`(含搜索框 + 引用 partial)**

```html
{{define "content"}}
<h2 class="text-xl font-semibold mb-3">订单</h2>
<input name="q" class="input input-bordered input-sm mb-3 w-full max-w-xs"
       placeholder="搜索订单号 / 状态…"
       value="{{.Data.q}}"
       hx-get="/orders" hx-target="#order-list"
       hx-trigger="input changed delay:300ms" hx-include="this">
<div id="order-list">{{template "orders_list" .}}</div>
{{end}}
```
(orders handler 需把 q 也放入 `.Data.q` 以回填搜索框——见 Step 9。)

- [ ] **Step 6: 重写 `order.html`**

```html
{{define "content"}}
<h2 class="text-xl font-semibold mb-1">订单 #{{.Data.order.ID}}</h2>
<p class="opacity-70 mb-3">{{.Data.order.Status}} · 每 {{usd .Data.order.QuotaPerCode}} · 生成 {{.Data.order.SucceededCount}}{{if gt .Data.order.RefundedAmount 0}} · 退款 {{usd .Data.order.RefundedAmount}}{{end}}</p>
{{if .Data.codes}}
<div class="card bg-base-100 shadow border border-base-200"><div class="card-body">
  <div class="flex flex-wrap gap-1.5">{{range .Data.codes}}<span class="kbd kbd-sm cursor-pointer" data-copy="{{.}}">{{.}}</span>{{end}}</div>
  <button class="btn btn-ghost btn-sm mt-2" data-copy-all="{{joinCodes .Data.codes}}">复制全部</button>
</div></div>
{{else}}<p class="opacity-60">无码</p>{{end}}
{{end}}
```

- [ ] **Step 7: 重写 `login.html`、`forgot.html`**

`login.html`:
```html
{{define "content"}}
<div class="max-w-sm mx-auto">
<h2 class="text-xl font-semibold mb-3">登录</h2>
{{if .Data.error}}<div class="alert alert-error mb-3"><span>{{.Data.error}}</span></div>{{end}}
<form method="post" action="/login" class="grid gap-3">
  <input name="email" placeholder="邮箱" required class="input input-bordered">
  <input name="password" type="password" placeholder="密码" required class="input input-bordered">
  <button class="btn btn-primary">登录</button>
</form>
<p class="mt-3 text-sm"><a class="link" href="/forgot">忘记密码?</a></p>
</div>
{{end}}
```
`forgot.html`:
```html
{{define "content"}}
<div class="max-w-sm mx-auto">
<h2 class="text-xl font-semibold mb-3">忘记密码</h2>
{{if .Data.error}}<div class="alert alert-error mb-3"><span>{{.Data.error}}</span></div>{{end}}
{{if .Data.msg}}<div class="alert alert-info mb-3"><span>{{.Data.msg}}</span></div>{{end}}
<form method="post" action="/forgot" class="grid gap-3">
  <input name="email" placeholder="注册邮箱" required class="input input-bordered">
  <button class="btn btn-primary">发送重置邮件</button>
</form>
<p class="mt-3 text-sm"><a class="link" href="/login">返回登录</a></p>
</div>
{{end}}
```

- [ ] **Step 8: dashboard handler 注入 codes(供最近订单卡片显示码)**

`internal/web/user.go` 的 `dashboard`,在 `recent, _ := s.store.RecentOrdersByUser(...)` 后、构造 `d` 前,加批量取码:
```go
	recentIDs := make([]int64, 0, len(recent))
	for _, o := range recent {
		recentIDs = append(recentIDs, o.ID)
	}
	recentCodes, _ := s.store.CodesForOrders(ctx, recentIDs)
```
并在 `d := map[string]any{...}` 里增 `"codes": recentCodes,`。最终 `d`:
```go
	d := map[string]any{
		"Balance":      balance,
		"OrderCount":   orderCount,
		"MonthlyUsed":  monthlyUsed,
		"RecentOrders": recent,
		"codes":        recentCodes,
		"orders":       recent, // orders_list 块读 .Data.orders/.Data.codes
	}
```
(orders_list 块用 `.Data.orders`/`.Data.codes`,所以 dashboard 传 `orders`=recent + `codes`。)

- [ ] **Step 9: orders handler 回填 q**

`orders` handler 的 `data` 增 `"q": q`:
```go
	data := ViewData{Title: "订单", Data: map[string]any{"orders": list, "codes": codes, "q": q}}
```

- [ ] **Step 10: 重写 admin 页面(daisyUI 表单/表格)**

`admin_users.html`:
```html
{{define "content"}}
<h2 class="text-xl font-semibold mb-3">用户管理</h2>
<a href="/admin/create" class="btn btn-primary btn-sm mb-3">建账户</a>
<div class="overflow-x-auto">
<table class="table table-sm">
<tr><th>ID</th><th>邮箱</th><th>角色</th><th>余额</th><th>状态</th><th>操作</th></tr>
{{range .Data.users}}<tr><td>{{.ID}}</td><td class="break-all">{{.Email}}</td><td>{{.Role}}</td><td>{{usd .Balance}}</td><td>{{if eq .Status 1}}<span class="badge badge-success badge-sm">启用</span>{{else}}<span class="badge badge-ghost badge-sm">禁用</span>{{end}}</td>
<td class="whitespace-nowrap">
  <a class="link" href="/admin/balance?id={{.ID}}">加余额</a> ·
  <a class="link" href="/admin/reset?id={{.ID}}">重置密码</a> ·
  <form method="post" action="/admin/users/{{.ID}}/status" class="inline"><input name="csrf" type="hidden" value="{{$.CSRF}}"><input type="hidden" name="status" value="{{if eq .Status 1}}0{{else}}1{{end}}"><button class="link">{{if eq .Status 1}}禁用{{else}}启用{{end}}</button></form>
</td></tr>{{end}}
</table></div>
{{end}}
```
`admin_create.html`:
```html
{{define "content"}}
<h2 class="text-xl font-semibold mb-3">建账户</h2>
{{if .Data.error}}<div class="alert alert-error mb-3"><span>{{.Data.error}}</span></div>{{end}}
<form method="post" action="/admin/create" class="card bg-base-100 shadow border border-base-200 p-4 grid gap-3 max-w-md">
  <input name="csrf" type="hidden" value="{{.CSRF}}">
  <input name="email" placeholder="用户邮箱" value="{{.Data.email}}" required class="input input-bordered">
  <input name="password" type="password" placeholder="密码(≥6位)" required class="input input-bordered">
  <input name="confirm" type="password" placeholder="确认密码" required class="input input-bordered">
  <button class="btn btn-primary">创建</button>
</form>
{{end}}
```
`admin_reset.html`:
```html
{{define "content"}}
<h2 class="text-xl font-semibold mb-3">重置 {{.Data.target.Email}} 的密码</h2>
{{if .Data.error}}<div class="alert alert-error mb-3"><span>{{.Data.error}}</span></div>{{end}}
<form method="post" action="/admin/reset" class="card bg-base-100 shadow border border-base-200 p-4 grid gap-3 max-w-md">
  <input name="csrf" type="hidden" value="{{.CSRF}}">
  <input name="id" type="hidden" value="{{.Data.target.ID}}">
  <input name="password" type="password" placeholder="新密码(≥6位)" required class="input input-bordered">
  <input name="confirm" type="password" placeholder="确认新密码" required class="input input-bordered">
  <button class="btn btn-primary">重置</button>
</form>
{{end}}
```
`admin_balance.html`:
```html
{{define "content"}}
<h2 class="text-xl font-semibold mb-3">给 {{.Data.target.Email}} 加余额</h2>
<form method="post" action="/admin/balance" class="card bg-base-100 shadow border border-base-200 p-4 grid gap-3 max-w-md">
  <input name="csrf" type="hidden" value="{{.CSRF}}">
  <input name="id" type="hidden" value="{{.Data.target.ID}}">
  <input name="amount" type="number" min="0.01" step="0.01" placeholder="金额($)" required class="input input-bordered">
  <button class="btn btn-primary">加余额</button>
</form>
{{end}}
```
`admin_config.html`:
```html
{{define "content"}}
<h2 class="text-xl font-semibold mb-3">站点配置</h2>
{{if .Data.msg}}<div class="alert alert-success mb-3"><span>{{.Data.msg}}</span></div>{{end}}
{{if .Data.testErr}}<div class="alert alert-error mb-3"><span>{{.Data.testErr}}</span></div>{{end}}
{{if .Data.testOK}}<div class="alert alert-success mb-3"><span>连接正常,且为管理员 ✓</span></div>{{end}}
<form method="post" action="/admin/config" class="card bg-base-100 shadow border border-base-200 p-4 grid gap-3">
  <input name="csrf" type="hidden" value="{{.CSRF}}">
  <fieldset class="border border-base-200 rounded p-3 grid gap-2">
    <legend class="text-sm opacity-70 px-1">NewAPI</legend>
    <label class="form-control"><span class="label-text">网址</span><input name="newapi_base_url" value="{{.Data.cfg.BaseURL}}" class="input input-bordered input-sm"></label>
    <label class="form-control"><span class="label-text">系统访问令牌</span><input name="newapi_access_token" placeholder="留空=不修改" class="input input-bordered input-sm"></label>
    <label class="form-control"><span class="label-text">管理员用户ID</span><input name="newapi_admin_user_id" value="{{.Data.cfg.AdminUserID}}" class="input input-bordered input-sm"></label>
  </fieldset>
  <fieldset class="border border-base-200 rounded p-3 grid gap-2">
    <legend class="text-sm opacity-70 px-1">SMTP</legend>
    <label class="form-control"><span class="label-text">主机</span><input name="smtp_host" value="{{.Data.cfg.SMTPHost}}" class="input input-bordered input-sm"></label>
    <label class="form-control"><span class="label-text">端口</span><input name="smtp_port" value="{{.Data.cfg.SMTPPort}}" class="input input-bordered input-sm"></label>
    <label class="form-control"><span class="label-text">用户</span><input name="smtp_user" value="{{.Data.cfg.SMTPUser}}" class="input input-bordered input-sm"></label>
    <label class="form-control"><span class="label-text">密码</span><input name="smtp_pass" type="password" placeholder="留空=不修改" class="input input-bordered input-sm"></label>
    <label class="form-control"><span class="label-text">发件人</span><input name="smtp_from" value="{{.Data.cfg.SMTPFrom}}" class="input input-bordered input-sm"></label>
  </fieldset>
  <button class="btn btn-primary">保存</button>
</form>
<form method="post" action="/admin/config/test" class="mt-3"><input name="csrf" type="hidden" value="{{.CSRF}}"><button class="btn btn-ghost">测试 NewAPI 连接</button></form>
{{end}}
```

- [ ] **Step 11: 删旧 CSS 文件**

```bash
rm internal/web/static/pico.min.css internal/web/static/style.css
```

- [ ] **Step 12: 重新生成 app.css(扫描新 class)+ 构建 + 测试**

```bash
cd /home/lisa/matrix/faka-site
npx tailwindcss -i src/input.css -o internal/web/static/app.css --minify
go build ./... && go test ./...
```
Expected:全 PASS;`app.css` 体积合理(应包含 navbar/card/kbd/badge/alert/table/drawer 等用到的 class)。

- [ ] **Step 13: 提交**

```bash
git add internal/web/render.go internal/web/static/app.css internal/web/templates/
git rm internal/web/static/pico.min.css internal/web/static/style.css
git commit -m "feat(web): 全模板重写为 daisyUI + embed 切 app.css/htmx + 删旧 CSS"
```

---

## Task 5: Dockerfile 多阶段重构

**Files:** Rewrite `Dockerfile`。

- [ ] **Step 1: 重写 `Dockerfile`**

```dockerfile
# 1) 构建 CSS(node)
FROM node:20-alpine AS css
WORKDIR /src
COPY package.json package-lock.json ./
RUN npm ci
COPY tailwind.config.js ./
COPY src/input.css ./src/input.css
COPY internal/web internal/web
RUN npx tailwindcss -i src/input.css -o internal/web/static/app.css --minify

# 2) 构建 Go(含生成的 app.css)
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=css /src/internal/web/static/app.css ./internal/web/static/app.css
RUN CGO_ENABLED=0 go build -o /out/faka-site .

# 3) 运行(无 node,二进制内嵌静态)
FROM alpine:latest
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=build /out/faka-site /app/faka-site
EXPOSE 8080
VOLUME ["/app/data"]
ENV FAKA_DB=/app/data/data.db FAKA_LISTEN=:8080 COOKIE_SECURE=true
ENTRYPOINT ["/app/faka-site"]
```

- [ ] **Step 2: 验证语法(可选:本地 docker build)**

Run(若环境有 docker):`docker build -t faka-site . 2>&1 | tail -15`
Expected:构建成功,最终镜像无 node(`docker run --rm faka-site ls /app` 仅见 faka-site)。
若环境无 docker,跳过实 build,人工核对 Dockerfile 三个阶段正确即可。

- [ ] **Step 3: 提交**

```bash
git add Dockerfile
git commit -m "build: Dockerfile 多阶段(node 编 CSS → go 编二进制 → alpine 运行)"
```

---

## Task 6: 端到端验证

**Files:** 无代码改动(验证)。

- [ ] **Step 1: 临时库冒烟**

```bash
cd /home/lisa/matrix/faka-site
go build -o /tmp/faka-tw . 2>&1
rm -f /tmp/faka-tw.db*
FAKA_DB=/tmp/faka-tw.db FAKA_LISTEN=:8097 SESSION_SECRET=t COOKIE_SECURE=false ADMIN_EMAIL=admin ADMIN_PASSWORD=1 setsid /tmp/faka-tw >/tmp/faka-tw.log 2>&1 < /dev/null & disown
sleep 2
curl -s -o /dev/null -w "login=%{http_code}\n" http://127.0.0.1:8097/login
curl -s http://127.0.0.1:8097/login | grep -oE 'app.css|htmx.min.js' | sort -u
```
Expected:login 200;HTML 引用 app.css + htmx.min.js。

- [ ] **Step 2: 建用户 + 购买 + 历史见码 + 复制属性**

```bash
CJ=$(mktemp)
curl -s -c $CJ -X POST http://127.0.0.1:8097/login -d "email=admin&password=1" -o /dev/null
# (需先在 /admin/config 配 NewAPI 才能真发码;此处只验页面结构)
curl -s -b $CJ http://127.0.0.1:8097/orders | grep -oE 'data-copy|data-copy-all|order-list|hx-get' | sort -u
rm -f $CJ
```
Expected:含 `data-copy`、`data-copy-all`、`order-list`、`hx-get`(HTMX 搜索 + 复制按钮就位)。

- [ ] **Step 3: 安全头 + nonce 仍在**

```bash
curl -s -D - -o /dev/null http://127.0.0.1:8097/login | grep -iE 'content-security|nonce'
```
Expected:CSP 含 nonce。

- [ ] **Step 4: 收尾 + 全量测试 + 推送**

```bash
pkill -f faka-tw 2>/dev/null
go test ./...
git push origin main   # 若在 feature 分支则先合并
```

(实际购买出码需 NewAPI 已配;真实库 `data/faka.db` 已有 2 笔带码订单,可在此库上重启验证历史码展示+复制。)

---

## Self-Review 结果

**Spec 覆盖:** §3 构建链→Task 1+5 ✓;§4 布局→Task 4 layout ✓;§5 码展示→Task 2(CodesForOrders)+Task 3(orders_list)+Task 4(各页码块)✓;§6 HTMX→Task 3(handler)+Task 4(orders.html 搜索框)✓;§7 文件清单全覆盖 ✓。

**类型一致性:** `CodesForOrders(ctx,[]int64)->map[int64][]string`、`OrdersByUserFiltered(ctx,uid,q)`、`renderBlock(w,page,block,data)`、`joinCodes([]string)->string`、`pagePartials` 在定义与调用处一致;模板 `.Data.orders`/`.Data.codes`/`.Data.q` 与 handler 传入一致;orders_list 块名 `"orders_list"` 在 renderBlock、`{{template "orders_list"}}`、`{{define "orders_list"}}` 三处一致。

**无占位符:** 所有步骤含完整代码/命令。
