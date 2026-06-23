/** @type {import('tailwindcss').Config} */
module.exports = {
  content: ["./internal/web/templates/**/*.html", "./internal/web/**/*.go"],
  theme: {
    extend: {
      fontFamily: {
        sans: ['Inter', '-apple-system', 'BlinkMacSystemFont', 'Segoe UI', 'system-ui', 'sans-serif'],
      },
      colors: {
        // 暖橙主色族（琥珀橙）。明色主题用 500/600 渐变，暗色主题用 400/500 渐变。
        brand: {
          50: '#FFFBF5',   // 页面背景（amber-50 偏暖白）
          100: '#FFF7ED',  // 次级背景（orange-50）
          200: '#FED7AA',  // 边框（amber-200）
          300: '#FDBA74',
          400: '#FBBF24',  // 暗色主题主色亮版（amber-400）
          500: '#F59E0B',  // 明色主题渐变起点（amber-500）
          600: '#EA580C',  // 明色主题主色（orange-600）
          700: '#C2410C',  // hover 深 10%（orange-700）
          800: '#9A3412',  // 正文文字（orange-800）
          900: '#7C2D12',  // 强文字（orange-900）
        },
      },
      boxShadow: {
        // 暖色调阴影（非默认黑），与暖色主题协调
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
