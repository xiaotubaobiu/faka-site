# 发卡站 — 前端重构(Tailwind + daisyUI + HTMX)+ 订单码展示 设计文档

- 日期: 2026-06-17
- 状态: Draft (待用户复审)
- 相关: 前序 `docs/specs/2026-06-1*-faka-*.md`(初版 / 安全+UI / 用户管理)
- 部署: 本地运行;生产经 Caddy(HTTPS)反代

---

## 1. 背景与目标

当前 UI = Pico.css + 手写 CSS + 侧栏仪表盘,用户反馈"还是丑"。另外购买后的**兑换码只在订单详情页可见**,主"订单"列表不显示,导致用户翻历史看不到码、以为没存(实测码确实在 `order_codes` 表里)。

本次:

1. 前端切换到 **Tailwind CSS + daisyUI + HTMX**,重做全部页面为简洁现代风格,手机/桌面自适应。
2. **修复码展示**:历史订单直接显示每单的码 + 复制按钮。
3. 模板保留 **stdlib `html/template`**;构建引入 **Node 步骤**(Makefile + Dockerfile 多阶段),运行镜像仅 Go 二进制 + 内嵌静态文件。

**非目标**
- 不引入 React/Vue/前后端分离。
- 不引入 `templ`(用 stdlib 模板)。
- 不做在线支付、自助注册。
- 买码流程暂不 HTMX 化(整页提交即可,YAGNI);HTMX 仅用于订单搜索/筛选。

---

## 2. 已确认决策

| 维度 | 决策 |
|---|---|
| 模板 | stdlib `html/template`(不用 templ) |
| 样式 | Tailwind CSS v3 + daisyUI v4(组件 class,稳定且文档多) |
| 交互 | HTMX(`htmx.min.js` 自托管 `//go:embed`)— 订单搜索/筛选局部刷新 |
| 构建 | Node 构建步骤:`Makefile`(`make css`/`make build`/`make run`)+ `Dockerfile` 多阶段;`app.css` 经 `//go:embed`;**运行镜像无 Node** |
| `app.css` | 生成产物,**提交进仓库**(保证 `go build`/`go test` 在任意环境直接可用);`make css` 重新生成 |
| 布局 | daisyUI 顶部 `navbar` + 居中 `container` + 卡片;移动端 `drawer` 抽屉 |
| 主题 | daisyUI 主题 **emerald**(翠绿);提供明暗切换 toggle(存 localStorage) |
| 码展示 | 历史订单卡片直接显示码 + 复制;码多则折叠 |

---

## 3. 构建工具链

- **`package.json`**:`devDependencies` = `tailwindcss@3`、`daisyui@4`。
- **`tailwind.config.js`**:
  - `content`:扫描 `./internal/web/templates/*.html` 与 `./internal/web/**/*.go`(提取 class)。
  - `plugins: [require("daisyui")]`。
  - `daisyui.themes: ["emerald", "dark"]`(或 `light`/`dark`,取翠绿为主)。
- **`src/input.css`**:
  ```css
  @tailwind base;
  @tailwind components;
  @tailwind utilities;
  ```
- **`Makefile`**:
  ```make
  css:
  	npx tailwindcss -i src/input.css -o internal/web/static/app.css --minify
  build: css
  	go build -o faka-site .
  run: build
  	FAKA_DB=./data/faka.db FAKA_LISTEN=:8090 SESSION_SECRET=t COOKIE_SECURE=false ./faka-site
  dev: css
  	go run .
  ```
- **`Dockerfile`**(多阶段):
  - stage `css`(node:alpine):拷 `package.json`、`tailwind.config.js`、`src/input.css`、模板/go → `npm ci` → `npx tailwindcss -i src/input.css -o /out/app.css --minify`。
  - stage `build`(golang):拷源码 + 上阶段产出的 `app.css` 放到 `internal/web/static/app.css` → `CGO_ENABLED=0 go build`。
  - stage `runtime`(alpine):仅拷 `faka-site` 二进制(静态文件已 `//go:embed`)。**无 Node**。
- **`.gitignore`** 增补:`node_modules/`。
- **移除**:`internal/web/static/pico.min.css`;旧 `style.css` 手写内容被 `app.css` 取代(layout 不再引用两者)。
- **新增提交**:`internal/web/static/app.css`(生成产物)、`internal/web/static/htmx.min.js`(从 jsdelivr 下载的 v2 最新版)。

---

## 4. 布局与组件(daisyUI)

`layout.html` 重写:

- `<html data-theme="emerald">`(JS 可切 `dark`)。
- `<head>`:`<link href="/static/app.css">`、`<script src="/static/htmx.min.js">`(defer)、viewport。
- body:
  - **桌面**:`navbar`(品牌 + `menu menu-horizontal`[概览/买码/订单] + 余额 `badge` + `dropdown dropdown-end`[邮箱 + 角色徽章 + 退出 + 明暗 toggle])。
  - **移动**:`drawer`(汉堡按钮 → 抽屉内放导航 + 用户区);`lg:` 以上用顶部 navbar,`max-lg:` 用抽屉(daisyUI 响应式)。
  - `<main class="container mx-auto px-4 py-6 max-w-3xl">{{template "content" .}}</main>`。
  - 末尾带 nonce 的 `<script>`:抽屉开关 + 复制按钮绑定 + 明暗切换存 localStorage(沿用 CSP nonce 机制,**无 inline onclick**)。
