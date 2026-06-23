# fakasite 前端视觉重设计 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 fakasite 前端从 DaisyUI 默认 emerald 主题 + emoji 图标，重设计为统一的暖橙视觉系统（明暗双主题 + Lucide 图标 + 全套细节 token），应用到全站 17 个模板 + epay 收款页。

**Architecture:** 不改变 SSR 架构（Go template + HTMX），纯模板层 + CSS 层工作。先建立视觉基础设施（CSS 变量、Tailwind 配置、Lucide sprite、布局），再逐页应用。所有改动在 git 分支进行，每个任务独立 commit。

**Tech Stack:** Go template、Tailwind CSS 3、DaisyUI（保留兼容）、HTMX、Lucide 图标（sprite 形式本地托管）

**参考 spec:** `docs/specs/2026-06-23-faka-visual-redesign-design.md`

**远程环境:** 代码在 HK 服务器 `root@103.85.224.229:/opt/faka-site`，本地副本在 `C:\Users\13063\Desktop\python\fakasite`。每个任务改完本地文件后，scp 到 HK 或直接在 HK 上编辑，然后 `docker compose build && up -d` 重新构建。**所有改动前先 `cp file file.bak.$(date +%s)` 备份。**

---

## 文件结构总览

**新增:**
- `internal/web/static/icons.js` — Lucide sprite（~25 个图标的 `<symbol>` 定义）

**重写/大改:**
- `src/input.css` — 主题 CSS 变量 + 工具类
- `tailwind.config.js` — 自定义颜色/字体/阴影
- `internal/web/templates/layout.html` — 全局布局
- 17 个页面模板 — 按规范重做

**小改:**
- `internal/web/render.go` — embed 行加 icons.js
- `internal/epay/handler.go` — `payPageTpl` 收款页模板重做

**不动:**
- 所有 Go 业务逻辑（路由/handler/store/auth/epay 业务）
- HTMX 交互逻辑
- CSP nonce 机制（headers.go）
- 现有测试

---

## Task 1: 建 git 分支 + 确认构建基线

**Files:**
- 无文件改动，环境准备

- [ ] **Step 1: SSH 到 HK，进入项目目录，确认当前能正常构建**

```bash
ssh root@103.85.224.229
cd /opt/faka-site
docker compose build faka 2>&1 | tail -3
```
Expected: `Image faka-site:latest Built`（确认基线干净，当前生产版本能编译）

- [ ] **Step 2: 确认现有测试通过**

```bash
docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine go test ./... 2>&1 | tail -10
```
Expected: 所有包 `ok` 或 `no test files`，无 FAIL。记录通过的测试数作为基线。

- [ ] **Step 3: 创建工作分支**

```bash
cd /opt/faka-site
git checkout -b visual-redesign
```
Expected: `Switched to a new branch 'visual-redesign'`

- [ ] **Step 4: 截图当前状态作为 before 对比**

```bash
# 记录当前生产版本的关键页面快照（用浏览器手动截 dashboard/buy/recharge/login）
# 这些是回归对比基准
echo "请在浏览器截图当前 dashboard/buy/recharge/login 页面存档"
```

---

## Task 2: 配置 Tailwind 自定义 token

**Files:**
- Modify: `tailwind.config.js`

- [ ] **Step 1: 备份**

```bash
cd /opt/faka-site
cp tailwind.config.js tailwind.config.js.bak.$(date +%s)
```

- [ ] **Step 2: 写入新配置**

```bash
cat > tailwind.config.js << 'EOF'
/** @type {import('tailwindcss').Config} */
module.exports = {
  content: ["./internal/web/templates/**/*.html", "./internal/web/**/*.go"],
  theme: {
    extend: {
      fontFamily: {
        sans: ['Inter', '-apple-system', 'BlinkMacSystemFont', 'Segoe UI', 'system-ui', 'sans-serif'],
      },
      colors: {
        // 暖橙主色族（明色）
        brand: {
          50: '#FFFBF5',   // 页面背景（amber-50 偏暖白）
          100: '#FFF7ED',  // 次级背景（orange-50）
          200: '#FED7AA',  // 边框（amber-200）
          300: '#FDBA74',
          400: '#FBBF24',  // 暗色主题主色亮版
          500: '#F59E0B',  // 明色主题渐变起点（amber-500）
          600: '#EA580C',  // 明色主题主色（orange-600）
          700: '#C2410C',  // hover 深 10%
          800: '#9A3412',  // 正文文字（orange-800）
          900: '#7C2D12',  // 强文字（orange-900）
        },
      },
      borderRadius: {
        'xl2': '16px',  // 卡片
      },
      boxShadow: {
        'card-sm': '0 1px 2px rgba(245,158,11,0.06)',
        'card-md': '0 4px 12px rgba(234,88,12,0.10)',
        'card-lg': '0 12px 32px rgba(234,88,12,0.18)',
        'cta': '0 4px 12px rgba(234,88,12,0.25)',
      },
    },
  },
  plugins: [require("daisyui")],
  daisyui: { themes: ["emerald", "dark"], darkTheme: "dark" },
};
EOF
```

- [ ] **Step 3: 验证 Tailwind 能编译（构建镜像）**

```bash
docker compose build faka 2>&1 | tail -3
```
Expected: `Built`，无 CSS 编译错误。

- [ ] **Step 4: 重启确认页面不破**

```bash
docker compose down 2>&1 | tail -1
docker stop faka-site 2>/dev/null; docker rm faka-site 2>/dev/null
docker compose up -d 2>&1 | tail -2
sleep 6
curl -s -o /dev/null -w '%{http_code}' https://pay.000328.xyz/login
```
Expected: 200（页面正常，此时视觉还没变，只是加了 token 定义）

- [ ] **Step 5: Commit**

```bash
git add tailwind.config.js
git commit -m "feat(design): configure Tailwind brand color tokens, font, shadows"
```

---

## Task 3: 写主题 CSS 变量（明/暗双主题）

**Files:**
- Modify: `src/input.css`

- [ ] **Step 1: 备份**

```bash
cp src/input.css src/input.css.bak.$(date +%s)
```

- [ ] **Step 2: 写入主题变量 + 工具类**

