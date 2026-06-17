# 发卡站 安全加固 + UI 重构 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 给发卡站补齐 HTTP 安全响应头 + 可信 IP 限流 + 错误脱敏,并把 UI 从"挤在左上角"重构为翠绿主题的侧栏仪表盘。

**Architecture:** 后端纯函数/中间件用 TDD 加固(clientIP、securityHeaders、store 查询、错误映射);前端重写 layout 模板为侧栏 + 抽屉,套用 Pico 变量定制翠绿主题,新增仪表盘概览页。技术栈不变(Go `html/template` + Pico.css + 单二进制),零 JS 框架。

**Tech Stack:** Go 1.25、`html/template`、`modernc.org/sqlite`、Pico.css v2、`//go:embed`。

**Spec:** `docs/specs/2026-06-17-faka-security-ui-design.md`

---

## File Map

| 文件 | 责任 | 动作 |
|---|---|---|
| `internal/web/middleware.go` | 会话/鉴权/CSRF 中间件 + `clientIP` 工具 | 改 |
| `internal/web/headers.go` | 安全响应头 + CSP nonce 中间件 | 新增 |
| `internal/web/render.go` | 模板渲染 + `ViewData`(加 Nonce) | 改 |
| `internal/web/server.go` | Server/路由(挂载 headers、概览路由) | 改 |
| `internal/web/auth.go` | 登录(限流改用 clientIP) | 改 |
| `internal/web/user.go` | dashboard/购买(错误脱敏、概览数据) | 改 |
| `internal/store/orders.go` | 订单 CRUD + 新查询(count/recent/sum) | 改 |
| `internal/store/users.go` | 用户 CRUD + 新查询(stats/total) | 改 |
| `internal/web/templates/layout.html` | 侧栏布局 + 抽屉 + nonce script | 重写 |
| `internal/web/templates/dashboard.html` | 状态卡 + 最近订单 | 重写 |
| `internal/web/static/style.css` | 翠绿主题 + 侧栏/卡片/响应式 | 重写 |

测试文件(新增,与被测文件同目录):`middleware_test.go`、`headers_test.go`、`buyerror_test.go`、`orders_stats_test.go`、`users_stats_test.go`。

---

## Task 1: 可信客户端 IP 工具 `clientIP`

**Files:**
- Modify: `internal/web/middleware.go`(末尾追加)
- Test: `internal/web/middleware_test.go`(新增)

- [ ] **Step 1: 写失败测试**

新建 `internal/web/middleware_test.go`:

```go
package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientIP_IgnoresXFFWhenNotLoopback(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "203.0.113.5:1234"
	req.Header.Set("X-Forwarded-For", "9.9.9.9")
	if got := clientIP(req); got != "203.0.113.5" {
		t.Fatalf("expected direct public IP, got %q", got)
	}
}

func TestClientIP_TrustsXFFWhenLoopback(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "9.9.9.9, 10.0.0.1")
	if got := clientIP(req); got != "9.9.9.9" {
		t.Fatalf("expected first XFF hop, got %q", got)
	}
}

func TestClientIP_FallsBackToRemoteAddr(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:1234" // loopback but no XFF
	if got := clientIP(req); got != "127.0.0.1" {
		t.Fatalf("expected loopback addr, got %q", got)
	}
}

// 防止 import 未使用占位
var _ = http.MethodGet
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/web/ -run TestClientIP -v`
Expected: FAIL — `undefined: clientIP`

- [ ] **Step 3: 实现 `clientIP` + `isLoopback`**

在 `internal/web/middleware.go` 顶部 import 增加 `"net"`、`"strings"`(strings 已在 auth.go 用,但 middleware.go 需自己加)。在文件末尾追加:

```go
// clientIP 返回可信客户端 IP。仅当直连来自回环(经本机 Caddy 反代)时才信任
// X-Forwarded-For 的首段;否则用 RemoteAddr,防止伪造 XFF 绕过限流。
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if isLoopback(host) {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			return strings.TrimSpace(strings.SplitN(xff, ",", 2)[0])
		}
	}
	return host
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
```

