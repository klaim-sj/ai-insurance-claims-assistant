/** @type {import('tailwindcss').Config} */
module.exports = {
  content: [
    "./web/templates/**/*.html",
    "./web/static/js/app.js",
    // Class lencana status didefinisikan di Go (Status.BadgeClass).
    "./internal/claims/status.go",
  ],
  theme: { extend: {} },
  plugins: [],
};
