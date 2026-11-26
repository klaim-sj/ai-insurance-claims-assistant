// Skrip kecil tanpa inline JS (CSP: script-src 'self').
(function () {
  "use strict";

  // Kirim token CSRF pada setiap request HTMX.
  document.addEventListener("htmx:configRequest", function (evt) {
    var meta = document.querySelector('meta[name="csrf-token"]');
    if (meta && meta.content) {
      evt.detail.headers["X-CSRF-Token"] = meta.content;
    }
  });

  // Respons 409 (duplikat) dan 422 (validasi) berisi pesan untuk pengguna;
  // tampilkan alih-alih diabaikan sebagai error.
  document.addEventListener("htmx:beforeSwap", function (evt) {
    var st = evt.detail.xhr.status;
    if (st === 409 || st === 422) {
      evt.detail.shouldSwap = true;
      evt.detail.isError = false;
    }
  });

  // Error lain saat unggah (mis. 413 total terlalu besar): tampilkan pesan
  // singkat di area hasil unggah.
  document.addEventListener("htmx:responseError", function (evt) {
    var box = document.getElementById("upload-result");
    if (!box || evt.detail.elt.id !== "upload-form") return;
    var p = document.createElement("p");
    p.setAttribute("role", "alert");
    p.className = "rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-800";
    p.textContent = evt.detail.xhr.status === 413
      ? "Total unggahan melebihi batas. Kurangi jumlah atau ukuran file."
      : "Unggahan gagal. Muat ulang halaman lalu coba lagi.";
    box.replaceChildren(p);
  });

  // Form dengan data-confirm meminta konfirmasi sebelum dikirim
  // (mis. menerapkan nilai terdeteksi, proses ulang dokumen).
  document.addEventListener("submit", function (evt) {
    var form = evt.target;
    var msg = form && form.getAttribute && form.getAttribute("data-confirm");
    if (msg && !window.confirm(msg)) {
      evt.preventDefault();
    }
  });

  // Toggle menu navigasi pada layar kecil (dapat dipakai dengan keyboard).
  document.addEventListener("DOMContentLoaded", function () {
    var btn = document.querySelector("[data-menu-toggle]");
    var menu = document.querySelector("[data-menu]");
    if (!btn || !menu) return;

    function setOpen(open) {
      btn.setAttribute("aria-expanded", open ? "true" : "false");
      menu.classList.toggle("hidden", !open);
    }

    btn.addEventListener("click", function () {
      setOpen(btn.getAttribute("aria-expanded") !== "true");
    });

    document.addEventListener("keydown", function (e) {
      if (e.key === "Escape" && btn.getAttribute("aria-expanded") === "true") {
        setOpen(false);
        btn.focus();
      }
    });
  });
})();
