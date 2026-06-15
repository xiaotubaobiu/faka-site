# 发卡站 (Faka Site) — 设计文档

- 日期: 2026-06-15
- 状态: Draft (待用户复审)
- 相关: NewAPI 生产 (52.220.94.27),`POST /api/redemption/` 发码接口
- 部署目标: 52.220.94.27 (与 new-api 同机)

---

## 1. 背景与目标

NewAPI 原生支持「兑换码」(redemption code),且暴露了管理员 API 可按指定 `quota` / `count` 批量生成 32 位兑换码。本项目建设一个**独立的轻量发卡站**,让受控用户(管理员创建)用账户余额「购买」兑换码,码生成后由用户自行在 NewAPI 兑换。

**核心目标**
- 用户登录后可选择「买几个码 / 每个码多少额度」,从余额扣费,实时生成 NewAPI 兑换码并展示。
- 多用户、独立账户体系,**不开放注册**,账户与余额均由管理员维护。
- 尽量轻量:单二进制 + SQLite,一个容器。

**非目标(明确不做)**
- 不做在线支付 / 不接 epay-gateway(余额仅管理员手动加)。
- 不做 NewAPI 用户 SSO / 不自动把码兑换进用户的 NewAPI 账户(码交给用户,自行兑换)。
- 不做用户自助注册、不做套餐/订阅。

---

## 2. 已确认决策

| 维度 | 决策 |
|---|---|
| 架构 | 独立 Go 服务,与 NewAPI 解耦,不动 NewAPI 一行代码 |
| 余额模型 | 仅管理员手动加余额,**无在线支付** |
| 余额面额 | **1:1 = NewAPI quota 单位**(买 1 个 50 万额度码扣 50 万余额) |
| 买码交付 | 站点生成码后**把码字符串交给用户**,用户自行在 NewAPI 兑换 |
| 技术栈 | Go 单二进制 + SQLite(纯 Go 驱动 `modernc.org/sqlite`,无 CGO)+ 服务端渲染 HTML(`html/template`),资源 `//go:embed` 内嵌 |
| 配置方式 | NewAPI 网址 / 管理员系统访问令牌 / 管理员用户 ID 通过**管理员后台填表**配置(存 `config` 表),非环境变量写死 |

---

## 3. 架构

运行形态:单个 Go 二进制,所有模板与静态资源 `//go:embed` 打包 → 真·单文件。SQLite 文件挂载宿主 volume。Docker 容器加入 new-api 所在 docker 网络,内网 `http://new-api:3000` 直连。Nginx 反代子域 `faka.000328.xyz`。

**组件(职责单一,可独立测试):**

| 组件 | 职责 | 依赖 |
|---|---|---|
| `web` | 路由、handler、`html/template` 服务端渲染 | auth, store, newapi, mailer |
| `auth` | 签名 session cookie + bcrypt + CSRF + 登录限流 | store |
| `store` | SQLite 访问层(`database/sql`),迁移、事务 | sqlite |
| `newapi` | NewAPI 发码 HTTP 客户端(批量循环 + 错误映射 + 连接测试) | config |
| `mailer` | SMTP 发信(建号、重置密码) | config |
| `config` | 读写 `config` 表(网址/令牌/用户ID/SMTP) | store |

---

## 4. 数据模型

> 余额以 `balance_ledger` 为准(每次变动记一条不可变流水),`users.balance` 为缓存快照,二者在事务内同步。

```sql
users
  id              INTEGER PK
  email           TEXT UNIQUE NOT NULL        -- 登录号
  password_hash   TEXT NOT NULL               -- bcrypt
  role            TEXT NOT NULL DEFAULT 'user'-- user | admin
  balance         INTEGER NOT NULL DEFAULT 0  -- 单位 = quota (1:1)
  status          INTEGER NOT NULL DEFAULT 1  -- 1=active, 0=disabled
  created_at      INTEGER
  updated_at      INTEGER

orders                            -- 一次"买码" = 一个订单
  id              INTEGER PK
  user_id         INTEGER NOT NULL
  code_count      INTEGER NOT NULL            -- N
  quota_per_code  INTEGER NOT NULL            -- Q
  total_cost      INTEGER NOT NULL            -- = N * Q
  status          TEXT NOT NULL               -- pending|completed|partial|failed
  succeeded_count INTEGER NOT NULL DEFAULT 0
  failed_count    INTEGER NOT NULL DEFAULT 0
  refunded_amount INTEGER NOT NULL DEFAULT 0  -- 失败部分退回的额度
  created_at      INTEGER
  updated_at      INTEGER

order_codes                       -- 每个订单实际生成的码(给用户查看/复制)
  id              INTEGER PK
  order_id        INTEGER NOT NULL
  user_id         INTEGER NOT NULL
  code            TEXT NOT NULL                -- 32 位兑换码
  quota           INTEGER NOT NULL
  created_at      INTEGER

balance_ledger                    -- 不可变流水,审计
  id              INTEGER PK
  user_id         INTEGER NOT NULL
  delta           INTEGER NOT NULL             -- 可为负
  balance_after   INTEGER NOT NULL
  reason          TEXT NOT NULL               -- admin_add|purchase|refund
  admin_id        INTEGER                     -- admin_add 时记录谁加的
  order_id        INTEGER                     -- purchase/refund 时关联订单
  created_at      INTEGER

config                            -- 键值配置(管理员后台填表)
  key             TEXT PK
  value           TEXT
  updated_at      INTEGER
  -- keys: newapi_base_url, newapi_access_token, newapi_admin_user_id,
  --        smtp_host, smtp_port, smtp_user, smtp_pass, smtp_from
```