- 各页面用 daisyUI 组件:
  - dashboard:`stats`(余额/总订单/近30天消费;admin 加 用户数/平台余额)+ 大「购买兑换码」`btn` + 最近订单卡片。
  - buy:`input input-bordered`、`btn btn-primary`;结果区显示码 + 复制。
  - orders:每订单一张 `card`(见 §5)。
  - order 详情、login、forgot、admin_*:对应 daisyUI 表单/卡片/表格组件。

---

## 5. 订单码展示(问题1 修复)

- **store 新增** `CodesForOrders(ctx, orderIDs []int64) (map[int64][]string, error)`:一次查多订单的码,避免 N+1。
- **orders handler**:取订单列表 → 收集 id → `CodesForOrders` → 把 `map[orderID]->codes` 传入模板。
- **orders.html**:每订单一张 daisyUI `card`:
  - 头部行:`#ID`、数量、每码金额(`usd`)、状态 `badge`(`completed`=success / `partial`=warning / `failed`=error)。
  - 码区:每码一个 `kbd`/mono 文本 + 旁边单码复制按钮;整单「复制全部」按钮。
  - 码 > 5:`<details>` 折叠,默认显示前 3 个 + "展开全部(N)"。
- **复制**:按钮带 `data-copy="<code>"` 属性,nonce `<script>` 绑定 click → `navigator.clipboard.writeText`,点击后短暂提示"已复制"。
- **buy.html**:购买成功结果同样展示码 + 复制按钮。

---

## 6. HTMX 用法(订单搜索/筛选)

- `orders.html` 顶部搜索框:
  ```html
  <input name="q" class="input input-bordered input-sm"
         hx-get="/orders" hx-target="#order-list"
         hx-trigger="input changed delay:300ms" hx-include="this"
         placeholder="搜索订单号 / 状态…">
  ```
- orders handler 判断 `r.Header.Get("HX-Request") == "true"`:
  - 是 → 只渲染列表片段 `orders_list.html`(不含 layout)。
  - 否 → 渲染整页(含搜索框 + 片段)。
- 搜索过滤:`q` 模糊匹配订单 id 或状态。**新增 store 方法** `OrdersByUserFiltered(ctx, userID int64, q string) ([]Order, error)`:`q` 为空时等价于 `OrdersByUser`;非空时 `WHERE user_id=? AND (CAST(id AS TEXT) LIKE ? OR status LIKE ?)`,参数化(`%q%`)。
- **抽出 `orders_list.html` partial**(仅卡片列表),整页与 HTMX 共用。
- 复制按钮在 partial 内,HTMX swap 后需重新绑定 → nonce 脚本用事件委托(`document.addEventListener('click', ...)` 检查 `data-copy`),swap 后仍生效。

---

## 7. 文件改动清单

| 文件 | 动作 |
|---|---|
| `package.json`、`tailwind.config.js`、`src/input.css` | 新增 |
| `Makefile` | 新增 |
| `Dockerfile` | 重写为多阶段 |
| `.gitignore` | 增 `node_modules/` |
| `internal/web/static/app.css` | 新增(生成,提交) |
| `internal/web/static/htmx.min.js` | 新增(下载,提交) |
| `internal/web/static/pico.min.css` | 删除 |
| `internal/web/render.go` | embed `app.css`+`htmx.min.js`;移除 pico/style 引用;注册 `orders_list.html` 片段 |
| `internal/web/templates/layout.html` | 重写(daisyUI navbar/drawer/nonce 脚本含复制+抽屉+主题) |
| `internal/web/templates/*.html`(其余) | 重写为 daisyUI class |
| `internal/web/templates/orders_list.html` | 新增(订单卡片 partial) |
| `internal/web/templates/orders.html` | 重写(搜索框 + 引用 partial) |
| `internal/store/orders.go` | 新增 `CodesForOrders`(批量取码)、`OrdersByUserFiltered`(按 q 过滤) |
| `internal/web/user.go` | orders handler:支持 HTMX 片段 + 传 codes + q 过滤 |

store 层订单码已有 `OrderCodes`(单订单)与新增的 `CodesForOrders`(批量)。

---

## 8. 测试

- `CodesForOrders`:表驱动测试(多订单、多码、空订单)。
- orders handler:HTMX header → 片段渲染;无 header → 整页;codes map 正确注入。
- 端到端(临时库):购买 → 历史订单卡片见码 + 复制(剪贴板);搜索 `q` 局部刷新;桌面 navbar / 移动抽屉;明暗切换;CSP 无违规(复制按钮经 nonce 事件委托,无 inline onclick)。

---

## 9. 风险与注意

- **Node 构建依赖**:本地与 CI 需 Node。`make css`/Dockerfile 包好;`app.css` 提交进仓 → 即使无 Node 也能 `go build`/`go test`(只是改 class 后需手动 `make css` 更新并提交,文档注明)。
- **CSP 与 HTMX**:HTMX 自身是 `<script src>`(同源,`script-src 'self'` 放行);复制/抽屉/主题交互走 nonce 脚本事件委托,HTMX swap 后仍生效;**禁止任何 inline `onclick`**。
- **app.css 体积**:Tailwind 按需 purge,生产体积小(几十 KB);`//go:embed` 内嵌,离线可用。
- **daisyUI 主题**:翠绿 `emerald` 为主;明暗切换由前端 `data-theme` + localStorage 控制,后端无感。