```bash
cat > src/input.css << 'EOF'
@tailwind base;
@tailwind components;
@tailwind utilities;

/* ============ 主题变量 ============ */
/* 明色主题（默认，data-theme="emerald" 或无） */
:root,
[data-theme="emerald"] {
  --bg-base: #FFFBF5;
  --bg-surface: #FFFFFF;
  --bg-subtle: #FFF7ED;
  --border-base: #FED7AA;

  --text-strong: #7C2D12;
  --text-base: #9A3412;
  --text-muted: #A16207;
  --text-faint: #B45309;

  --brand-grad-from: #F59E0B;
  --brand-grad-to: #EA580C;
  --brand-solid: #EA580C;

  --success-bg: #DCFCE7;  --success-text: #15803D;
  --warning-bg: #FEF3C7;  --warning-text: #A16207;
  --error-bg: #FEE2E2;    --error-text: #B91C1C;
  --neutral-bg: #F1F5F9;  --neutral-text: #475569;

  --shadow-sm: 0 1px 2px rgba(245,158,11,0.06);
  --shadow-md: 0 4px 12px rgba(234,88,12,0.10);
}

/* 暗色主题 */
[data-theme="dark"] {
  --bg-base: #1C1410;
  --bg-surface: #2A1F18;
  --bg-subtle: #3D2E22;
  --border-base: #4A382A;

  --text-strong: #FEF3C7;
  --text-base: #FDE68A;
  --text-muted: #FCD34D;
  --text-faint: #D97706;

  --brand-grad-from: #FBBF24;
  --brand-grad-to: #F97316;
  --brand-solid: #F97316;

  --success-bg: #052E16;  --success-text: #86EFAC;
  --warning-bg: #422006;  --warning-text: #FCD34D;
  --error-bg: #450A0A;    --error-text: #FCA5A5;
  --neutral-bg: #1E293B;  --neutral-text: #CBD5E1;

  --shadow-sm: 0 1px 2px rgba(0,0,0,0.3);
  --shadow-md: 0 4px 12px rgba(0,0,0,0.4);
}

/* ============ 基础应用 ============ */
body {
  background-color: var(--bg-base);
  color: var(--text-base);
  font-feature-settings: "cv11", "ss01";
}

/* ============ 工具类 ============ */
@layer components {
  /* 主 CTA 按钮：暖橙渐变 */
  .btn-brand {
    background-image: linear-gradient(135deg, var(--brand-grad-from), var(--brand-grad-to));
    color: #FFFFFF;
    border: none;
    box-shadow: var(--shadow-md);
    transition: all 0.15s ease;
  }
  .btn-brand:hover {
    background-image: linear-gradient(135deg, #D97706, #C2410C);
    box-shadow: 0 2px 6px rgba(234,88,12,0.4);
  }
  .btn-brand:active {
    background-image: linear-gradient(135deg, #B45309, #9A3412);
    box-shadow: 0 0 0 3px rgba(234,88,12,0.3);
  }
  .btn-brand:disabled {
    background-image: none;
    background-color: #F3F4F6;
    color: #9CA3AF;
    box-shadow: none;
    cursor: not-allowed;
  }

  /* 卡片 */
  .card-warm {
    background-color: var(--bg-surface);
    border: 1px solid var(--border-base);
    border-radius: 16px;
    box-shadow: var(--shadow-sm);
  }

  /* 状态徽章 */
  .badge-success { background: var(--success-bg); color: var(--success-text); }
  .badge-warning { background: var(--warning-bg); color: var(--warning-text); }
  .badge-error   { background: var(--error-bg);   color: var(--error-text); }
  .badge-neutral { background: var(--neutral-bg); color: var(--neutral-text); }
}
EOF
```

- [ ] **Step 3: 构建 + 重启验证**

```bash
docker compose build faka 2>&1 | tail -3
docker compose down 2>&1 | tail -1
docker stop faka-site 2>/dev/null; docker rm faka-site 2>/dev/null
docker compose up -d 2>&1 | tail -2
sleep 6
curl -s -o /dev/null -w '%{http_code}' https://pay.000328.xyz/login
```
Expected: 200

- [ ] **Step 4: Commit**

```bash
git add src/input.css
git commit -m "feat(design): add light/dark theme CSS variables and warm utility classes"
```

---

## Task 4: 创建 Lucide 图标 sprite

**Files:**
- Create: `internal/web/static/icons.js`

- [ ] **Step 1: 创建 icons.js（SVG sprite with `<symbol>`）**

