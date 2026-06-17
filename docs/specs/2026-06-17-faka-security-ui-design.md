# 发卡站 — 安全加固 + UI 重构 设计文档

- 日期: 2026-06-17
- 状态: Draft (待用户复审)
- 相关: `docs/specs/2026-06-15-faka-site-design.md`(初版整体设计)
- 部署: 本地运行(开发),生产经 Caddy(HTTPS)反代至 NewAPI 同机

---

## 1. 背景与目标

初版发卡站已上线(单二进制 + SQLite + Go `html/template` + Pico.css)。本次针对两个维度优化:

1. **安全加固** —— 审计发现底层(注入/会话/越权)扎实,但缺表层 HTTP 安全头、存在信息泄露与限流可绕过问题。
2. **UI 重构** —— 当前内容挤在左上角(根因:Pico v2 不自动居中 `<body>`,初版只设了 `max-width` 未用 `.container`/`margin:auto`),且首页过于简陋。重构为侧栏仪表盘 + 定制主题。

**核心目标**
- 补齐标准 HTTP 安全响应头,堵掉点击劫持 / MIME 嗅探 / 限流绕过 / 错误信息泄露。
- UI 改为固定侧栏 + 居中主内容区,翠绿主题,带仪表盘概览页,移动端可用。
- 技术栈不变:仍为 Go 模板 + Pico.css + 单二进制,不引入 JS 框架。

**非目标(明确不做)**
- 不重写为前后端分离 / 不引入 React / Node 构建链。
- 不做在线支付、自助注册。
- 不引入会话服务端存储(保持无状态 HMAC cookie)。
- 不为登录额外加 CSRF token(评估为 SameSite=Lax 已覆盖的已知接受风险,见 §4.4)。

---

## 2. 已确认决策

| 维度 | 决策 |
|---|---|
| UI 方向 | 侧栏仪表盘 + 定制主题(用户已选「主题定制+仪表盘」) |
| 主色调 | **翠绿 emerald** `--pico-primary: #10b981`(用户已确认) |
| 明暗模式 | 沿用 Pico 自动跟随系统 `prefers-color-scheme`,不强制 |
| 仪表盘指标 | 3 张卡:当前余额 / 总订单数 / 近 30 天消费(admin 额外:总用户数、平台总余额) |
| 安全加固范围 | 安全响应头 + 可信客户端 IP + 购买错误脱敏;登录 CSRF 列为接受风险 |
| JS | 零 JS 框架;仅必要的原生 JS(移动端抽屉菜单开关,见 §5.4) |

---

## 3. 安全审计结论(现状)

| 攻击面 | 现状 | 依据 |
|---|---|---|
| SQL 注入 | ✅ 安全 | 全部查询 `?` 参数化,无字符串拼接(`store/*.go`) |
| CSRF | ✅ 已防护 | `csrfCheck` 校验表单 `csrf` == 会话内 token;cookie `SameSite=Lax` |
| XSS | ✅ 安全 | `html/template` 自动转义,无 `template.HTML` |
| 会话伪造 | ✅ 安全 | HMAC-SHA256 签名 + 过期校验 + `hmac.Equal` 恒定时间比较 |
| 越权 IDOR | ✅ 已防护 | `orderDetail` 校验属主 → 404;管理页 `requireAdmin` 校验角色 |
| 余额透支 | ✅ 安全 | `HoldForOrder` 条件 `UPDATE...WHERE balance>=?` + `RowsAffected` |
| 密码存储 | ✅ 安全 | bcrypt |
| **安全响应头** | ⚠️ 缺失 | 无 CSP / X-Frame-Options / X-Content-Type-Options / Referrer-Policy |
| **限流 IP 可信度** | ⚠️ 可绕过 | `auth.go:17` 直接信任 `X-Forwarded-For`,可伪造 |
| **错误信息泄露** | ⚠️ 存在 | `user.go:38` 把上游 `err.Error()` 原样回显 |
| 登录 CSRF | ⚠️ 低危 | 登录 POST 免 CSRF;`SameSite=Lax` 已基本覆盖 |

---

## 4. 安全加固设计

### 4.1 安全响应头中间件(新增 `internal/web/headers.go`)

