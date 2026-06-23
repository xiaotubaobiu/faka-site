# fakasite 前端视觉重设计

**日期**: 2026-06-23
**状态**: 设计确认，待实现
**范围**: 全站 17 个模板 + epay 收款页的视觉重设计

## 背景与目标

fakasite 当前前端基于 DaisyUI + Tailwind + HTMX 的 SSR 架构，功能完整但视觉存在以下问题：

- **图标不统一**: 全站使用 emoji（🎫💰📊📦👤⚙️），跨平台（Windows/macOS/Android）渲染不一致，无法统一线宽与颜色，显得拼凑、不精致
- **配色"模板感"**: 使用 DaisyUI 默认 emerald 主题，缺乏品牌识别度，像未定制的脚手架
- **细节缺失**: 缺少统一的状态色系统、按钮交互态、阴影层次、间距 token，各页面细节不一致
- **暗色主题不配套**: 现有暗色是 DaisyUI 默认，与目标暖橙风格不搭

**目标**: 在不改变 SSR 架构（Go template + HTMX）的前提下，建立一套完整的暖色视觉系统并应用到全站，使产品观感统一、专业、细节到位。

## 已确认决策

| 决策项 | 选择 | 理由 |
|--------|------|------|
| 视觉方向 | **暖色亲和** | 亲切友好，偏"产品"而非"企业"，适合发卡站 |
| 主色调 | **琥珀橙 (Amber/Orange)** | 明亮活泼，社区/电商常用，亲和力强 |
| 图标库 | **Lucide** | 统一 1.75px 线宽，跨平台一致，现代简洁 |
| 主题 | **明 + 暗双主题** | 暗色版用深棕背景 + 暖橙强调色配套 |
| 移动端 | **全面适配** | 所有页面精细移动适配，收款页手机可扫码 |
| 重做范围 | **全部页面** | 17 个模板 + epay 收款页全做，保证全站一致 |

## 视觉系统

### 1. 色彩系统

#### 明色主题（Light）
```
背景层级:
  bg-base (页面):    #FFFBF5  (amber-50 偏暖白)
  bg-surface (卡片): #FFFFFF
  bg-subtle (次级):  #FFF7ED  (orange-50)
  border:            #FED7AA  (amber-200)

文字:
  text-strong:  #7C2D12  (orange-900)
  text-base:    #9A3412  (orange-800)
  text-muted:   #A16207  (yellow-700)
  text-faint:   #B45309  (amber-700)

主色 (Primary):
  gradient:  linear-gradient(135deg, #F59E0B → #EA580C)  (amber-500 → orange-600)
  solid:     #EA580C  (orange-600, 用于强调)
  soft:      #FFF7ED  (orange-50, 用于背景)
```

#### 暗色主题（Dark）
```
背景层级:
  bg-base (页面):    #1C1410  (暖色调深棕,非纯黑)
  bg-surface (卡片): #2A1F18  (深棕卡片)
  bg-subtle (次级):  #3D2E22
  border:            #4A382A

文字:
  text-strong:  #FEF3C7  (amber-100)
  text-base:    #FDE68A  (amber-200)
  text-muted:   #FCD34D  (amber-300)
  text-faint:   #D97706  (amber-600)

主色 (保持暖橙但稍亮以适配深背景):
  gradient:  linear-gradient(135deg, #FBBF24 → #F97316)  (amber-400 → orange-500)
  solid:     #F97316  (orange-500)
  glow:      0 0 16px rgba(251,191,36,0.3)  (轻微辉光增强可见性)
```

### 2. 语义状态色（明暗通用语义，色值随主题切换）