```bash
cat > internal/web/static/icons.js << 'EOF'
// Lucide icon sprite (locally hosted, no external CDN).
// Usage: <svg class="icon"><use href="#icon-dashboard"></use></svg>
// Inject this script once (in layout <head>); it defines all <symbol>s in a hidden SVG.
(function(){
  var svg = '<svg xmlns="http://www.w3.org/2000/svg" style="display:none">';
  // helper: each icon is 24x24 viewBox, stroke 1.75, round caps
  var icons = {
    'ticket': '<path d="M2 9a3 3 0 0 1 0 6v2a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2v-2a3 3 0 0 1 0-6V7a2 2 0 0 0-2-2H4a2 2 0 0 0-2 2Z"/><path d="M13 5v2"/><path d="M13 17v2"/><path d="M13 11v2"/>',
    'dashboard': '<rect width="7" height="9" x="3" y="3" rx="1"/><rect width="7" height="5" x="14" y="3" rx="1"/><rect width="7" height="9" x="14" y="12" rx="1"/><rect width="7" height="5" x="3" y="16" rx="1"/>',
    'cart': '<circle cx="8" cy="21" r="1"/><circle cx="19" cy="21" r="1"/><path d="M2.05 2.05h2l2.66 12.42a2 2 0 0 0 2 1.58h9.78a2 2 0 0 0 1.95-1.57l1.65-7.43H5.12"/>',
    'wallet': '<path d="M19 7V4a1 1 0 0 0-1-1H5a2 2 0 0 0 0 4h15a1 1 0 0 1 1 1v4h-3a2 2 0 0 0 0 4h3a1 1 0 0 0 1-1v-2a1 1 0 0 0-1-1"/><path d="M3 5v14a2 2 0 0 0 2 2h15a1 1 0 0 0 1-1v-4"/>',
    'package': '<path d="m7.5 4.27 9 5.15"/><path d="M21 8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16Z"/><path d="m3.3 7 8.7 5 8.7-5"/><path d="M12 22V12"/>',
    'users': '<path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2"/><circle cx="9" cy="7" r="4"/><path d="M22 21v-2a4 4 0 0 0-3-3.87"/><path d="M16 3.13a4 4 0 0 1 0 7.75"/>',
    'user-plus': '<path d="M19 21v-2a4 4 0 0 0-4-4H9a4 4 0 0 0-4 4v2"/><circle cx="12" cy="7" r="4"/><line x1="22" x2="18" y1="11" y2="11"/><line x1="20" x2="20" y1="9" y2="13"/>',
    'settings': '<path d="M12.22 2h-.44a2 2 0 0 0-2 2v.18a2 2 0 0 1-1 1.73l-.43.25a2 2 0 0 1-2 0l-.15-.08a2 2 0 0 0-2.73.73l-.22.38a2 2 0 0 0 .73 2.73l.15.1a2 2 0 0 1 1 1.72v.51a2 2 0 0 1-1 1.74l-.15.09a2 2 0 0 0-.73 2.73l.22.38a2 2 0 0 0 2.73.73l.15-.08a2 2 0 0 1 2 0l.43.25a2 2 0 0 1 1 1.73V20a2 2 0 0 0 2 2h.44a2 2 0 0 0 2-2v-.18a2 2 0 0 1 1-1.73l.43-.25a2 2 0 0 1 2 0l.15.08a2 2 0 0 0 2.73-.73l.22-.39a2 2 0 0 0-.73-2.73l-.15-.08a2 2 0 0 1-1-1.74v-.5a2 2 0 0 1 1-1.74l.15-.09a2 2 0 0 0 .73-2.73l-.22-.38a2 2 0 0 0-2.73-.73l-.15.08a2 2 0 0 1-2 0l-.43-.25a2 2 0 0 1-1-1.73V4a2 2 0 0 0-2-2z"/><circle cx="12" cy="12" r="3"/>',
    'store': '<path d="m2 7 4.41-4.41A2 2 0 0 1 7.83 2h8.34a2 2 0 0 1 1.42.59L22 7"/><path d="M4 12v8a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-8"/><path d="M15 22v-4a2 2 0 0 0-2-2h-2a2 2 0 0 0-2 2v4"/><path d="M2 7h20"/><path d="M22 7v3a2 2 0 0 1-2 2c-1.1 0-2-.9-2-2V7"/><path d="M18 7v3a2 2 0 0 1-2 2c-1.1 0-2-.9-2-2V7"/><path d="M14 7v3a2 2 0 0 1-2 2c-1.1 0-2-.9-2-2V7"/><path d="M10 7v3a2 2 0 0 1-2 2c-1.1 0-2-.9-2-2V7"/>',
    'book': '<path d="M4 19.5v-15A2.5 2.5 0 0 1 6.5 2H20v20H6.5a2.5 2.5 0 0 1 0-5H20"/>',
    'sun': '<circle cx="12" cy="12" r="4"/><path d="M12 2v2"/><path d="M12 20v2"/><path d="m4.93 4.93 1.41 1.41"/><path d="m17.66 17.66 1.41 1.41"/><path d="M2 12h2"/><path d="M20 12h2"/><path d="m6.34 17.66-1.41 1.41"/><path d="m19.07 4.93-1.41 1.41"/>',
    'moon': '<path d="M12 3a6 6 0 0 0 9 9 9 9 0 1 1-9-9Z"/>',
    'menu': '<line x1="4" x2="20" y1="12" y2="12"/><line x1="4" x2="20" y1="6" y2="6"/><line x1="4" x2="20" y1="18" y2="18"/>',
    'x': '<path d="M18 6 6 18"/><path d="m6 6 12 12"/>',
    'check': '<path d="M20 6 9 17l-5-5"/>',
    'copy': '<rect width="14" height="14" x="8" y="8" rx="2" ry="2"/><path d="M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2"/>',
    'chevron-down': '<path d="m6 9 6 6 6-6"/>',
    'log-out': '<path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"/><polyline points="16 17 21 12 16 7"/><line x1="21" x2="9" y1="12" y2="12"/>',
    'loader': '<line x1="12" x2="12" y1="2" y2="6"/><line x1="12" x2="12" y1="18" y2="22"/><line x1="4.93" x2="7.76" y1="4.93" y2="7.76"/><line x1="16.24" x2="19.07" y1="16.24" y2="19.07"/><line x1="2" x2="6" y1="12" y2="12"/><line x1="18" x2="22" y1="12" y2="12"/><line x1="4.93" x2="7.76" y1="19.07" y2="16.24"/><line x1="16.24" x2="19.07" y1="7.76" y2="4.93"/>',
    'search-x': '<path d="M13 13a5 5 0 0 0-7.54.54l-.46.46a5 5 0 0 0 7.07 7.07l.46-.46a5 5 0 0 0 .55-5.45l3.41-3.41"/><path d="M21 21l-4.35-4.35"/><path d="m17 6 4 4"/><path d="M21 6l-4 4"/>',
    'mail': '<rect width="20" height="16" x="2" y="4" rx="2"/><path d="m22 7-8.97 5.7a1.94 1.94 0 0 1-2.06 0L2 7"/>',
    'lock': '<rect width="18" height="11" x="3" y="11" rx="2" ry="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/>',
    'eye': '<path d="M2 12s3-7 10-7 10 7 10 7-3 7-10 7-10-7-10-7Z"/><circle cx="12" cy="12" r="3"/>',
  };
  for (var name in icons) {
    svg += '<symbol id="icon-'+name+'" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round">'+icons[name]+'</symbol>';
  }
  svg += '</svg>';
  document.addEventListener('DOMContentLoaded', function(){
    var div = document.createElement('div');
    div.innerHTML = svg;
    div.style.display = 'none';
    document.body.insertBefore(div, document.body.firstChild);
  });
})();
EOF
```

- [ ] **Step 2: 更新 render.go embed**

```bash
# 备份
cp internal/web/render.go internal/web/render.go.bak.$(date +%s)
# 在 embed 行加 static/icons.js
sed -i 's|//go:embed templates/\*.html static/app.css static/htmx.min.js static/qrcode.min.js|//go:embed templates/*.html static/app.css static/htmx.min.js static/qrcode.min.js static/icons.js|' internal/web/render.go
grep 'go:embed' internal/web/render.go
```
Expected: 输出含 `static/icons.js`

- [ ] **Step 3: 确认 server.go 已 serve /static/（之前已确认，无需改）**

```bash
grep 'static/' internal/web/server.go | head -2
```
Expected: 看到 `mux.Handle("GET /static/", http.StripPrefix(...))`（已有，无需改）

- [ ] **Step 4: 构建验证 icons.js 能编译进去 + 可访问**

