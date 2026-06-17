# 发卡站 用户管理流程重构 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把建号改成"管理员填邮箱+密码"(不依赖 SMTP),SMTP 唯一用途改为用户自助"忘记密码"重置(直接邮件新随机临时密码),管理员重置改成表单设密码,并加角色徽章强化权限可见性。

**Architecture:** 纯函数(密码校验)用 TDD;建号/管理员重置走友好文案 + 表单;`/forgot` 引入可注入 `mailSender` 接口以便单测(注入假 mailer 断言"存在用户→发信、不存在→不落库")。技术栈不变(Go `html/template` + SQLite + 单二进制)。

**Tech Stack:** Go 1.25、`html/template`、`modernc.org/sqlite`、bcrypt、`crypto/rand`。

**Spec:** `docs/specs/2026-06-17-faka-usermgmt-design.md`

---

## File Map

| 文件 | 责任 | 动作 |
|---|---|---|
| `internal/web/admin.go` | 建号 / 管理员重置 / 密码校验 | 改 |
| `internal/web/auth.go` | 登录 + `/forgot` 自助重置 | 改 |
| `internal/web/server.go` | 路由 + `mailSender` 字段 | 改 |
| `internal/web/templates/admin_create.html` | 邮箱+密码+确认表单 | 重写 |
| `internal/web/templates/admin_reset.html` | 新密码+确认表单 | 新增 |
| `internal/web/templates/admin_users.html` | 重置按钮改链接 | 改 |
| `internal/web/templates/forgot.html` | 邮箱表单 | 新增 |
| `internal/web/templates/login.html` | 忘记密码链接 | 改 |
| `internal/web/templates/layout.html` | 角色徽章 | 改 |
| `internal/web/static/style.css` | 角色徽章样式 | 改(小) |

测试文件:`internal/web/password_test.go`、`internal/web/forgot_test.go`(新增)。

---

## Task 1: 密码校验纯函数 `validateNewPassword`

**Files:**
- Modify: `internal/web/admin.go`(末尾追加)
- Test: `internal/web/password_test.go`(新增)

- [ ] **Step 1: 写失败测试**

新建 `internal/web/password_test.go`:

```go
package web

import "testing"

func TestValidateNewPassword(t *testing.T) {
	cases := []struct{ pw, confirm, want string }{
		{"", "", "密码至少 6 位"},
		{"123", "123", "密码至少 6 位"},
		{"123456", "654321", "两次密码不一致"},
		{"123456", "123456", ""},
		{"abcdef", "abcdef", ""},
	}
	for _, c := range cases {
		if got := validateNewPassword(c.pw, c.confirm); got != c.want {
			t.Fatalf("validate(%q,%q)=%q want %q", c.pw, c.confirm, got, c.want)
		}
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/web/ -run TestValidateNewPassword -v`
Expected: FAIL — `undefined: validateNewPassword`

- [ ] **Step 3: 实现函数**

在 `internal/web/admin.go` 末尾追加:

```go
// validateNewPassword 校验新密码:长度≥6 且两次一致。返回友好错误串,合法则空串。
func validateNewPassword(pw, confirm string) string {
	if len(pw) < 6 {
		return "密码至少 6 位"
	}
	if pw != confirm {
		return "两次密码不一致"
	}
	return ""
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/web/ -run TestValidateNewPassword -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/web/admin.go internal/web/password_test.go
git commit -m "feat(web): 密码校验纯函数 validateNewPassword"
```

---

## Task 2: 建号改为邮箱+密码(不发邮件)

**Files:**
- Modify: `internal/web/admin.go`(`postCreate`)
- Modify: `internal/web/templates/admin_create.html`(重写)

- [ ] **Step 1: 重写 `postCreate`**

`internal/web/admin.go` 中 `postCreate` 当前是:

```go
func (s *Server) postCreate(w http.ResponseWriter, r *http.Request) {
	email := r.PostFormValue("email")
	pw := randPassword()
	hash, _ := auth.HashPassword(pw)
	if _, err := s.store.CreateUser(email, hash, "user"); err != nil {
		log.Printf("create user failed: email=%s: %v", email, err)
		s.render(w, r, "admin_create.html", ViewData{Title: "建账户", Data: map[string]any{"error": "创建失败,该邮箱可能已存在"}})
		return
	}
	var mailErr string
	if m := s.mailer(); m != nil {
		if err := m.Send(email, "你的发卡站账户", "邮箱:"+email+"\n初始密码:"+pw+"\n请登录后修改。"); err != nil {
			mailErr = err.Error()
		}
	} else {
		mailErr = "SMTP 未配置"
	}
	s.render(w, r, "admin_create.html", ViewData{Title: "建账户", Data: map[string]any{
		"newEmail": email, "newPassword": pw, "mailErr": mailErr,
	}})
}
```