---

## 5. NewAPI 集成(已核实源码)

### 5.1 鉴权(关键)
NewAPI `/api/redemption/` 走 `middleware.AdminAuth()`,需 **两个** 请求头:

| Header | 值 | 说明 |
|---|---|---|
| `Authorization` | `<access_token>` | 管理员账号**个人设置里生成的「系统访问令牌」**(`users.access_token`,char(32))。**不是** relay 的 `sk-xxx`。`Bearer ` 前缀可选(NewAPI 会 strip)。 |
| `New-Api-User` | `<admin_user_id>` | 管理员的**数字 user id**,必须与令牌属主一致(防 CSRF)。**必填**,缺失或不符返回 401。 |

令牌对应的用户 role 必须 ≥ admin(`RoleAdminUser`),否则 `AdminAuth` 拒绝(权限不足)。

### 5.2 发码请求
```
POST {newapi_base_url}/api/redemption/
Headers:
  Authorization: <access_token>
  New-Api-User:  <admin_user_id>
Body (JSON):
  { "name": "fk-<orderid>",   // 1~20 字符
    "quota": <Q>,             // 每码额度
    "count": <1~100>,         // 单次 ≤100
    "expired_time": <unix|0> }
Response:
  { "success": true, "data": ["<32位码>", ...] }
```
来源:`controller.AddRedemption` (`controller/redemption.go:62`)。

### 5.3 限制与约束(来自源码)
- `count` 单次 **≤100**(超过返回错误)→ N>100 时站点**自动分批**循环调用。
- `name` 长度 1~20 字符 → 用 `fk-<订单号短>`。
- **前置开关**:`AddRedemption` 开头校验 `IsPaymentComplianceConfirmed()`,NewAPI 后台「支付合规」未开启则**直接拒绝**。站点必须把该错误映射为明确提示,让管理员去 NewAPI 开启。

### 5.4 错误映射
| NewAPI 返回 | 站点行为 |
|---|---|
| "payment compliance" 类 | 提示管理员:「请到 NewAPI 后台开启支付合规开关」 |
| 401 / 403 / "access token invalid" | 提示管理员:「令牌无效或非管理员,请检查系统访问令牌与用户 ID」 |
| 5xx / 超时 | 该批按**失败**处理,走退款逻辑(见 6.1) |
| count>100 / name 越界 | 不应发生(站点已校验);兜底提示 |

### 5.5 「测试连接」
配置页按钮:用填写的 网址/令牌/用户ID 调 `GET /api/user/self`(带上述两 header),成功即配置有效;从返回体校验 `role ≥ admin`,非管理员则提示「令牌对应的不是管理员账号」。失败按 5.4 映射提示。

> 备注:`expired_time` 语义——`0` 表示**永不过期**(已核实 `model/redemption.go:26`)。买码流程默认 `expired_time=0`(永不过期);是否开放「按订单设有效期」留作后续可选项,初版不做。

---

## 6. 核心流程

### 6.1 买码流程(user)— hold → settle / refund
> 目标:扣的钱永远 = 实际发出码的钱,不超扣不少扣,全程事务,余额不损坏。

1. 用户填 数量 `N`、每码额度 `Q`。页面实时算 `total = N*Q`,提示余额是否足够。
2. 提交后端事务(`BEGIN IMMEDIATE` 锁该 user 行):
   - 校验 `balance ≥ total`;不足 → 拒绝。
   - `balance -= total`。
   - 建 `order(status=pending, total_cost=total)`。
   - 记 `ledger(delta=-total, reason=purchase, order_id)`。
   - 提交事务。
3. 调 NewAPI 批量发码(N>100 自动分批,每批 ≤100),逐批收集成功码。
4. 结果回写(新事务):
   - **全成功** → `order(completed)`,写 `order_codes`(全部码)。
   - **部分失败**(s 个成功,f 个失败)→ `order(partial)`,`succeeded=s, failed=f`;写成功的 `order_codes`;退款 `refund = f * Q` → `balance += refund`,记 `ledger(delta=+refund, reason=refund, order_id)`,`refunded_amount=refund`。
   - **全失败** → `order(failed)`;全额退款 `balance += total`,记 `ledger(delta=+total, reason=refund)`。