`middleware.go` 的 import 块改为:

```go
import (
	"context"
	"net"
	"net/http"
	"strings"

	"faka-site/internal/auth"
)
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/web/ -run TestClientIP -v`
Expected: PASS(3 个用例全过)

- [ ] **Step 5: 提交**

```bash
git add internal/web/middleware.go internal/web/middleware_test.go
git commit -m "feat(web): 可信客户端 IP 工具 clientIP(限流防伪造 XFF)"
```

---

## Task 2: 登录限流改用 `clientIP`

**Files:**
- Modify: `internal/web/auth.go:14-23`(postLogin 的 ip 取值)

- [ ] **Step 1: 改取值逻辑**

把 `internal/web/auth.go` 中 `postLogin` 开头的:

```go
	ip := r.RemoteAddr
	if f := r.Header.Get("X-Forwarded-For"); f != "" {
		ip = strings.SplitN(f, ",", 2)[0]
	}
```

替换为:

```go
	ip := clientIP(r)
```

- [ ] **Step 2: 清理未使用 import**

`auth.go` 顶部 import 中的 `"strings"` 若此后无其他引用会编译失败——检查:该文件仅此处用了 `strings`。改为去掉 `"strings"`:

```go
import (
	"net/http"

	"faka-site/internal/auth"
)
```

- [ ] **Step 3: 编译验证**

Run: `go build ./...`
Expected: 无输出(成功)。若报 `strings imported and not used`,确认 import 已移除。

- [ ] **Step 4: 跑全量测试确认无回归**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/web/auth.go
git commit -m "fix(web): 登录限流改用可信 clientIP"
```

---

## Task 3: 安全响应头 + CSP nonce 中间件

**Files:**
- Create: `internal/web/headers.go`
- Modify: `internal/web/render.go`(ViewData 加 Nonce + render 注入)
- Modify: `internal/web/server.go`(路由最外层挂载)
- Test: `internal/web/headers_test.go`(新增)

- [ ] **Step 1: 写失败测试**

新建 `internal/web/headers_test.go`:

```go
package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSecurityHeaders_SetsAllHeadersAndNonce(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest("GET", "/login", nil)
	rec := httptest.NewRecorder()
	s.securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := nonceFromContext(r)
		if n == "" {
			t.Fatal("nonce missing from request context")
		}
		csp := w.Header().Get("Content-Security-Policy")
		if !strings.Contains(csp, "'nonce-"+n+"'") {
			t.Fatalf("CSP must contain context nonce; got %q", csp)
		}
	})).ServeHTTP(rec, req)

	for _, h := range []string{"Content-Security-Policy", "X-Frame-Options", "X-Content-Type-Options", "Referrer-Policy"} {
		if rec.Header().Get(h) == "" {
			t.Fatalf("missing security header %q", h)
		}
	}
}

func TestSecurityHeaders_NonceUniquePerRequest(t *testing.T) {
	s := &Server{}
	grab := func() string {
		var n string
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/login", nil)
		s.securityHeaders(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			n = nonceFromContext(r)
		})).ServeHTTP(rec, req)
		return n
	}
	a, b := grab(), grab()
	if a == "" || a == b {
		t.Fatalf("nonces must be non-empty and unique: %q %q", a, b)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/web/ -run TestSecurityHeaders -v`
Expected: FAIL — `undefined: securityHeaders` / `nonceFromContext`

- [ ] **Step 3: 新建 `internal/web/headers.go`**

```go
package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
)

type nonceCtxKey struct{}

// withNonce 把 nonce 放入 request context,供 render 注入模板。
func withNonce(r *http.Request, nonce string) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), nonceCtxKey{}, nonce))
}

// nonceFromContext 取出当前请求的 CSP nonce;若无则空串。
func nonceFromContext(r *http.Request) string {
	if v, ok := r.Context().Value(nonceCtxKey{}).(string); ok {
		return v
	}
	return ""
}