替换为:

```go
func (s *Server) postCreate(w http.ResponseWriter, r *http.Request) {
	email := strings.TrimSpace(r.PostFormValue("email"))
	pw := r.PostFormValue("password")
	if msg := validateNewPassword(pw, r.PostFormValue("confirm")); msg != "" {
		s.render(w, r, "admin_create.html", ViewData{Title: "建账户", Data: map[string]any{"error": msg, "email": email}})
		return
	}
	hash, _ := auth.HashPassword(pw)
	if _, err := s.store.CreateUser(email, hash, "user"); err != nil {
		log.Printf("create user failed: email=%s: %v", email, err)
		s.render(w, r, "admin_create.html", ViewData{Title: "建账户", Data: map[string]any{"error": "创建失败,该邮箱可能已存在", "email": email}})
		return
	}
	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}
```

确认 `admin.go` import 含 `"strings"`、`"net/http"`、`"log"`、`"faka-site/internal/auth"`(均已在)。

- [ ] **Step 2: 重写模板**

`internal/web/templates/admin_create.html` 整文件替换为:

```html
{{define "content"}}
<h2>建账户</h2>
{{if .Data.error}}<p class="err">{{.Data.error}}</p>{{end}}
<form method="post" action="/admin/create"><input name="csrf" type="hidden" value="{{.CSRF}}">
  <input name="email" placeholder="用户邮箱" value="{{.Data.email}}" required><br>
  <input name="password" type="password" placeholder="密码(≥6位)" required><br>
  <input name="confirm" type="password" placeholder="确认密码" required><br>
  <button>创建</button>
</form>{{end}}
```

- [ ] **Step 3: 编译 + 全量测试**

Run: `go build ./... && go test ./...`
Expected: 全 PASS

- [ ] **Step 4: 提交**

```bash
git add internal/web/admin.go internal/web/templates/admin_create.html
git commit -m "feat(web): 建号改为管理员填邮箱+密码(不依赖 SMTP)"
```

---

## Task 3: 管理员重置改为表单设密码(不发邮件)

**Files:**
- Modify: `internal/web/admin.go`(删 `postUserReset`,加 `getReset`/`postReset`)
- Modify: `internal/web/server.go`(路由)
- Create: `internal/web/templates/admin_reset.html`
- Modify: `internal/web/templates/admin_users.html`(按钮改链接)

- [ ] **Step 1: 删 `postUserReset`,加两个 handler**

`internal/web/admin.go` 中删除整个 `postUserReset` 函数(当前约 105–127 行),替换为:

```go
func (s *Server) getReset(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
	u, _ := s.store.UserByID(id)
	s.render(w, r, "admin_reset.html", ViewData{Title: "重置密码", Data: map[string]any{"target": u}})
}

func (s *Server) postReset(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PostFormValue("id"), 10, 64)
	pw := r.PostFormValue("password")
	if msg := validateNewPassword(pw, r.PostFormValue("confirm")); msg != "" {
		u, _ := s.store.UserByID(id)
		s.render(w, r, "admin_reset.html", ViewData{Title: "重置密码", Data: map[string]any{"target": u, "error": msg}})
		return
	}
	hash, _ := auth.HashPassword(pw)
	s.store.SetUserPassword(id, hash)
	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}
```

- [ ] **Step 2: 改路由**

`internal/web/server.go` 的 `adminMux` 块中删除:

```go
	adminMux.HandleFunc("POST /users/{id}/reset", s.postUserReset)
```

替换为:

```go
	adminMux.HandleFunc("GET /reset", s.getReset)
	adminMux.HandleFunc("POST /reset", s.postReset)
```

- [ ] **Step 3: 新建模板 `admin_reset.html`**

```html
{{define "content"}}
<h2>重置 {{.Data.target.Email}} 的密码</h2>
{{if .Data.error}}<p class="err">{{.Data.error}}</p>{{end}}
<form method="post" action="/admin/reset"><input name="csrf" type="hidden" value="{{.CSRF}}">
  <input name="id" type="hidden" value="{{.Data.target.ID}}">
  <input name="password" type="password" placeholder="新密码(≥6位)" required><br>
  <input name="confirm" type="password" placeholder="确认新密码" required><br>
  <button>重置</button>
</form>{{end}}
```