5. 返回订单页:展示生成的码(可复制)、状态、退款明细。

### 6.2 管理员流程
- **建账户(一键)**:管理员填 email → 生成随机强密码 → bcrypt 存储 → 尝试发邮件(邮箱+初始密码)→ 无论邮件成功与否账户都建成;面板**显示一次初始密码**(邮件失败时提示「邮件发送失败,请手动转交」)。用户首登可改密。
- **加余额**:选用户 + 输入额度(正数)→ 事务 `balance += amount` + `ledger(delta=+amount, reason=admin_add, admin_id=<你>)`。
- **配置页**:填写 NewAPI 网址 / 系统访问令牌(掩码)/ 管理员用户 ID + SMTP 配置(host/port/user/pass 掩码/from);保存到 `config` 表;「测试连接」按 5.5。
- **用户管理**:列表(余额、订单数、状态)、禁用/启用、重置密码(生成新随机密码发邮件 + 面板显示)。

---

## 7. 邮件(SMTP)

- SMTP 配置随管理员配置页填,存 `config` 表(pass 字段掩码)。
- 发信时机:① 建账户 ② 重置密码。
- email = 登录号 + 通知 + 找回密码通道;管理员创建即视为已绑定(不做额外邮箱验证)。用户可改密码,改 email 走管理员。
- 邮件发送失败**不阻塞**建账户/重置(见 6.2),密码在管理员面板可见。

---

## 8. 安全

- 密码 bcrypt。
- session:HMAC **签名 cookie**(HttpOnly + Secure + SameSite=Lax),无状态、最轻。
- CSRF:每表单 token(session 一份 + hidden 字段一份,POST 校验)。
- 登录限流:同 IP / 同邮箱每分钟 ≤5 次。
- NewAPI 系统访问令牌与 SMTP 密码:**仅服务端使用**,日志与 UI 掩码(仅露后 4 位)。
- 输入边界:`N ≥ 1`、`Q > 0` 且 `Q ≤ 1e9`、`N*Q` 不溢出 int64;越界直接拒。
- HTTPS 由 Nginx 终止。

---

## 9. 部署

- Docker 容器跑在 52.220.94.27,加入 new-api 所在 docker 网络 → 内网 `http://new-api:3000`。
- SQLite 文件挂载宿主 volume(持久);定时 `sqlite3 .backup` 备份(轻量)。
- Nginx 反代子域 `faka.000328.xyz` → 容器端口;TLS 复用现有证书机制(参考 `docs-site` 的 nginx 配置)。
- **首次启动 bootstrap**(环境变量,只用一次):`ADMIN_EMAIL` + `ADMIN_PASSWORD` 建第一个管理员;`SESSION_SECRET` 用于签名 cookie。其余配置全部后台页填。

---

## 10. 错误处理与幂等

- NewAPI 调用失败 → 6.1 退款逻辑,事务保证余额一致。
- 双击买码:前端点击后禁用按钮 + 后端事务余额校验天然防超扣(第二次余额不足即失败),无需幂等键。
- DB/5xx → 友好错误页,不暴露内部信息。
- 邮件失败 → 不阻塞,降级为面板显示。

---

## 11. 测试

- **单测**:
  - 流水余额计算(`balance_after` 一致性)。
  - 批量切分:`N=350 → [100,100,100,50]`;边界 `N=100 → [100]`、`N=1 → [1]`。
  - 部分失败退款金额(`f*Q`)与全失败全额退款。
  - 输入边界校验。
- **newapi 客户端单测**(mock HTTP):验证两 header、请求体、100 上限循环、错误映射(compliance/401/5xx)、测试连接。
- **store 单测**(in-memory sqlite):迁移、并发扣款不超抽(`BEGIN IMMEDIATE` 隔离)。
- **端到端**(mock NewAPI):建号 → 加余额 → 买码 → 校验码与流水一致。
- **上线前手动冒烟**(真 NewAPI):建号、加 10 万、买 1 个 5 万码 → 确认码出现在 NewAPI 兑换码列表且能兑换;再测 N>100 分批与部分失败退款。

---

## 12. 假设与待确认

1. 项目目录:`/home/lisa/matrix/faka-site`(与 epay-gateway 等同级)。可改。
2. 子域名 `faka.000328.xyz` 与 TLS 证书:需确认是否复用现有泛域名/证书,或新签。
3. NewAPI 内部端口默认 3000(`http://new-api:3000`):需在部署时确认容器名/端口一致。
4. 管理员需先在 NewAPI 管理员账号下**生成「系统访问令牌」**(非 sk- relay 令牌)并记下自己的**数字 user id**,填入配置页。
5. NewAPI 后台「支付合规」开关需开启,否则发码被拒。
6. SMTP:需提供可用的 SMTP 凭据(用于建号/重置密码邮件);如暂无,可先靠面板显示密码运行。