```bash
docker compose build faka 2>&1 | tail -2
docker compose down 2>&1 | tail -1
docker stop faka-site 2>/dev/null; docker rm faka-site 2>/dev/null
docker compose up -d 2>&1 | tail -2
sleep 6
curl -s -o /dev/null -w '%{http_code} size=%{size_download}\n' https://pay.000328.xyz/static/icons.js
```
Expected: `200 size=<约 4000-6000>`（icons.js 可访问）

- [ ] **Step 5: Commit**

```bash
git add internal/web/static/icons.js internal/web/render.go
git commit -m "feat(design): add Lucide icon sprite (local, no CDN)"
```

---

## Task 5: 重写 layout.html（全局布局 + 主题切换）

**Files:**
- Modify: `internal/web/templates/layout.html`

这是最关键的文件——它定义侧栏/header/抽屉/主题切换，所有页面都套用它。重写后所有 emoji 换 Lucide，配色换暖橙，但保留所有现有逻辑（nonce、drawer toggle、copy、theme localStorage）。

- [ ] **Step 1: 备份**

```bash
cp internal/web/templates/layout.html internal/web/templates/layout.html.bak.$(date +%s)
```

- [ ] **Step 2: 写入新 layout.html**

```bash
cat > internal/web/templates/layout.html << 'LAYOUT_EOF'
{{define "layout"}}<!doctype html>
<html lang="zh" data-theme="emerald"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}}</title>
<link rel="stylesheet" href="/static/app.css">
<script src="/static/htmx.min.js" defer></script>
<script src="/static/icons.js" defer></script>
</head>
<body style="background-color:var(--bg-base);color:var(--text-base);min-height:100vh">
<div class="flex min-h-screen">
  {{if .User}}<aside id="app-sidebar" class="hidden lg:flex w-60 shrink-0 flex-col sticky top-0 h-screen" style="background-color:var(--bg-surface);border-right:1px solid var(--border-base)">
    <div class="h-14 flex items-center px-5" style="border-bottom:1px solid var(--border-base)">
      <svg class="icon mr-2" style="width:20px;height:20px;color:var(--brand-solid)"><use href="#icon-ticket"></use></svg>
      <span class="font-bold text-lg" style="color:var(--text-strong)">发卡站</span>
    </div>
    <nav class="flex flex-col py-2 flex-1 overflow-y-auto">
      {{$t := .Title}}
      {{template "navitem" (dict "T" $t "Match" "概览" "Href" "/" "Icon" "dashboard" "Label" "概览")}}
      {{template "navitem" (dict "T" $t "Match" "购买" "Href" "/buy" "Icon" "cart" "Label" "买码")}}
      {{template "navitem" (dict "T" $t "Match" "充值" "Href" "/recharge" "Icon" "wallet" "Label" "充值")}}
      {{template "navitem" (dict "T" $t "Match" "订单" "Href" "/orders" "Icon" "package" "Label" "订单")}}
      {{if eq .User.Role "admin"}}
      <div class="text-xs px-5 mt-3 mb-1 uppercase tracking-wide" style="color:var(--text-faint)">管理</div>
      {{template "navitem" (dict "T" $t "Match" "用户管理" "Href" "/admin/users" "Icon" "users" "Label" "用户")}}
      {{template "navitem" (dict "T" $t "Match" "建账户" "Href" "/admin/create" "Icon" "user-plus" "Label" "建账户")}}
      {{template "navitem" (dict "T" $t "Match" "配置" "Href" "/admin/config" "Icon" "settings" "Label" "配置")}}
      {{template "navitem" (dict "T" $t "Match" "商户管理" "Href" "/admin/merchants" "Icon" "store" "Label" "商户管理")}}
      {{template "navitem" (dict "T" $t "Match" "支付配置文档" "Href" "/admin/docs" "Icon" "book" "Label" "支付文档")}}
      {{end}}
    </nav>
    <div class="m-3 p-3 rounded-2xl" style="background-color:var(--bg-subtle)">
      <div class="flex items-center gap-3">
        <div class="w-9 h-9 rounded-full grid place-items-center font-semibold shrink-0" style="background-image:linear-gradient(135deg,var(--brand-grad-from),var(--brand-grad-to));color:#fff">{{slice .User.Email 0 1}}</div>
        <div class="min-w-0">
          <div class="text-sm font-medium truncate" style="color:var(--text-strong)">{{.User.Email}}</div>
          <div class="mt-0.5">{{if eq .User.Role "admin"}}<span class="badge badge-success badge-xs">管理员</span>{{else}}<span class="badge badge-neutral badge-xs">普通用户</span>{{end}}</div>
        </div>
      </div>
      <div class="text-xs mt-2" style="color:var(--text-muted)">余额 <span class="font-semibold" style="color:var(--text-strong)">{{.User.BalanceFmt}}</span></div>
      <a href="/logout" class="btn btn-ghost btn-xs btn-block mt-2 flex items-center justify-center gap-1"><svg class="icon" style="width:12px;height:12px"><use href="#icon-log-out"></use></svg>退出</a>
    </div>
  </aside>{{end}}

  <div class="flex-1 min-w-0 flex flex-col min-h-screen">
    <header class="h-14 sticky top-0 flex items-center justify-between px-6 z-10" style="background-color:var(--bg-surface);border-bottom:1px solid var(--border-base)">
      <div class="flex items-center gap-3 min-w-0">
        {{if .User}}<button id="nav-toggle" class="btn btn-ghost btn-circle btn-sm lg:hidden" aria-label="菜单"><svg class="icon" style="width:18px;height:18px"><use href="#icon-menu"></use></svg></button>{{end}}
        <span class="text-sm truncate"><span style="color:var(--text-strong)">发卡站</span> <span style="color:var(--text-faint)">/</span> <span style="color:var(--text-muted)">{{.Title}}</span></span>
      </div>
      <div class="flex items-center gap-2 shrink-0">
        {{if .User}}<span class="badge badge-success badge-outline badge-lg flex items-center gap-1"><svg class="icon" style="width:12px;height:12px"><use href="#icon-wallet"></use></svg>{{.User.BalanceFmt}}</span>{{end}}
        <button class="btn btn-ghost btn-circle btn-sm" data-theme-toggle aria-label="切换主题"><svg class="icon theme-icon-light" style="width:18px;height:18px"><use href="#icon-sun"></use></svg><svg class="icon theme-icon-dark hidden" style="width:18px;height:18px"><use href="#icon-moon"></use></svg></button>
      </div>
    </header>
    <main class="flex-1 p-6" style="background-color:var(--bg-base)"><div class="max-w-6xl mx-auto">{{template "content" .}}</div></main>
  </div>
</div>
{{if .User}}<div id="mobile-drawer" class="hidden fixed inset-0 z-40 lg:hidden">
  <div class="absolute inset-0 bg-black/40" data-drawer-close></div>
  <aside class="absolute left-0 top-0 h-full w-64 flex flex-col" style="background-color:var(--bg-surface);border-right:1px solid var(--border-base)">
    <div class="h-14 flex items-center justify-between px-5" style="border-bottom:1px solid var(--border-base)">
      <span class="font-bold text-lg flex items-center gap-2"><svg class="icon" style="width:20px;height:20px;color:var(--brand-solid)"><use href="#icon-ticket"></use></svg>发卡站</span>
      <button class="btn btn-ghost btn-circle btn-sm" data-drawer-close aria-label="关闭"><svg class="icon" style="width:18px;height:18px"><use href="#icon-x"></use></svg></button>
    </div>
    <nav class="flex flex-col py-2 flex-1 overflow-y-auto">
      {{$t := .Title}}
      {{template "navitem" (dict "T" $t "Match" "概览" "Href" "/" "Icon" "dashboard" "Label" "概览")}}
      {{template "navitem" (dict "T" $t "Match" "购买" "Href" "/buy" "Icon" "cart" "Label" "买码")}}
      {{template "navitem" (dict "T" $t "Match" "充值" "Href" "/recharge" "Icon" "wallet" "Label" "充值")}}
      {{template "navitem" (dict "T" $t "Match" "订单" "Href" "/orders" "Icon" "package" "Label" "订单")}}
      {{if eq .User.Role "admin"}}
      <div class="text-xs px-5 mt-3 mb-1 uppercase tracking-wide" style="color:var(--text-faint)">管理</div>
      {{template "navitem" (dict "T" $t "Match" "用户管理" "Href" "/admin/users" "Icon" "users" "Label" "用户")}}
      {{template "navitem" (dict "T" $t "Match" "建账户" "Href" "/admin/create" "Icon" "user-plus" "Label" "建账户")}}
      {{template "navitem" (dict "T" $t "Match" "配置" "Href" "/admin/config" "Icon" "settings" "Label" "配置")}}
      {{template "navitem" (dict "T" $t "Match" "商户管理" "Href" "/admin/merchants" "Icon" "store" "Label" "商户管理")}}
      {{template "navitem" (dict "T" $t "Match" "支付配置文档" "Href" "/admin/docs" "Icon" "book" "Label" "支付文档")}}
      {{end}}
    </nav>
  </aside>
</div>{{end}}
{{define "navitem"}}<a href="{{.Href}}" class="h-11 flex items-center gap-3 px-5 border-l-4 {{if eq .T .Match}}" style="background-color:var(--bg-subtle);color:var(--brand-solid);font-weight:600;border-color:var(--brand-solid)"{{else}}" style="color:var(--text-muted);border-color:transparent" onmouseover="this.style.backgroundColor='var(--bg-subtle)'" onmouseout="this.style.backgroundColor='transparent'"{{end}}><svg class="icon" style="width:18px;height:18px"><use href="#icon-{{.Icon}}"></use></svg>{{.Label}}</a>{{end}}
<script nonce="{{.Nonce}}">
(function(){
  var d = document.documentElement;
  var saved = localStorage.getItem('theme');
  if (saved) d.setAttribute('data-theme', saved);
  function syncThemeIcon(){ var dark = d.getAttribute('data-theme')==='dark';
    document.querySelectorAll('.theme-icon-light').forEach(function(e){e.classList.toggle('hidden',dark)});
    document.querySelectorAll('.theme-icon-dark').forEach(function(e){e.classList.toggle('hidden',!dark)});
  }
  syncThemeIcon();
  document.querySelectorAll('[data-theme-toggle]').forEach(function(el){
    el.addEventListener('click', function(){
      var nt = d.getAttribute('data-theme') === 'dark' ? 'emerald' : 'dark';
      d.setAttribute('data-theme', nt); localStorage.setItem('theme', nt); syncThemeIcon();
    });
  });
  function flash(el){ var o = el.textContent; el.textContent = '已复制 ✓'; setTimeout(function(){ el.textContent = o; }, 900); }
  document.body.addEventListener('click', function(e){
    var c = e.target.closest('[data-copy]'); if (c) { navigator.clipboard.writeText(c.getAttribute('data-copy')); flash(c); return; }
    var a = e.target.closest('[data-copy-all]'); if (a) { navigator.clipboard.writeText(a.getAttribute('data-copy-all')); flash(a); return; }
  });
  var drawer = document.getElementById('mobile-drawer');
  var toggle = document.getElementById('nav-toggle');
  function closeDrawer(){ if (drawer) drawer.classList.add('hidden'); }
  if (toggle && drawer) {
    toggle.addEventListener('click', function(){ drawer.classList.toggle('hidden'); });
    drawer.querySelectorAll('[data-drawer-close], nav a').forEach(function(el){ el.addEventListener('click', closeDrawer); });
  }
})();
</script>
</body></html>{{end}}
LAYOUT_EOF
```