// securityHeaders 为每个响应注入安全头;CSP 的 script-src 用 per-request nonce,
// 内联脚本(移动端抽屉开关)经 layout 的 <script nonce> 放行,其余内联脚本被拦。
func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 16)
		if _, err := rand.Read(buf); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		nonce := hex.EncodeToString(buf)
		csp := "default-src 'self'; script-src 'self' 'nonce-" + nonce + "'; " +
			"style-src 'self' 'unsafe-inline'; img-src 'self' data:; frame-ancestors 'none'"
		h := w.Header()
		h.Set("Content-Security-Policy", csp)
		h.Set("X-Frame-Options", "DENY")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, withNonce(r, nonce))
	})
}
```

- [ ] **Step 4: `ViewData` 加 Nonce 字段**

在 `internal/web/render.go` 的 `ViewData` 结构体加 `Nonce string`:

```go
type ViewData struct {
	Title string
	User  *ViewUser
	CSRF  string
	Nonce string
	Data  map[string]any
}
```

- [ ] **Step 5: `render` 注入 nonce**

在 `render` 函数中,`data.CSRF = sess.CSRF` 那段之后,追加(与 session 无关,所有请求都注入):

```go
	data.Nonce = nonceFromContext(r)
```

- [ ] **Step 6: 路由最外层挂载中间件**

`internal/web/server.go` 的 `Routes()` 末尾,把:

```go
	mux.Handle("/admin/", s.loadSession(s.csrfCheck(s.requireAdmin(http.StripPrefix("/admin", adminMux)))))
	return mux
```

改为:

```go
	mux.Handle("/admin/", s.loadSession(s.csrfCheck(s.requireAdmin(http.StripPrefix("/admin", adminMux)))))
	return s.securityHeaders(mux)
```

- [ ] **Step 7: 跑测试确认通过**

Run: `go test ./internal/web/ -run TestSecurityHeaders -v`
Expected: PASS(2 个用例)

- [ ] **Step 8: 编译 + 全量测试**

Run: `go build ./... && go test ./...`
Expected: 全 PASS

- [ ] **Step 9: 提交**

```bash
git add internal/web/headers.go internal/web/headers_test.go internal/web/render.go internal/web/server.go
git commit -m "feat(web): 安全响应头 + CSP per-request nonce 中间件"
```

---

## Task 4: Store 查询 — 订单 count/recent/sum

**Files:**
- Modify: `internal/store/orders.go`(追加 3 个方法)
- Test: `internal/store/orders_stats_test.go`(新增)

- [ ] **Step 1: 写失败测试**

新建 `internal/store/orders_stats_test.go`:

```go
package store

import (
	"context"
	"testing"
)