| 语义 | 明色 (bg/text) | 暗色 (bg/text) | 用途 |
|------|---------------|---------------|------|
| **成功 success** | `#DCFCE7` / `#15803D` | `#052E16` / `#86EFAC` | 订单完成、充值到账 |
| **处理中 warning** | `#FEF3C7` / `#A16207` | `#422006` / `#FCD34D` | 订单生成中、待支付 |
| **失败 error** | `#FEE2E2` / `#B91C1C` | `#450A0A` / `#FCA5A5` | 购买失败、余额不足 |
| **待处理 neutral** | `#F1F5F9` / `#475569` | `#1E293B` / `#CBD5E1` | 默认、未激活 |

实现为 Tailwind 工具类映射，在 input.css 用 CSS 变量按 `data-theme` 切换。

### 3. 圆角 token

| 用途 | 值 | 示例 |
|------|-----|------|
| 卡片 / 大容器 | `16px` (rounded-2xl) | 统计卡、表单卡 |
| 按钮 | `12px` (rounded-xl) | 主按钮、步进器 |
| 输入框 | `10px` (rounded-lg) | 文本输入 |
| 徽章 / 标签 | `999px` (rounded-full) | 状态标签、余额徽章 |
| 小图标按钮 | `8px` (rounded-lg) | 复制按钮、关闭按钮 |

### 4. 阴影层次（暖色调，非默认黑）

阴影颜色用 `rgba(234,88,12,*)` (orange-600 基底) 替代默认黑色，与暖色主题协调：

| 层级 | 值 | 用途 |
|------|-----|------|
| `shadow-sm` | `0 1px 2px rgba(245,158,11,0.06)` | 静态卡片 |
| `shadow-md` | `0 4px 12px rgba(234,88,12,0.10)` | 卡片 hover |
| `shadow-lg` | `0 12px 32px rgba(234,88,12,0.18)` | 弹窗、下拉 |
| `shadow-cta` | `0 4px 12px rgba(234,88,12,0.25)` | 主 CTA 按钮常驻 |

### 5. 字号层级（Inter / system-ui 字体栈）

| 角色 | 字号/字重 | 用途 |
|------|----------|------|
| stat-value | `20px / 700` | 统计卡大数字（余额） |
| h1 | `15px / 700` | 页面标题 |
| h3 | `13px / 600` | 区块标题 |
| body | `12px / 400` | 正文 |
| caption | `10px / 500 / uppercase / letter-spacing 0.5px` | 标签、时间戳 |

### 6. 间距 token（4px 基准）

```
space-1: 4px
space-2: 8px
space-3: 12px
space-4: 16px
space-5: 20px
space-6: 24px
```

页边距：桌面 `24px` / 平板 `16px` / 手机 `12px`。

### 7. 响应式断点

```
sm: 640px   (大手机横屏)
md: 768px   (平板竖屏)
lg: 1024px  (平板横屏/小桌面，侧栏在此切换显示)
xl: 1280px  (桌面)
```

**移动端规则**:
- `< lg`: 侧栏折叠为抽屉 drawer（已存在，需重做样式）
- `< md`: 统计卡从横向网格切换为纵向堆叠
- `< sm`: 表单单列、按钮全宽、字号适度缩小
- 收款页：二维码 + 金额居中、字号放大，手机可扫码

## 图标系统

### Lucide 集成方案

**不引入 Lucide 全量库**（1200+ 图标，体积大），采用**按需本地托管**：

1. 把用到的图标（约 25-30 个）的 SVG 内联进 Go template，或
2. 建一个 `static/icons.js`（雪碧图 sprite，`<symbol>` 定义 + `<use>` 引用）

**推荐方案 2**（sprite）：所有图标定义一次，全站 `<use href="#icon-x">` 引用，缓存友好、体积小（~15KB）。

### 图标统一规范

- **线宽**: `stroke-width="1.75"`
- **端点**: `stroke-linecap="round" stroke-linejoin="round"`
- **尺寸**: 默认 `18px`，导航 `20px`，统计卡 `16px`，按钮内 `16px`
- **颜色**: 用 `currentColor`，继承父元素文字色

### 图标映射表（替换现有 emoji）