> 注意：上面的 `{{template "navitem" (dict ...)}}` 用了 `dict` 函数。Go template 默认没有 `dict`，需要确认 render.go 是否注册了该 funcmap。如果没注册，需在 render.go 的 template New 处加 `"dict": func(args ...interface{}) map[string]interface{}{...}`。**Step 3 先检查。**

- [ ] **Step 3: 检查/添加 dict funcmap**

```bash
grep -n 'Funcs\|"dict"\|"joinCodes"\|"usd"' internal/web/render.go | head
```
如果输出含 `"dict"` → 已注册，跳过。如果不含，需要加：

```bash
# 找到 template.New 那行，在 Funcs map 里加 dict
# 示例（具体行号用上面 grep 结果定位）:
python3 -c "
p='internal/web/render.go'
s=open(p).read()
# 在已有的 Funcs map 里加 dict（假设有 joinCodes/usd 等已有 func）
if '\"dict\"' not in s:
    # 找 joinCodes 注册处附近加
    import re
    s = re.sub(r'(\"joinCodes\":\s*)([^,}]+)(,?)', r'\1\2\3\n\t\t\"dict\": func(values ...interface{}) (map[string]interface{}, error) {\n\t\t\tif len(values)%2 != 0 { return nil, fmt.Errorf(\"odd args\") }\n\t\t\tm := make(map[string]interface{}, len(values)/2)\n\t\t\tfor i := 0; i < len(values); i += 2 { k, ok := values[i].(string); if !ok { return nil, fmt.Errorf(\"key not string\") }; m[k] = values[i+1] }\n\t\t\treturn m, nil\n\t\t},', s)
    open(p,'w').write(s)
    print('dict funcmap added')
else:
    print('dict already registered')
"
```
确认 `fmt` 已 import（render.go 顶部应该有）。