func TestOrderStats_CountRecentSum(t *testing.T) {
	s, _ := OpenInMemory()
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// orders 表无 FK,可直接插;created_at 用真实秒,便于 sum 过滤。
	mk := func(total int64, ts int64) {
		_, err := s.db.Exec(`INSERT INTO orders(user_id,code_count,quota_per_code,total_cost,status,created_at,updated_at)
			VALUES(1,1,500000,?,'completed',?,?)`, total, ts, ts)
		if err != nil {
			t.Fatal(err)
		}
	}
	mk(500000, 1_700_000_000)
	mk(1000000, 1_700_000_010)
	mk(500000, 1_600_000_000) // 早于 since,不计入 sum

	count, err := s.CountOrdersByUser(ctx, 1)
	if err != nil || count != 3 {
		t.Fatalf("count=%d err=%v want 3", count, err)
	}
	sum, err := s.SumUsedByUser(ctx, 1, 1_650_000_000)
	if err != nil || sum != 1500000 {
		t.Fatalf("sum=%d err=%v want 1500000", sum, err)
	}
	recent, err := s.RecentOrdersByUser(ctx, 1, 2)
	if err != nil || len(recent) != 2 || recent[0].TotalCost != 1000000 {
		t.Fatalf("recent=%v err=%v want 2 rows, first total=1000000", recent, err)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/store/ -run TestOrderStats -v`
Expected: FAIL — `undefined: CountOrdersByUser`

- [ ] **Step 3: 实现 3 个方法**

在 `internal/store/orders.go` 末尾追加:

```go
// CountOrdersByUser 返回某用户的订单总数。
func (s *Store) CountOrdersByUser(ctx context.Context, userID int64) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM orders WHERE user_id=?`, userID).Scan(&n)
	return n, err
}

// RecentOrdersByUser 返回某用户最近的 limit 条订单(id 倒序)。
func (s *Store) RecentOrdersByUser(ctx context.Context, userID int64, limit int) ([]Order, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id,user_id,code_count,quota_per_code,total_cost,status,succeeded_count,failed_count,refunded_amount
		 FROM orders WHERE user_id=? ORDER BY id DESC LIMIT ?`, userID, limit)
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

// SumUsedByUser 返回某用户 created_at>=since 的订单 total_cost 之和(无则 0)。
func (s *Store) SumUsedByUser(ctx context.Context, userID, since int64) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(total_cost),0) FROM orders WHERE user_id=? AND created_at>=?`, userID, since).Scan(&n)
	return n, err
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/store/ -run TestOrderStats -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/store/orders.go internal/store/orders_stats_test.go
git commit -m "feat(store): 订单 count/recent/sum 查询(仪表盘用)"
```

---

## Task 5: Store 查询 — 用户 stats/total balance

**Files:**
- Modify: `internal/store/users.go`(追加 2 个方法)
- Test: `internal/store/users_stats_test.go`(新增)

- [ ] **Step 1: 写失败测试**

新建 `internal/store/users_stats_test.go`:

```go
package store

import "testing"

func TestUserStats_CountAndTotalBalance(t *testing.T) {
	s, _ := OpenInMemory()
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	_, _ = s.CreateUser("a@x.com", "h1", "user") // status=1, balance=0
	_, _ = s.CreateUser("b@x.com", "h2", "user")
	// users 表无 FK,直接 SQL 加余额(避开带 ctx 的 AddBalance,测试更可控)
	_, _ = s.db.Exec(`UPDATE users SET balance=balance+? WHERE email=?`, 500000, "a@x.com")
	_, _ = s.db.Exec(`UPDATE users SET balance=balance+? WHERE email=?`, 500000, "b@x.com")

	count, err := s.UserStats()
	if err != nil || count != 2 {
		t.Fatalf("count=%d err=%v want 2", count, err)
	}
	total, err := s.TotalBalance()
	if err != nil || total != 1000000 {
		t.Fatalf("total=%d err=%v want 1000000", total, err)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/store/ -run TestUserStats -v`
Expected: FAIL — `undefined: UserStats`

- [ ] **Step 3: 实现 2 个方法**

在 `internal/store/users.go` 末尾追加:

```go
// UserStats 返回启用用户数。
func (s *Store) UserStats() (int64, error) {
	var n int64
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users WHERE status=1`).Scan(&n)
	return n, err
}