- [ ] **Step 4: `admin_users.html` 按钮改链接**

`internal/web/templates/admin_users.html` 中这一行:

```html
  <form method="post" action="/admin/users/{{.ID}}/reset" style="display:inline"><input name="csrf" type="hidden" value="{{$.CSRF}}"><button>重置密码</button></form>
```

替换为:

```html
  <a href="/admin/reset?id={{.ID}}">重置密码</a>
```

- [ ] **Step 5: 编译 + 全量测试**

Run: `go build ./... && go test ./...`
Expected: 全 PASS(无 `postUserReset` 残留引用)

- [ ] **Step 6: 提交**

```bash
git add internal/web/admin.go internal/web/server.go internal/web/templates/admin_reset.html internal/web/templates/admin_users.html
git commit -m "feat(web): 管理员重置密码改为表单设密码(不发邮件)"
```

---

## Task 4: 用户自助 `/forgot` 重置(SMTP 唯一用途)

**Files:**
- Modify: `internal/web/server.go`(Server 加 `mailSender` 字段 + `NewServer` 不变 + 新增公共路由)
- Modify: `internal/web/auth.go`(加 `mailSender` 接口 + `smtpMailer` + `getForgot`/`postForgot`)
- Create: `internal/web/templates/forgot.html`
- Modify: `internal/web/templates/login.html`(加链接)
- Test: `internal/web/forgot_test.go`(新增)

- [ ] **Step 1: 写失败测试**

新建 `internal/web/forgot_test.go`:

```go
package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"faka-site/internal/auth"
	"faka-site/internal/store"
)

type fakeMailer struct{ got string }

func (f *fakeMailer) Send(to, subject, body string) error { f.got = to; return nil }

// newForgotServer 构造一个用内存库 + 假 mailer 的 Server(不读配置、不碰真实 DB)。
func newForgotServer(t *testing.T, seedEmail string) (*Server, *fakeMailer) {
	t.Helper()
	st, _ := store.OpenInMemory()
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	if seedEmail != "" {
		_, _ = st.CreateUser(seedEmail, "seedhash", "user") // hash 会被 postForgot 覆盖,占位即可
	}
	fm := &fakeMailer{}
	return &Server{store: st, throttle: auth.NewThrottle(100), now: time.Now, mailSender: fm}, fm
}

func TestPostForgot_SendsForExistingUser(t *testing.T) {
	s, fm := newForgotServer(t, "real@x.com")
	req := httptest.NewRequest("POST", "/forgot", strings.NewReader("email=real@x.com"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.postForgot(rec, req)
	if fm.got != "real@x.com" {
		t.Fatalf("expected mail to real@x.com, got %q", fm.got)
	}
	if !strings.Contains(rec.Body.String(), "新密码已发送") {
		t.Fatalf("expected uniform sent message, got: %s", rec.Body.String())
	}
}

func TestPostForgot_NoMailForUnknownUser(t *testing.T) {
	s, fm := newForgotServer(t, "real@x.com")
	req := httptest.NewRequest("POST", "/forgot", strings.NewReader("email=ghost@x.com"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.postForgot(rec, req)
	if fm.got != "" {
		t.Fatalf("must not mail unknown user, but mailed %q", fm.got)
	}
	if !strings.Contains(rec.Body.String(), "新密码已发送") {
		t.Fatalf("must show same uniform message to avoid enumeration, got: %s", rec.Body.String())
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/web/ -run TestPostForgot -v`
Expected: FAIL — `undefined: postForgot` / `s.mailSender undefined`

- [ ] **Step 3: Server 加 `mailSender` 字段**

`internal/web/server.go` 的 `Server` 结构体当前:

```go
type Server struct {
	store        *store.Store
	secret       []byte
	throttle     *auth.Throttle
	now          func() time.Time
	secureCookie bool
}
```

改为:

```go
type Server struct {
	store        *store.Store
	secret       []byte
	throttle     *auth.Throttle
	now          func() time.Time
	secureCookie bool
	mailSender   mailSender // 可注入(测试用);nil 时由配置构建
}
```

`NewServer` 不变(`mailSender` 零值为 nil)。

- [ ] **Step 4: 加接口 + `smtpMailer` + 两个 handler(放 `auth.go`)**

在 `internal/web/auth.go` 顶部 import 块(当前为 `net/http`、`strings`、`faka-site/internal/auth`)后,追加接口与方法、handler。在文件末尾追加:

```go
// mailSender 抽象发信,便于 /forgot 注入假实现做单测。
type mailSender interface {
	Send(to, subject, body string) error
}

// smtpMailer 返回注入的 mailSender;未注入时由站点配置构建(未配 SMTP 则 nil)。
func (s *Server) smtpMailer() mailSender {
	if s.mailSender != nil {
		return s.mailSender
	}
	return s.mailer()
}

func (s *Server) getForgot(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, "forgot.html", ViewData{Title: "忘记密码"})
}

func (s *Server) postForgot(w http.ResponseWriter, r *http.Request) {
	email := strings.TrimSpace(r.PostFormValue("email"))
	ip := clientIP(r)
	if !s.throttle.Allow(ip + "|" + email) {
		s.render(w, r, "forgot.html", ViewData{Title: "忘记密码", Data: map[string]any{"error": "尝试过多,稍后再试"}})
		return
	}
	ms := s.smtpMailer()
	if ms == nil {
		s.render(w, r, "forgot.html", ViewData{Title: "忘记密码", Data: map[string]any{"msg": "SMTP 未配置,请联系管理员"}})
		return
	}
	if u, err := s.store.UserByEmail(email); err == nil {
		pw := randPassword()
		hash, _ := auth.HashPassword(pw)
		s.store.SetUserPassword(u.ID, hash)
		_ = ms.Send(u.Email, "发卡站密码重置", "你的新密码:"+pw)
	}
	// 统一文案,防止用户枚举
	s.render(w, r, "forgot.html", ViewData{Title: "忘记密码", Data: map[string]any{"msg": "如该邮箱已注册,新密码已发送至该邮箱"}})
}
```

`randPassword` 定义在 `admin.go`(同 package),可直接调用。`clientIP` 定义在 `middleware.go`(同 package)。

- [ ] **Step 5: 新建模板 `forgot.html`**

```html
{{define "content"}}
<h2>忘记密码</h2>
{{if .Data.error}}<p class="err">{{.Data.error}}</p>{{end}}
{{if .Data.msg}}<p>{{.Data.msg}}</p>{{end}}
<form method="post" action="/forgot">
  <input name="email" placeholder="注册邮箱" required><br>
  <button>发送重置邮件</button>
</form>
<p><a href="/login">返回登录</a></p>{{end}}
```

- [ ] **Step 6: `login.html` 加链接**

`internal/web/templates/login.html` 整文件替换为:

```html
{{define "content"}}
<h2>登录</h2>
{{if .Data.error}}<p class="err">{{.Data.error}}</p>{{end}}
<form method="post" action="/login">
  <input name="email" placeholder="邮箱" required><br>
  <input name="password" type="password" placeholder="密码" required><br>
  <button>登录</button>
</form>
<p><a href="/forgot">忘记密码?</a></p>{{end}}
```

- [ ] **Step 7: 注册公开路由**

`internal/web/server.go` 中,在登录路由旁(`mux.HandleFunc("POST /login", ...)` 之后)加:

```go
	mux.HandleFunc("GET /forgot", s.getForgot)
	mux.HandleFunc("POST /forgot", s.postForgot)
```

- [ ] **Step 8: 跑测试确认通过**

Run: `go test ./internal/web/ -run TestPostForgot -v`
Expected: PASS(2 用例:存在→发信、不存在→不发且文案一致)

- [ ] **Step 9: 编译 + 全量测试**

Run: `go build ./... && go test ./...`
Expected: 全 PASS

- [ ] **Step 10: 提交**

```bash
git add internal/web/server.go internal/web/auth.go internal/web/templates/forgot.html internal/web/templates/login.html internal/web/forgot_test.go
git commit -m "feat(web): 用户自助 /forgot 密码重置(SMTP 唯一用途,防枚举)"
```

---

## Task 5: 侧栏角色徽章

**Files:**
- Modify: `internal/web/templates/layout.html`
- Modify: `internal/web/static/style.css`(小)

- [ ] **Step 1: layout 加徽章**

`internal/web/templates/layout.html` 中侧栏底部:

```html
  {{if .User}}<div class="side-foot">
    <div class="who">{{.User.Email}}</div>
    <div class="bal">余额 {{.User.BalanceFmt}}</div>
    <a href="/logout" class="logout">退出</a>
  </div>{{end}}
```

替换为:

```html
  {{if .User}}<div class="side-foot">
    <div class="who">{{.User.Email}} <small class="role">{{if eq .User.Role "admin"}}管理员{{else}}普通用户{{end}}</small></div>
    <div class="bal">余额 {{.User.BalanceFmt}}</div>
    <a href="/logout" class="logout">退出</a>
  </div>{{end}}
```

- [ ] **Step 2: 加徽章样式**

`internal/web/static/style.css` 末尾追加:

```css
.side-foot .role{ opacity:.7; font-weight:400; font-size:.75rem; }
```

- [ ] **Step 3: 编译验证**

Run: `go build ./...`
Expected: 成功

- [ ] **Step 4: 提交**

```bash
git add internal/web/templates/layout.html internal/web/static/style.css
git commit -m "feat(web): 侧栏角色徽章(管理员/普通用户)"
```

---

## Task 6: 端到端验证

**Files:** 无代码改动(验证)

- [ ] **Step 1: 构建并在临时库上冒烟(不碰真实 data/faka.db)**

```bash
cd /home/lisa/matrix/faka-site
go build -o /tmp/faka-um . 2>&1
rm -f /tmp/faka-um.db*
FAKA_DB=/tmp/faka-um.db FAKA_LISTEN=:8093 SESSION_SECRET=t COOKIE_SECURE=false ADMIN_EMAIL=admin ADMIN_PASSWORD=1 setsid /tmp/faka-um >/tmp/faka-um.log 2>&1 < /dev/null & disown
sleep 2
```

- [ ] **Step 2: 验证建号(邮箱+密码)+ 登录**

```bash
CJ=$(mktemp)
curl -s -c $CJ -X POST http://127.0.0.1:8093/login -d "email=admin&password=1" -o /dev/null -w "admin login=%{http_code}\n"
# 建一个普通用户(密码 admin 自设)
csrf=$(curl -s -b $CJ http://127.0.0.1:8093/admin/create | grep -o 'value="[a-f0-9]*"' | head -1 | sed 's/value="//;s/"//')
curl -s -b $CJ -o /dev/null -w "create=%{http_code}→%{redirect_url}\n" -X POST http://127.0.0.1:8093/admin/create -d "csrf=$csrf&email=u1@x.com&password=secret1&confirm=secret1"
# 用该用户登录
curl -s -o /dev/null -w "u1 login=%{http_code}→%{redirect_url}\n" -X POST http://127.0.0.1:8093/login -d "email=u1@x.com&password=secret1"
rm -f $CJ
```

Expected: admin login 303;create 303→/admin/users;u1 login 303→/(自设密码可用)。

- [ ] **Step 3: 验证 /forgot(无 SMTP → 友好提示;防枚举文案一致)**

```bash
curl -s -X POST http://127.0.0.1:8093/forgot -d "email=ghost@x.com" | grep -oE 'SMTP 未配置|新密码已发送'
curl -s -X POST http://127.0.0.1:8093/forgot -d "email=u1@x.com" | grep -oE 'SMTP 未配置|新密码已发送'
```

Expected:两条都显示"SMTP 未配置"(因临时库没配 SMTP),文案一致。

- [ ] **Step 4: 验证角色徽章渲染**

```bash
CJ=$(mktemp); curl -s -c $CJ -X POST http://127.0.0.1:8093/login -d "email=admin&password=1" -o /dev/null
curl -s -b $CJ http://127.0.0.1:8093/ | grep -o '管理员'
rm -f $CJ
```

Expected:输出"管理员"。

- [ ] **Step 5: 收尾**

```bash
pkill -f faka-um 2>/dev/null
go test ./...
```

Expected:全 PASS。

---

## Self-Review 结果

**Spec 覆盖:** §3.1 建号邮箱+密码→Task 2 ✓;§3.2 管理员重置表单→Task 3 ✓;§3.3 /forgot 自助重置→Task 4 ✓;§3.4 登录页链接→Task 4 Step 6 ✓;§4 权限分层(徽章/恒 user)→Task 2(role=user)+ Task 5 ✓;§5 安全(限流/防枚举/长度校验)→Task 1+4 ✓;§6 文件清单全部覆盖。

**类型一致性:** `validateNewPassword(pw,confirm)string` 在 Task1 定义、Task2/3 调用一致;`mailSender` 接口、`smtpMailer()`、`getReset/postReset/getForgot/postForgot` 定义与路由注册一致;模板字段 `{{.Data.target.ID/Email}}`、`{{.Data.email}}` 与 handler 传入一致。

**无占位符:** 所有步骤含完整代码与命令。
