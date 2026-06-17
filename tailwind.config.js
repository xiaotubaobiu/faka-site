/** @type {import('tailwindcss').Config} */
module.exports = {
  content: ["./internal/web/templates/**/*.html", "./internal/web/**/*.go"],
  theme: { extend: {} },
  plugins: [require("daisyui")],
  daisyui: { themes: ["emerald", "dark"], darkTheme: "dark" },
};