// TotalBalance 返回所有启用用户的余额之和。
func (s *Store) TotalBalance() (int64, error) {
	var n int64
	err := s.db.QueryRow(`SELECT COALESCE(SUM(balance),0) FROM users WHERE status=1`).Scan(&n)
	return n, err
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/store/ -run TestUserStats -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/store/users.go internal/store/users_stats_test.go
git commit -m "feat(store): 用户数 + 总余额查询(管理仪表盘用)"
```

---

## Task 6: 购买错误脱敏

**Files:**
- Modify: `internal/web/user.go`(postBuy + 新增 `friendlyBuyError`)
- Test: `internal/web/buyerror_test.go`(新增)

- [ ] **Step 1: 写失败测试**

新建 `internal/web/buyerror_test.go`:

```go
package web

import (
	"errors"
	"faka-site/internal/newapi"
	"faka-site/internal/store"
	"testing"
)

func TestFriendlyBuyError(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{store.ErrInsufficient, "余额不足,请联系管理员充值"},
		{newapi.ErrCompliance, "服务暂不可用(兑换码功能未开启)"},
		{newapi.ErrUnauthorized, "服务暂不可用(配置异常)"},
		{newapi.ErrUpstream, "生成失败,请稍后重试"},
		{errors.New("boom"), "购买失败,请稍后重试"},
	}
	for _, c := range cases {
		if got := friendlyBuyError(c.err); got != c.want {
			t.Fatalf("for %v: got %q want %q", c.err, got, c.want)
		}
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/web/ -run TestFriendlyBuyError -v`
Expected: FAIL — `undefined: friendlyBuyError`

- [ ] **Step 3: 实现 `friendlyBuyError`**

在 `internal/web/user.go` 末尾追加:

```go
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
```

`user.go` 顶部 import 增加 `"errors"`、`"log"`、`"faka-site/internal/newapi"`、`"faka-site/internal/store"`(若已有则不重复)。完整 import 块:

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

- [ ] **Step 4: 改 `postBuy` 调用脱敏**

`internal/web/user.go` 中 `postBuy` 的尾部:

```go
	res, err := bs.Buy(r.Context(), currentUser(r).UserID, count, quota)
	d := map[string]any{"result": res}
	if err != nil {
		d["error"] = "购买失败:" + err.Error()
	}
	s.render(w, r, "buy.html", ViewData{Title: "购买", Data: d})
```

替换为:

```go
	res, err := bs.Buy(r.Context(), currentUser(r).UserID, count, quota)
	d := map[string]any{"result": res}
	if err != nil {
		log.Printf("buy failed: userID=%d count=%d quota=%d: %v", currentUser(r).UserID, count, quota, err)
		d["error"] = friendlyBuyError(err)
	}
	s.render(w, r, "buy.html", ViewData{Title: "购买", Data: d})
```

- [ ] **Step 5: 跑测试确认通过**

Run: `go test ./internal/web/ -run TestFriendlyBuyError -v && go build ./...`
Expected: PASS + 编译成功

- [ ] **Step 6: 提交**

```bash
git add internal/web/user.go internal/web/buyerror_test.go
git commit -m "fix(web): 购买错误脱敏,原始错误仅落日志"
```

---

## Task 7: 仪表盘数据接线(`dashboard` handler)

**Files:**
- Modify: `internal/web/user.go`(重写 `dashboard`)

- [ ] **Step 1: 重写 `dashboard`**

把 `internal/web/user.go` 的 `dashboard` 整个替换为:

```go
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
		"Balance":     balance,
		"OrderCount":  orderCount,
		"MonthlyUsed": monthlyUsed,
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
```

- [ ] **Step 2: 编译验证**

Run: `go build ./...`
Expected: 成功

- [ ] **Step 3: 跑全量测试**

Run: `go test ./...`
Expected: PASS(无回归)

- [ ] **Step 4: 提交**

```bash
git add internal/web/user.go
git commit -m "feat(web): 仪表盘概览数据接线(余额/订单数/月消费/最近订单)"
```

---

## Task 8: 重写侧栏布局 `layout.html`

**Files:**
- Modify: `internal/web/templates/layout.html`(整文件重写)

- [ ] **Step 1: 重写 layout**

把 `internal/web/templates/layout.html` 整文件替换为(注意:开关逻辑全部在底部带 nonce 的 `<script>` 里绑定,**不使用任何 `onclick=` 内联属性** —— CSP 不放行 `'unsafe-inline'`,内联事件处理器会被拦):

```html
{{define "layout"}}<!doctype html>
<html lang="zh"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}}</title>
<link rel="stylesheet" href="/static/pico.min.css">
<link rel="stylesheet" href="/static/style.css"></head>
<body>
<button class="menu-toggle" id="menuToggle" aria-label="菜单">☰</button>
<div class="overlay" id="overlay"></div>