| 当前 emoji | Lucide 图标 | 用途 |
|-----------|------------|------|
| 🎫 | `ticket` | Logo、发卡站品牌 |
| 📊 | `layout-dashboard` | 概览 |
| 🛒 | `shopping-cart` | 买码 |
| 💰 | `wallet` | 充值、余额 |
| 📦 | `package` | 订单 |
| 👤 | `users` | 用户管理 |
| ➕ | `user-plus` | 建账户 |
| ⚙️ | `settings` | 配置 |
| 🏪 | `store` | 商户管理 |
| 📖 | `book-open` | 支付文档 |
| 🌓 | `sun` / `moon` | 主题切换 |
| ☰ | `menu` | 移动端菜单 |
| ✕ | `x` | 关闭 |
| ✓ | `check` | 复制成功、勾选 |
| 🔽 | `chevron-down` | 下拉 |

## 按钮交互态规范

| 状态 | 视觉变化 | 实现 |
|------|---------|------|
| **Default** | 渐变 amber→orange + shadow-cta | 基础态 |
| **Hover** | 渐变深 10%（amber-600→orange-700）+ shadow 收紧 | `:hover` |
| **Active** | 渐变深 20% + 3px orange-200 ring | `:active` |
| **Disabled** | 灰底 `#F3F4F6` + 灰字 `#9CA3AF`，无阴影 | `[disabled]` |
| **Loading** | 内容替换为 spinner + "正在..."文字，按钮禁用 | JS 切换 class |

按钮变体：
- `btn-primary`: 暖橙渐变（主 CTA：购买、充值）
- `btn-secondary`: 白底 + orange 边框（次要：取消）
- `btn-ghost`: 透明 + hover 出 soft 背景（导航、工具）
- `btn-danger`: red-600 实色（删除、危险操作）

## 架构与实现策略

### 不变的部分（保护现有投资）
- **后端**: Go 路由、HTMX 交互、所有业务逻辑不动
- **SSR 模式**: 继续用 Go template 渲染
- **数据流**: 现有的 template context（`.User`/`.Data`/`.CSRF`/`.Nonce`）结构不变
- **CSP nonce**: 保留现有 nonce 机制（已验证有效）

### 改动的部分
1. **`src/input.css`**: 从纯 Tailwind 指令扩展为含 CSS 变量定义（明/暗主题色）、Lucide 配置、自定义工具类
2. **`tailwind.config.js`**: 配置自定义颜色 token、字体、阴影
3. **新增 `internal/web/static/icons.js`**: Lucide sprite
4. **`internal/web/render.go`**: embed 行加入 `static/icons.js`
5. **`internal/web/templates/layout.html`**: 重做布局（侧栏/header/抽屉）、引入 icons.js、替换 emoji 为 `<use>`
6. **17 个页面模板**: 按 token 系统重做，每个页面都要符合视觉规范
7. **`internal/epay/handler.go` 收款页模板** (`payPageTpl` 常量): 重做（含已修复的本地 qrcode.min.js 引用）。注意：这是 epay 网关对外部商户的收款页，与上面的 `recharge_pay.html`（fakasite 用户站内充值流程页）是两个不同的页面，都要重做

### 关键文件改动清单

```
src/input.css                          [重写] 主题变量 + 工具类
tailwind.config.js                     [改] 颜色/字体/阴影配置
internal/web/static/icons.js           [新增] Lucide sprite (~15KB)
internal/web/render.go                 [改] embed 加 icons.js
internal/web/templates/layout.html     [重写] 全局布局 + 主题切换
internal/web/templates/dashboard.html  [重写]
internal/web/templates/buy.html        [重写]
internal/web/templates/recharge.html   [重写]
internal/web/templates/recharge_pay.html [重写]
internal/web/templates/orders.html     [重写]
internal/web/templates/orders_list.html [重写]
internal/web/templates/order.html      [重写]
internal/web/templates/login.html      [重写]
internal/web/templates/forgot.html     [重写]
internal/web/templates/admin_users.html    [重写]
internal/web/templates/admin_create.html   [重写]
internal/web/templates/admin_balance.html  [重写]
internal/web/templates/admin_config.html   [重写]
internal/web/templates/admin_merchants.html [重写]
internal/web/templates/admin_reset.html    [重写]
internal/web/templates/admin_docs.html     [重写]
internal/epay/handler.go (payPageTpl)  [改] 收款页模板重做
```