- [ ] **Step 4: 构建 + 重启 + 验证页面不报错**

```bash
docker compose build faka 2>&1 | tail -3
```
如果有编译错误，根据错误修。常见：`dict` 未注册（按 Step 3 加）、`fmt` 未 import（render.go 加 `"fmt"`）。

```bash
docker compose down 2>&1 | tail -1
docker stop faka-site 2>/dev/null; docker rm faka-site 2>/dev/null
docker compose up -d 2>&1 | tail -2
sleep 6
# 验证登录页能渲染（用 admin 登录看 dashboard）
curl -s https://pay.000328.xyz/login | grep -c 'icon-' 
```
Expected: > 0（页面含 Lucide 图标引用）。同时浏览器打开 `https://pay.000328.xyz/login` 肉眼确认布局正常、图标显示。

- [ ] **Step 5: Commit**

```bash
git add internal/web/templates/layout.html internal/web/render.go
git commit -m "feat(design): rewrite layout with Lucide icons and warm theme tokens"
```

---

## Task 6: 重做 login.html + forgot.html

这两个是未登录页，先做（不依赖 layout 的 `.User`）。

**Files:**
- Modify: `internal/web/templates/login.html`
- Modify: `internal/web/templates/forgot.html`

- [ ] **Step 1: 备份**

```bash
cp internal/web/templates/login.html internal/web/templates/login.html.bak.$(date +%s)
cp internal/web/templates/forgot.html internal/web/templates/forgot.html.bak.$(date +%s)
```

- [ ] **Step 2: 读现有 login.html 确认字段名**

```bash
cat internal/web/templates/login.html
```
记录：表单 action、input name（email/password/csrf）、错误显示位置、`.Data.error` 等结构。**新模板必须保留这些字段名。**

- [ ] **Step 3: 写新 login.html（保留字段，换样式）**

按 spec 视觉系统重写：居中卡片、Lucide `mail`/`lock`/`eye` 图标作为 input 前缀、暖橙 submit 按钮、错误用 `badge-error` 样式。**保留所有 `name=`、`action=`、`{{.CSRF}}`、`{{.Data.error}}`。**

```bash
# 用 heredoc 写入，基于 Step 2 读到的字段名
# 模板要点:
# - form action/method 保留原值
# - input name="email" name="password" 保留
# - {{.CSRF}} hidden input 保留
# - {{.Data.error}} 错误用 alert 样式
# - 用 .card-warm + Lucide 图标 + btn-brand
```
（具体 HTML 由实现者基于读到的字段名写，遵循 spec 视觉系统）

- [ ] **Step 4: 同样重做 forgot.html**

- [ ] **Step 5: 构建 + 重启 + 浏览器验证 login 页**

```bash
docker compose build faka 2>&1 | tail -2
docker compose down 2>&1 | tail -1; docker stop faka-site 2>/dev/null; docker rm faka-site 2>/dev/null
docker compose up -d 2>&1 | tail -2; sleep 6
```
浏览器打开 `https://pay.000328.xyz/login`，确认：图标显示、表单可用、错误提示样式、能登录成功（功能无回归）。

- [ ] **Step 6: Commit**

```bash
git add internal/web/templates/login.html internal/web/templates/forgot.html
git commit -m "feat(design): redesign login and forgot pages with warm theme"
```

---

## Task 7: 重做 dashboard.html

**Files:**
- Modify: `internal/web/templates/dashboard.html`

- [ ] **Step 1: 备份 + 读现有**

```bash
cp internal/web/templates/dashboard.html internal/web/templates/dashboard.html.bak.$(date +%s)
cat internal/web/templates/dashboard.html
```
保留：`.Data.Balance`/`.Data.OrderCount`/`.Data.MonthlyUsed`/`.Data.UserCount`/`.Data.PlatformBalance`、`{{usd .}}`、`{{template "orders_list" .}}`。

- [ ] **Step 2: 按视觉规范重写**

统计卡用 `.card-warm` + 4 列网格（移动端单列）、stat-value 20px/700 暖橙、CTA 用 `.btn-brand`、订单列表区保留 `{{template "orders_list" .}}`。**所有 emoji 换 Lucide。**

- [ ] **Step 3: 构建 + 重启 + 验证（登录后看 dashboard）**

```bash
docker compose build faka 2>&1 | tail -2
docker compose down 2>&1 | tail -1; docker stop faka-site 2>/dev/null; docker rm faka-site 2>/dev/null
docker compose up -d 2>&1 | tail -2; sleep 6
```
浏览器登录后看 `/`，确认统计卡、CTA、订单列表样式。

- [ ] **Step 4: Commit**

```bash
git add internal/web/templates/dashboard.html
git commit -m "feat(design): redesign dashboard with stat cards and warm theme"
```

---

## Task 8: 重做 buy.html

**Files:**
- Modify: `internal/web/templates/buy.html`

- [ ] **Step 1: 备份 + 读现有（含 JS）**

```bash
cp internal/web/templates/buy.html internal/web/templates/buy.html.bak.$(date +%s)
cat internal/web/templates/buy.html
```
保留：表单 `name=count`/`name=quota`/`name=csrf`、`action="/buy"`、`.Data.error`/`.Data.result`、所有 JS（recalc/inc/dec/submit disabled 逻辑）、`data-copy`/`data-copy-all`、`{{joinCodes .Data.result.Codes}}`。**JS 逻辑要完整搬到新模板，不能丢。**

- [ ] **Step 2: 重写（保留所有字段 + JS，换样式）**

左侧表单卡 `.card-warm`、步进器用 Lucide 替代 `−`/`+` 文字、快捷按钮用 `.badge-neutral`、价格汇总用 `var(--bg-subtle)`、submit 用 `.btn-brand` + loading 态。右侧账户信息卡 + 说明卡。

- [ ] **Step 3: 构建 + 重启 + 验证购买流程**