<aside class="sidebar">
  <div class="brand">🎫 发卡站</div>
  <nav class="side-nav">
    {{if .User}}
      <a href="/" {{if eq .Title "概览"}}aria-current="page"{{end}}>概览</a>
      <a href="/buy" {{if eq .Title "购买"}}aria-current="page"{{end}}>买码</a>
      <a href="/orders" {{if eq .Title "订单"}}aria-current="page"{{end}}>订单</a>
      {{if eq .User.Role "admin"}}
      <hr>
      <a href="/admin/users" {{if eq .Title "用户管理"}}aria-current="page"{{end}}>用户</a>
      <a href="/admin/create" {{if eq .Title "建账户"}}aria-current="page"{{end}}>建账户</a>
      <a href="/admin/config" {{if eq .Title "配置"}}aria-current="page"{{end}}>配置</a>
      {{end}}
    {{else}}
      <a href="/login">登录</a>
    {{end}}
  </nav>
  {{if .User}}<div class="side-foot">
    <div class="who">{{.User.Email}}</div>
    <div class="bal">余额 {{.User.BalanceFmt}}</div>
    <a href="/logout" class="logout">退出</a>
  </div>{{end}}
</aside>

<main class="main">
  <header class="topbar"><h1>{{.Title}}</h1></header>
  <div class="content">{{template "content" .}}</div>
</main>