## 数据流

无变化。所有模板仍从 Go handler 接收相同的 context 结构。视觉重设计纯粹是模板层 + CSS 层的工作，不触碰业务逻辑。

```
Go handler → template context (不变) → HTML 模板 (重做) → 浏览器
                                      ↑ CSS/icons (重做)
```

## 错误处理与状态

### 表单错误
- 错误提示用 `alert-error`（red 状态色）统一样式
- 字段级错误：输入框边框变 red-500 + 下方红字提示
- HTMX 局部错误：保持现有错误返回逻辑，仅改样式

### 空状态
- 订单列表为空：显示带 `package` 图标的空状态插画 + 引导文案
- 搜索无结果：显示 `search-x` 图标 + "未找到结果"

### 加载态
- HTMX 请求中：被请求区域显示 spinner（Lucide `loader-2` 旋转）
- 按钮提交：按钮进入 loading 态（见按钮规范）

## 测试策略

### 视觉回归
- 每个 token（色/字/间距/圆角/阴影）有对应的渲染验证
- 明/暗主题都要验证（每个页面截图两版）

### 现有测试保护
- `internal/web/*_test.go` 已有的测试（headers/middleware/recharge 等）**不能破坏**
- 特别是 CSP nonce 相关测试（headers_test.go）必须继续通过
- 跑 `go test ./...` 确认无回归

### 人工验证清单
- [ ] 明色主题全站截图
- [ ] 暗色主题全站截图
- [ ] 移动端（375px / 768px）全站截图
- [ ] 收款页二维码在明/暗主题下都正常渲染
- [ ] 主题切换持久化（localStorage）正常
- [ ] 所有 emoji 已替换为 Lucide，无遗漏
- [ ] 状态色（成功/失败/处理中）在各页面一致
- [ ] 现有功能（购买/充值/登录）端到端无回归

## 风险与回滚

### 风险
1. **Tailwind 配置改动影响现有样式**: 通过保留 DaisyUI 兼容类、渐进替换降低风险
2. **CSP 与新 JS**: icons.js 是内联 SVG，需确认 CSP `script-src` 或 `default-src 'self'` 允许
3. **暗色主题色值对比度**: 需验证 WCAG AA 对比度（文字 vs 背景）

### 回滚
- 所有改动在 git 分支进行
- 每个模板改动独立 commit，可按文件回滚
- CSS/icons 改动是增量，回滚只需还原 input.css + 删 icons.js

## 非目标（明确排除）

- **不改后端逻辑**: 路由、handler、store、业务规则不动
- **不改 HTMX 交互模式**: 不引入 React/Vue，保持 SSR + HTMX
- **不做安全加固**: 那是独立的下一个 spec（用户已确认先做前端）
- **不做支付正式环境切换**: fakasite 沙箱→正式是独立任务
- **不重做 newapi 侧**: 本次只改 fakasite（pay.000328.xyz），南京 newapi 不在范围
- **不加新功能**: 纯视觉重设计，不新增页面或功能

## 成功标准

1. 全站 17 个模板 + 收款页统一应用暖橙视觉系统，无 emoji 残留
2. 明/暗双主题都完整、对比度达 WCAG AA
3. 移动端（375px）所有页面可用且美观
4. 所有现有功能端到端无回归（`go test ./...` 通过 + 人工验证）
5. 视觉细节符合 spec 中的所有 token 规范