```bash
docker compose build faka 2>&1 | tail -2
docker compose down 2>&1 | tail -1; docker stop faka-site 2>/dev/null; docker rm faka-site 2>/dev/null
docker compose up -d 2>&1 | tail -2; sleep 6
```
浏览器 `/buy`，测试：步进器加减、快捷按钮、价格联动、**真实下一单**确认功能无回归。

- [ ] **Step 4: Commit**

```bash
git add internal/web/templates/buy.html
git commit -m "feat(design): redesign buy page, preserve all form/JS logic"
```

---

## Task 9: 重做 recharge.html + recharge_pay.html

**Files:**
- Modify: `internal/web/templates/recharge.html`
- Modify: `internal/web/templates/recharge_pay.html`

- [ ] **Step 1: 备份 + 读现有**

```bash
cp internal/web/templates/recharge.html internal/web/templates/recharge.html.bak.$(date +%s)
cp internal/web/templates/recharge_pay.html internal/web/templates/recharge_pay.html.bak.$(date +%s)
cat internal/web/templates/recharge.html
cat internal/web/templates/recharge_pay.html
```

- [ ] **Step 2: 重写两个文件（保留字段/JS）**

recharge：金额输入 + 支付方式选择（支付宝/微信，图标用 Lucide 或品牌色块）+ `.btn-brand`。
recharge_pay：支付中状态页，可能含倒计时/轮询 JS，完整保留。

- [ ] **Step 3: 构建 + 重启 + 验证充值下单流程**

浏览器 `/recharge` 下单，确认跳转到 recharge_pay，功能无回归。

- [ ] **Step 4: Commit**

```bash
git add internal/web/templates/recharge.html internal/web/templates/recharge_pay.html
git commit -m "feat(design): redesign recharge flow pages"
```

---

## Task 10: 重做 orders.html + orders_list.html + order.html

**Files:**
- Modify: `internal/web/templates/orders.html`
- Modify: `internal/web/templates/orders_list.html`（被 dashboard/orders 复用，改动影响多处，谨慎）
- Modify: `internal/web/templates/order.html`

- [ ] **Step 1: 备份 + 读现有（orders_list 是复用片段，特别注意）**

```bash
cp internal/web/templates/orders_list.html internal/web/templates/orders_list.html.bak.$(date +%s)
cp internal/web/templates/orders.html internal/web/templates/orders.html.bak.$(date +%s)
cp internal/web/templates/order.html internal/web/templates/order.html.bak.$(date +%s)
cat internal/web/templates/orders_list.html
cat internal/web/templates/order.html
```

- [ ] **Step 2: 重写三个文件**

orders_list：表格用 `.card-warm` 包裹、状态列用 `.badge-success/.badge-warning/.badge-error`、空状态用 Lucide `package`/`search-x`。
order：单订单详情，兑换码展示用 `.kbd` + `data-copy`。

- [ ] **Step 3: 构建 + 重启 + 验证（看订单列表 + 点进详情）**

- [ ] **Step 4: Commit**

```bash
git add internal/web/templates/orders_list.html internal/web/templates/orders.html internal/web/templates/order.html
git commit -m "feat(design): redesign orders list and detail pages"
```

---

## Task 11: 重做 7 个 admin 模板

**Files:**
- Modify: `internal/web/templates/admin_users.html`
- Modify: `internal/web/templates/admin_create.html`
- Modify: `internal/web/templates/admin_balance.html`
- Modify: `internal/web/templates/admin_config.html`
- Modify: `internal/web/templates/admin_merchants.html`
- Modify: `internal/web/templates/admin_reset.html`
- Modify: `internal/web/templates/admin_docs.html`

这 7 个是管理后台页面，工作量大但模式相似（表格 + 表单）。可以按相似度分组并行处理。

- [ ] **Step 1: 备份全部 + 逐个读现有**

```bash
for f in admin_users admin_create admin_balance admin_config admin_merchants admin_reset admin_docs; do
  cp internal/web/templates/$f.html internal/web/templates/$f.html.bak.$(date +%s)
done
# 逐个 cat 读，记录字段名/JS
```

- [ ] **Step 2: 重做 admin_users.html（表格为主）**

用户列表表格 `.card-warm`、状态 badge、操作按钮。保留所有路由/字段。

- [ ] **Step 3: 构建 + 验证 admin_users**

```bash
docker compose build faka 2>&1 | tail -2
docker compose down 2>&1 | tail -1; docker stop faka-site 2>/dev/null; docker rm faka-site 2>/dev/null
docker compose up -d 2>&1 | tail -2; sleep 6
```
浏览器 `/admin/users` 验证。

- [ ] **Step 4: Commit admin_users**

```bash
git add internal/web/templates/admin_users.html
git commit -m "feat(design): redesign admin users page"
```

- [ ] **Step 5: 重做 admin_create.html（表单为主）+ 构建/验证/Commit**

类似 Step 2-4，表单用 `.card-warm` + `.btn-brand`。

- [ ] **Step 6: 重做 admin_balance.html（余额调整表单）+ 构建/验证/Commit**

- [ ] **Step 7: 重做 admin_config.html（支付配置，可能含密钥上传）+ 构建/验证/Commit**

⚠️ 这个页面含敏感配置（alipay 密钥等），重做时**只改样式不改字段/JS**，特别注意文件上传 input。

- [ ] **Step 8: 重做 admin_merchants.html（商户管理表格）+ 构建/验证/Commit**

- [ ] **Step 9: 重做 admin_reset.html（重置页）+ 构建/验证/Commit**

- [ ] **Step 10: 重做 admin_docs.html（支付配置文档，多为静态说明）+ 构建/验证/Commit**

- [ ] **Step 11: 全部 admin 页面统一验证**

浏览器逐个访问 `/admin/*`，确认所有页面样式统一、功能正常。

---

## Task 12: 重做 epay 收款页（payPageTpl）

**Files:**
- Modify: `internal/epay/handler.go`（`payPageTpl` 常量）

这是对外部商户的收款页（`/submit.php`），独立于用户站，不走 layout。已修过本地 qrcode.min.js 引用，现在套用暖色风格。

- [ ] **Step 1: 备份 + 定位 payPageTpl**

```bash
cp internal/epay/handler.go internal/epay/handler.go.bak.paytpl.$(date +%s)
grep -n 'payPageTpl' internal/epay/handler.go | head -2
# 读 payPageTpl = template.Must(...) 整块
```

- [ ] **Step 2: 重写 payPageTpl（保留所有模板字段 + JS）**