<script nonce="{{.Nonce}}">
(function(){
  var b = document.body;
  function toggle(){ b.classList.toggle('sidebar-open'); }
  function close(){ b.classList.remove('sidebar-open'); }
  var mt = document.getElementById('menuToggle'); if (mt) mt.addEventListener('click', toggle);
  var ov = document.getElementById('overlay'); if (ov) ov.addEventListener('click', close);
  document.querySelectorAll('.side-nav a').forEach(function(a){ a.addEventListener('click', close); });
})();
</script>
</body></html>{{end}}
```

说明:admin 子页的 `aria-current` 判定用页面标题——需保证各 admin handler 传的 `Title` 与上表一致(用户管理页 `Title:"用户管理"`、建账户 `Title:"建账户"`、配置 `Title:"配置"`)。Task 11 验证时如标题不匹配,就地修正 handler 的 `Title` 值即可。

- [ ] **Step 2: 编译验证**

Run: `go build ./...`
Expected: 成功(模板是 embed,build 时不会校验 HTML,但确保未破坏 go 源)

- [ ] **Step 3: 提交**

```bash
git add internal/web/templates/layout.html
git commit -m "feat(web): 重写为侧栏布局 + 抽屉菜单 + nonce script"
```

---

## Task 9: 翠绿主题 + 侧栏/卡片 CSS

**Files:**
- Modify: `internal/web/static/style.css`(整文件重写)

- [ ] **Step 1: 重写 style.css**

把 `internal/web/static/style.css` 整文件替换为:

```css
/* ===== 翠绿主题:覆盖 Pico 变量,组件全局跟随 ===== */
:root {
  --pico-primary: #10b981;
  --pico-primary-hover: #059669;
  --pico-primary-background: #10b981;
  --pico-primary-border: #10b981;
  --pico-primary-underline: rgba(16,185,129,.5);
}
@media (prefers-color-scheme: light){ :root{ --side-bg: #f7f8f8; } }
@media (prefers-color-scheme: dark){ :root{ --side-bg: #15171a; } }

/* ===== 侧栏布局 ===== */
body{ margin:0; display:flex; min-height:100vh; }
.sidebar{
  width:200px; flex:0 0 200px; background:var(--side-bg, #f7f8f8);
  padding:1rem .75rem; box-sizing:border-box; display:flex; flex-direction:column;
  position:fixed; top:0; bottom:0; left:0; overflow-y:auto;
}
.brand{ font-size:1.15rem; font-weight:700; padding:.25rem .5rem 1rem; }
.side-nav{ display:flex; flex-direction:column; gap:.1rem; }
.side-nav a{
  display:block; padding:.5rem .75rem; border-radius:.5rem;
  text-decoration:none; color:inherit; font-weight:600;
}
.side-nav a:hover{ background:rgba(16,185,129,.12); }
.side-nav a[aria-current="page"]{ background:rgba(16,185,129,.18); color:var(--pico-primary); }
.side-nav hr{ margin:.6rem .5rem; opacity:.4; }
.side-foot{ margin-top:auto; padding:.75rem .5rem 0; border-top:1px solid rgba(128,128,128,.25); font-size:.85rem; }
.side-foot .who{ word-break:break-all; }
.side-foot .bal{ color:var(--pico-primary); font-weight:700; margin:.15rem 0; }
.side-foot .logout{ color:inherit; }

.main{ flex:1 1 auto; margin-left:200px; min-width:0; }
.topbar{ padding:1rem 1.5rem; border-bottom:1px solid rgba(128,128,128,.2); }
.topbar h1{ margin:0; font-size:1.25rem; }
.content{ max-width:960px; margin:0 auto; padding:1.5rem 1rem 3rem; }

/* ===== 移动端抽屉(<1024px) ===== */
.menu-toggle, .overlay{ display:none; }
@media (max-width:1023px){
  body{ display:block; }
  .sidebar{ transform:translateX(-100%); transition:transform .2s; z-index:30; box-shadow:2px 0 12px rgba(0,0,0,.15); }
  body.sidebar-open .sidebar{ transform:translateX(0); }
  .main{ margin-left:0; }
  .menu-toggle{ display:inline-block; position:fixed; top:.6rem; right:.8rem; z-index:40; background:var(--pico-primary); color:#fff; border:0; border-radius:.4rem; width:2.2rem; height:2.2rem; font-size:1.1rem; }
  .overlay{ display:block; position:fixed; inset:0; background:rgba(0,0,0,.4); z-index:20; opacity:0; pointer-events:none; transition:opacity .2s; }
  body.sidebar-open .overlay{ opacity:1; pointer-events:auto; }
}

/* ===== 仪表盘卡片 + 通用 ===== */
.stat-grid{ display:grid; grid-template-columns:repeat(auto-fit,minmax(160px,1fr)); gap:1rem; margin-bottom:1.5rem; }
.stat-card{ border:1px solid rgba(128,128,128,.25); border-radius:.75rem; padding:1rem 1.25rem; }
.stat-card .big{ font-size:1.6rem; font-weight:700; color:var(--pico-primary); }
.stat-card .lbl{ color:var(--pico-muted-color, #888); font-size:.85rem; }
.err{ color:var(--pico-color-red-550, #c00); }
code{ word-break:break-all; }
```

- [ ] **Step 2: 重新构建并重启验证(手动)**

Run:
```bash
go build -o /tmp/faka-test . && \
FAKA_LISTEN=:8090 FAKA_DB=./data/faka.db SESSION_SECRET=testsecret123456 COOKIE_SECURE=false setsid /tmp/faka-test >/tmp/faka.log 2>&1 < /dev/null & disown
```
然后浏览器打开 `http://localhost:8090/login`,确认:侧栏在左、主内容居中、翠绿按钮。**预期:不再挤左上角。**

- [ ] **Step 3: 提交**

```bash
git add internal/web/static/style.css
git commit -m "feat(web): 翠绿主题 + 侧栏/卡片/响应式样式"
```

---

## Task 10: 仪表盘模板 `dashboard.html`

**Files:**
- Modify: `internal/web/templates/dashboard.html`(整文件重写)

- [ ] **Step 1: 重写 dashboard.html**

把 `internal/web/templates/dashboard.html` 整文件替换为:

```html
{{define "content"}}
<div class="stat-grid">
  <div class="stat-card"><div class="big">{{usd .Data.Balance}}</div><div class="lbl">当前余额</div></div>
  <div class="stat-card"><div class="big">{{.Data.OrderCount}}</div><div class="lbl">总订单数</div></div>
  <div class="stat-card"><div class="big">{{usd .Data.MonthlyUsed}}</div><div class="lbl">近 30 天消费</div></div>
  {{if .Data.UserCount}}
  <div class="stat-card"><div class="big">{{.Data.UserCount}}</div><div class="lbl">启用用户(admin)</div></div>
  <div class="stat-card"><div class="big">{{usd .Data.PlatformBalance}}</div><div class="lbl">平台总余额(admin)</div></div>
  {{end}}
</div>

<a href="/buy"><button>＋ 购买兑换码</button></a>

<h3>最近订单</h3>
{{if .Data.RecentOrders}}
<table>
  <tr><th>订单</th><th>数量</th><th>每码金额</th><th>状态</th></tr>
  {{range .Data.RecentOrders}}<tr>
    <td><a href="/orders/{{.ID}}">#{{.ID}}</a></td><td>{{.CodeCount}}</td><td>{{usd .QuotaPerCode}}</td><td>{{.Status}}</td>
  </tr>{{end}}
</table>
{{else}}<p>暂无订单,<a href="/buy">去购买</a>。</p>{{end}}
{{end}}
```

- [ ] **Step 2: 重新构建并手动验证**

Run: `go build -o /tmp/faka-test .`(重启服务同 Task 9 Step 2 的启动命令)

浏览器登录后访问 `/`,确认 3 张状态卡 + 购买按钮 + 最近订单表格正常;admin 账号能看到额外 2 张卡。**预期:数据正确显示。**

- [ ] **Step 3: 提交**

```bash
git add internal/web/templates/dashboard.html
git commit -m "feat(web): 仪表盘概览模板(状态卡 + 最近订单)"
```

---

## Task 11: 验证其余页面适配 + 安全头端到端

**Files:**
- 无代码改动(验证任务);如发现破坏再就地修

- [ ] **Step 1: 逐页验证**

重启服务后,用 admin 账号逐个访问并确认在新侧栏布局下正常:`/buy`、`/orders`、`/orders/{id}`、`/admin/users`、`/admin/create`、`/admin/balance`、`/admin/config`。

- [ ] **Step 2: 验证安全头(端到端)**

Run:
```bash
curl -s -D - -o /dev/null http://127.0.0.1:8090/login | grep -iE 'content-security|x-frame|x-content|referrer'
```
Expected: 4 个头都在,CSP 含 `nonce-`。

- [ ] **Step 3: 验证 nonce 真的放行了抽屉脚本**

浏览器 DevTools → Console 应**无** CSP 报错;窄屏(<1024px)点 ☰ 能开/关抽屉。

- [ ] **Step 4: 验证错误脱敏**

把 NewAPI 配置改成无效 token,在 `/buy` 下单,确认页面只显示友好文案(如"服务暂不可用(配置异常)"),不出现原始 `err.Error()`。

- [ ] **Step 5: 全量测试 + 构建**

Run: `go test ./... && go build ./...`
Expected: 全 PASS、构建成功

- [ ] **Step 6: 推送**

```bash
git push origin main
```

---

## Self-Review 结果

**Spec 覆盖:** §4.1 安全头→Task 3 ✓;§4.2 clientIP→Task 1/2 ✓;§4.3 错误脱敏→Task 6 ✓;§4.4 登录 CSRF 接受风险(不改)→ 无任务,符合预期 ✓;§5.1 布局→Task 8 ✓;§5.2 主题→Task 9 ✓;§5.3 仪表盘→Task 4/5/7/10 ✓;§5.4 移动抽屉→Task 8/9 ✓;§5.5 其余页面→Task 11 ✓。

**类型一致性:** `clientIP`/`friendlyBuyError`/`CountOrdersByUser`/`RecentOrdersByUser`/`SumUsedByUser`/`UserStats`/`TotalBalance`/`securityHeaders`/`nonceFromContext` 在定义处与调用处签名一致。dashboard 注入的 `Balance`/`OrderCount`/`MonthlyUsed`/`RecentOrders`/`UserCount`/`PlatformBalance` 与模板 `.Data.*` 一致。

**无占位符:** 所有步骤含完整代码。
