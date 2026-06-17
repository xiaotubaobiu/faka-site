# 发卡站 — 用户管理流程重构 设计文档

- 日期: 2026-06-17
- 状态: Draft (待用户复审)
- 相关: `docs/specs/2026-06-15-faka-site-design.md`(初版)、`docs/specs/2026-06-17-faka-security-ui-design.md`(安全+UI)
- 部署: 本地运行,生产经 Caddy(HTTPS)反代

---

## 1. 背景与目标

当前建号流程:`POST /admin/create` 只收邮箱 → 系统随机生成密码 → 依赖 SMTP 把密码发到邮箱。问题:**SMTP 未配置时,密码只能显示在管理员屏幕上(易丢),用户实际"数据库里有、但拿不到密码 = 不可用"**。同时 SMTP 既用于建号又用于重置,职责混乱。

本次重构:

1. **建号**改为管理员直接填**邮箱 + 密码**,不再依赖 SMTP。
2. **SMTP 唯一用途** = 用户自助"忘记密码"重置(直接邮件新随机临时密码,方案 A)。
3. **管理员重置密码**改为管理员在表单里直接设新密码(不发邮件)。
4. **强化权限分层**可见性(侧栏角色徽章);确认新用户恒为普通用户。

**非目标(明确不做)**
- 不做邮箱验证 / 不做自助注册。
- 不做"修改密码"页(用户自助改密;YAGNI,可后续再加)。
- 不引入重置 token 表(已选方案 A:直接发临时密码,无 token)。

---

## 2. 已确认决策

| 维度 | 决策 |
|---|---|
| 建号 | 管理员填**邮箱 + 密码 + 确认密码**,直接建号,**不发邮件** |
| 新用户角色 | 固定 `user`(恒为普通用户,无越权) |
| 用户自助重置 | **方案 A**:登录页"忘记密码"→ 输邮箱 → 邮件发新随机临时密码 |
| 管理员重置 | 管理员在表单里**直接设新密码**,不发邮件 |
| SMTP 角色 | **仅** `/forgot` 自助重置使用;建号与管理员重置均不调用 mailer |
| 权限分层 | 已有 `role` + `requireAdmin`;加角色徽章;普通用户仪表盘不显示平台统计(已实现) |

---

## 3. 流程设计

### 3.1 建号(管理员)

- `GET /admin/create`:表单字段 = 邮箱 + 密码 + 确认密码。
- `POST /admin/create`:校验邮箱非空、密码长度 ≥ 6、密码 == 确认密码;`CreateUser(role="user", hash)`;成功 → 重定向 `/admin/users`;重复邮箱 → 友好提示"该邮箱可能已存在"(不泄露 DB 错误,沿用上次脱敏)。
- **不调用 mailer**。删除现有 `randPassword()` + `m.Send` 逻辑。

### 3.2 管理员重置密码

- `GET /admin/reset?id=X`:表单(新密码 + 确认密码),标题"重置密码"。
- `POST /admin/reset`:校验密码长度 ≥ 6、== 确认;`SetUserPassword(id, hash)`;成功 → 重定向 `/admin/users`。
- **不调用 mailer**。
- `admin_users.html` 中每行的"重置密码"按钮改为链接到 `GET /admin/reset?id={{.ID}}`。

### 3.3 用户自助重置(`/forgot`,公开)

- `GET /forgot`:表单(邮箱)。
- `POST /forgot`:
  1. `throttle.Allow(clientIP(r) + "|" + email)`(复用登录同款限流),超限 → "尝试过多,稍后再试"。
  2. `UserByEmail(email)`:
     - **存在** → 生成随机临时密码(`randPassword()`),`SetUserPassword`,调 `mailer().Send(email, "发卡站密码重置", "新密码:"+pw)`。
     - **不存在** → 不做任何写操作。
  3. 统一回显**"如该邮箱已注册,新密码已发送至该邮箱"**(防用户枚举);仅当 SMTP 未配置时回显"SMTP 未配置,请联系管理员"。
- 公开路由(与 `/login` 同级,挂在主 `mux`,预认证,POST 无需 CSRF —— `csrfCheck` 对无 session 的请求放行)。

### 3.4 登录页

- `login.html` 表单下方加链接 `<a href="/forgot">忘记密码?</a>`。

---

## 4. 权限分层(确认 + 小强化)

- `role`: `admin` / `user`。`requireAdmin` 已隔离 `/admin/*`;普通用户侧栏不渲染管理组(已有)。
- 新建用户恒为 `user`(建号流程强制)。
- **侧栏用户区加角色徽章**:管理员显示「管理员」,普通用户显示「普通用户」(模板 `{{if eq .User.Role "admin"}}`)。
- 普通用户仪表盘不显示平台级统计卡(已通过 `{{if .Data.UserCount}}` 实现,无需改)。

---

## 5. 安全

- `/forgot` 加 `clientIP` 限流(复用),防暴力/刷信;统一文案防用户枚举。
- 建号 / 管理员重置:密码长度 ≥ 6、两次输入一致才入库。
- 临时密码由 `randPassword()`(`crypto/rand` 16 hex)生成。
- 重复邮箱、重置失败等均走友好文案 + 服务端日志,不回显内部错误(沿用已建立的脱敏约定)。

---

## 6. 文件改动清单

| 文件 | 改动 |
|---|---|
| `internal/web/admin.go` | 重写 `postCreate`(邮箱+密码+确认,无 mailer);新增 `getReset`/`postReset`(设新密码,无 mailer);移除 create/reset 的邮件逻辑 |
| `internal/web/auth.go` | 新增 `getForgot`/`postForgot`(自助重置 + 限流 + 防枚举) |
| `internal/web/server.go` | 路由:`GET/POST /forgot`(公开,主 mux)、`GET/POST /admin/reset`(admin mux) |
| `templates/admin_create.html` | 改为 邮箱 + 密码 + 确认密码 |
| `templates/admin_reset.html`(新增) | 新密码 + 确认密码 表单 |
| `templates/admin_users.html` | "重置密码" 按钮改链接到 `/admin/reset?id=X` |
| `templates/login.html` | 加"忘记密码?"链接 |
| `templates/forgot.html`(新增) | 邮箱表单 |
| `templates/layout.html` | 侧栏用户区加角色徽章 |

`store` 层无需新增方法(`CreateUser`/`SetUserPassword`/`UserByEmail` 已存在)。`randPassword()` 保留(`crypto/rand`,供 `/forgot` 用)。

---

## 7. 测试

- `postCreate`:密码 <6 或两次不一致 → 友好错误不建号;合法 → 建号成功且 `role=user`、未发邮件;重复邮箱 → 友好提示。
- `postReset`:合法 → 密码被更新;不一致 → 不更新。
- `postForgot`:存在用户 → 生成临时密码、落库、邮件被调用(用假 mailer 注入断言);不存在用户 → 不落库、回显统一文案;超限 → 拦截。
- 防枚举:存在/不存在邮箱回显文案一致。
- 端到端:管理员建号 → 该用户用设定密码登录成功;用户 /forgot → 收到临时密码 → 登录成功。