保留：`{{.Name}}`/`{{.Money}}`/`{{.QRCode}}`/`{{.TradeNo}}`/`{{.PayTypeName}}`/`{{.ErrMsg}}`、qrcode.min.js 引用、checkStatus 轮询 JS、return_url 跳转。
换样式：`.card-warm` 居中卡、金额 stat-value 暖橙、状态用 `.badge-*`、二维码区背景 `var(--bg-subtle)`。

⚠️ **收款页是 epay 公开端点，CSP 已在 headers.go 设为宽松（`unsafe-inline`）**，内联 JS 能跑。但要注意：这个页面**不经过 layout**，所以 CSS 变量需要在本页 `<style>` 里也定义（或内联用具体色值）。

- [ ] **Step 3: 构建 + 重启 + 用浏览器实测二维码页**

```bash
docker compose build faka 2>&1 | tail -2
docker compose down 2>&1 | tail -1; docker stop faka-site 2>/dev/null; docker rm faka-site 2>/dev/null
docker compose up -d 2>&1 | tail -2; sleep 6
```
浏览器从南京 newapi 下单（`https://matrix.wubinstu.com/wallet` 充值 $1 支付宝），跳转到 `pay.000328.xyz/submit.php`，确认：收款页样式、二维码渲染（之前修过的 CSP 问题不能回归）。

- [ ] **Step 4: Commit**

```bash
git add internal/epay/handler.go
git commit -m "feat(design): redesign epay payment page with warm theme"
```

---

## Task 13: 全站回归测试 + 暗色主题验证

**Files:**
- 无新改动，验证

- [ ] **Step 1: 跑全部 Go 测试**

```bash
docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine go test ./... 2>&1 | tail -15
```
Expected: 全部 `ok`，无 FAIL。**与 Task 1 基线对比，测试数不减少。** 特别关注 `headers_test.go`（CSP nonce 测试）、`recharge*_test.go`（充值流程测试）。

- [ ] **Step 2: 明色主题全站截图**

浏览器（明色）逐页截图：login/dashboard/buy/recharge/orders/order/admin_*(7)/收款页。

- [ ] **Step 3: 暗色主题全站截图**

浏览器切换暗色（点 theme-toggle），重复 Step 2。确认：暗色背景是深棕（非纯黑）、暖橙强调色可见、对比度足够（文字清晰）。

- [ ] **Step 4: 移动端截图（375px 宽）**

Chrome DevTools 切到 iPhone SE (375px)，逐页截图：侧栏折叠成抽屉、统计卡单列、表单单列、按钮全宽、收款页二维码居中可扫。

- [ ] **Step 5: emoji 残留检查**

```bash
grep -rnE '🎫|💰|📊|📦|👤|⚙️|🛒|🏪|📖|🌓|☰|✕|➕|🔽' internal/web/templates/ internal/epay/handler.go
```
Expected: **无输出**（所有 emoji 已替换为 Lucide）。如有残留，逐个替换。

- [ ] **Step 6: 端到端功能验证（无回归）**

- 登录/登出正常
- 购买兑换码成功生成
- 充值下单 → 跳转收款页 → 二维码显示
- 订单列表/详情正常
- admin 各页面操作正常（建用户/改余额/配置）
- 主题切换持久化（刷新后保持）

- [ ] **Step 7: 如有问题，修复后回到对应 Task 的 Commit**

---

## Task 14: 清理备份文件 + 合并

**Files:**
- 删除所有 `.bak.*` 文件

- [ ] **Step 1: 清理备份**

```bash
cd /opt/faka-site
find . -name '*.bak.*' -not -path './node_modules/*' -delete
git status  # 确认工作区干净（备份文件未被 git 跟踪的话）
```

- [ ] **Step 2: 最终构建确认**

```bash
docker compose build faka 2>&1 | tail -2
docker compose down 2>&1 | tail -1; docker stop faka-site 2>/dev/null; docker rm faka-site 2>/dev/null
docker compose up -d 2>&1 | tail -2; sleep 6
curl -s -o /dev/null -w '%{http_code}' https://pay.000328.xyz/login
```
Expected: 200，生产版本干净运行。

- [ ] **Step 3: 合并分支（或留 PR）**

```bash
git checkout main  # 或 master，看默认分支
git merge visual-redesign
# 或推到远端开 PR
```

---

## Self-Review

**Spec coverage 检查:**
- ✅ 色彩系统（明/暗）→ Task 2 (Tailwind), Task 3 (CSS vars)
- ✅ 语义状态色 → Task 3 (CSS vars) + 各页面 Task 用 `.badge-*`
- ✅ 圆角/阴影/字号/间距 token → Task 2 (Tailwind config) + Task 3 (CSS)
- ✅ 响应式断点/移动端 → Task 5 (layout drawer) + 各页面 + Task 13 验证
- ✅ Lucide 图标系统 + 映射表 → Task 4 (sprite) + Task 5 (nav) + 各页替换
- ✅ 按钮交互态 → Task 3 (`.btn-brand` CSS) + 各页面用
- ✅ 架构策略（不变/改的）→ Task 5-12 严格保留后端字段/JS
- ✅ 错误/空状态/加载态 → 各页面 Task 内处理
- ✅ 测试策略 → Task 1 基线 + Task 13 回归
- ✅ 风险回滚 → 全程 git 分支 + 每任务 commit + 备份

**Placeholder 检查:** Task 6-12 的"重写 HTML"步骤用文字描述了要点但没贴完整 HTML（因为每个页面需先读现有文件才知道字段名，无法预写）。这是**必要的不确定性**——每个 Task 的 Step 1 都要求"读现有文件记录字段名"，Step 2 基于读到的内容写。这不是 placeholder，是因为模板内容依赖现有代码。已在每个 Task 明确列出"保留字段"清单。

**类型一致性:** `dict` funcmap 在 Task 5 注册，layout 用 `{{template "navitem" (dict ...)}}`，navitem 定义在 layout 内 `{{define "navitem"}}`——签名匹配。`.badge-success/.badge-warning/.badge-error/.badge-neutral` 在 Task 3 CSS 定义，各 Task 使用——一致。`.card-warm`/`.btn-brand` 同理。

---

## 执行交接

Plan complete and saved to `docs/superpowers/plans/2026-06-23-faka-visual-redesign.md`. Two execution options:

**1. Subagent-Driven (recommended)** - 每个 Task 派一个 fresh subagent，任务间 review，快速迭代。适合这种多任务、每任务独立的计划。

**2. Inline Execution** - 在当前 session 用 executing-plans 批量执行，带 checkpoint review。

Which approach?