对**所有**响应注入以下头(`<NONCE>` 为每次请求随机生成,见 nonce 说明):

```
Content-Security-Policy: default-src 'self'; script-src 'self' 'nonce-<NONCE>'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; frame-ancestors 'none'
X-Frame-Options: DENY
X-Content-Type-Options: nosniff
Referrer-Policy: no-referrer
```

说明:
- `style-src 'self' 'unsafe-inline'`:模板中存在内联 `style="..."`(如订单页表单按钮的 `display:inline`),故样式放行内联。
- `script-src 'self' 'nonce-<NONCE>'`:本项目唯一的内联脚本(§5.4 移动端抽屉开关,~10 行)用 **per-request nonce** 放行,不放宽 `'unsafe-inline'`。中间件每次请求生成 16 字节随机 nonce,同时写入 CSP 头与模板 `<script nonce="{{.Nonce}}">`(经 `ViewData.Nonce` 注入,见 §6)。这样内联脚本被允许,其余内联脚本仍被拦。
- `X-Frame-Options: DENY` 与 CSP `frame-ancestors 'none'` 双保险(老浏览器认前者)。
- 该中间件包在路由最外层(`server.go` 的 `mux` 外再套一层,或作为首个 `mux.Use`)。

### 4.2 可信客户端 IP(改 `middleware.go`,新增 `clientIP`)

```go
func clientIP(r *http.Request) string {
    host, _, err := net.SplitHostPort(r.RemoteAddr)
    if err != nil {
        host = r.RemoteAddr
    }
    // 仅当直连来自回环(即经过本机 Caddy 反代)时才信任 XFF
    if isLoopback(host) {
        if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
            return strings.TrimSpace(strings.SplitN(xff, ",", 2)[0])
        }
    }
    return host
}
```

`auth.go` 登录限流键由 `ip + "|" + email` 改为 `clientIP(r) + "|" + email`。直连(非回环)时无视 XFF → 攻击者无法靠伪造 XFF 稀释限流键。

### 4.3 购买错误脱敏(改 `user.go:postBuy`)

不再 `"购买失败:" + err.Error()`。改为映射:

| 上游错误类型 | 对用户显示 |
|---|---|
| 余额不足 (`store.ErrInsufficient`) | 余额不足,请联系管理员充值 |
| NewAPI 合规未开 (`newapi.ErrCompliance`) | 服务暂不可用(兑换码功能未开启) |
| NewAPI 鉴权失败 (`newapi.ErrUnauthorized`) | 服务暂不可用(配置异常) |
| 上游不可达 (`newapi.ErrUpstream`) | 生成失败,请稍后重试 |
| 其他 | 购买失败,请稍后重试 |

完整错误 `log.Printf("buy failed: userID=%d count=%d quota=%d: %v", ...)` 落服务端日志,便于排查但不暴露给用户。

### 4.4 登录 CSRF(接受风险)

登录 POST 不做 CSRF 校验。风险:攻击者诱导受害者登录进攻击者账户(login CSRF)。缓解:会话 cookie 已设 `SameSite=Lax`,浏览器会阻止带 body 的跨站 POST 到达登录端点,基本覆盖该攻击。**结论:列为已知接受风险,不额外加 token**,避免过度设计。若后续有强需求再加同源 Origin/Referer 校验。

---

## 5. UI 重构设计

### 5.1 布局:侧栏 + 主内容区

重写 `templates/layout.html`,结构:

```
<body>
  <aside class="sidebar">
    品牌 + 导航(概览/买码/订单)+ admin 组 + 底部用户信息
  </aside>
  <div class="main">
    <header class="topbar">页面标题 + 移动端汉堡按钮</header>
    <main class="content">{{template "content" .}}</main>
  </div>
</body>
```

- 侧栏固定宽度 200px,背景 `--pico-secondary-background`(比主区略深)。
- 主内容区内部 `.content` 限宽 960px 并居中。
- 侧栏导航用 `<nav>` + `<a>`,当前页高亮(`aria-current="page"` + 强调色)。

### 5.2 配色主题(改 `style.css` 顶部)

覆盖 Pico 变量:

```css
:root {
  --pico-primary: #10b981;
  --pico-primary-hover: #059669;
  --pico-primary-background: #10b981;
  --pico-primary-border: #10b981;
}
```

全部按钮 / 链接 / 焦点环 / 徽章自动跟随翠绿。明暗模式沿用 Pico 默认。

### 5.3 仪表盘概览页(改 `dashboard.html`,新增数据查询)

`/` 路由渲染概览,数据:
- `Balance`(当前用户余额,已有)
- `OrderCount`(新增 `store.CountOrdersByUser(userID)`)
- `MonthlyUsed`(新增 `store.SumUsedByUser(userID, sinceUnix)`,近 30 天已扣额度)

模板:3 张状态卡(网格)→ 快捷购买按钮 → 最近 5 单表格(`store.RecentOrdersByUser(userID, 5)`)。
admin 额外两张卡:总用户数、平台总余额(新增 `store.UserStats()`、`store.TotalBalance()`)。

### 5.4 移动端响应式 + 抽屉菜单

- 断点 `<1024px`:侧栏 `position:fixed` 移出屏幕左外侧(`transform: translateX(-100%)`),主区占满宽。
- 顶栏汉堡按钮 `button.menu-toggle`,点击给 `<body>` 加 `.sidebar-open` 类,侧栏 `translateX(0)` 滑入;点击遮罩/链接移除该类。
- 唯一内联 `<script>`(~10 行 toggle 逻辑),用 §4.1 的 nonce 放行。

### 5.5 其他页面美化

`buy` / `orders` / `order` / `login` / `admin_*`:套用新侧栏布局自动受益(共用 layout),表单用 Pico `<article>`/卡片包裹,表格用 Pico 默认表样式。错误提示用带色 `<small>` 或提示块。

---

## 6. 文件改动清单

| 文件 | 改动 |
|---|---|
| 新增 `internal/web/headers.go` | 安全响应头 + CSP nonce 中间件 |
| `internal/web/middleware.go` | 新增 `clientIP(r)` + `isLoopback` |
| `internal/web/server.go` | 挂载 headers 中间件(最外层);概览路由接数据;模板注入 nonce |
| `internal/web/auth.go` | 限流键改用 `clientIP(r)` |
| `internal/web/user.go` | `postBuy` 错误脱敏映射 |
| `internal/web/render.go` | `ViewData` 增加 `Nonce` 字段并注入 |
| `internal/store/orders.go` | 新增 `CountOrdersByUser` / `RecentOrdersByUser` / `SumUsedByUser` |
| `internal/store/users.go` | 新增 `UserStats` / `TotalBalance` |
| `internal/web/templates/layout.html` | 重写为侧栏布局 + 抽屉 + nonce script |
| `internal/web/templates/dashboard.html` | 状态卡 + 最近订单概览 |
| 其余模板 | 适配新布局(主要受益于共用 layout) |
| `internal/web/static/style.css` | 主题变量 + 侧栏/卡片/响应式样式 |

---

## 7. 测试

- **安全头**:对 `/login`、`/`(登录后)发请求,断言响应含 4 个安全头且 CSP 含 nonce;断言不同请求 nonce 不同。
- **clientIP**:构造 `RemoteAddr` 为回环 + 带 XFF → 返回 XFF 首段;`RemoteAddr` 为公网 IP + 带 XFF → 返回该公网 IP(忽略 XFF)。
- **错误脱敏**:mock `Issuer` 返回各类错误,断言页面文案为映射后的友好语,不含原始 err 字符串。
- **store 新查询**:`CountOrdersByUser` / `RecentOrdersByUser` / `SumUsedByUser` / `UserStats` / `TotalBalance` 各写表驱动测试。
- **UI**:手动验证桌面侧栏布局、移动端抽屉、明暗切换、各页面不再挤左上角。

---

## 8. 风险与回滚

- CSP nonce 方案若实现有误会导致内联脚本被浏览器拒(抽屉打不开)→ 部署后手动验证移动端;出错可临时移除 `script-src` 限制回退。
- 主题/布局为纯 CSS + 模板改动,回滚 = revert 对应提交,无数据迁移。
- 安全头中间件位于最外层,若误配影响全站 → 本地 `curl -D-` 验证后再上线。
