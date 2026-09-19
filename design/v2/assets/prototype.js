(function () {
  "use strict";

  const params = new URLSearchParams(window.location.search);
  const theme = params.get("theme");
  const state = params.get("state");

  if (theme === "light" || theme === "dark") {
    document.documentElement.dataset.theme = theme;
  }
  if (state) document.documentElement.dataset.state = state;

  const $ = (selector, root = document) => root.querySelector(selector);
  const $$ = (selector, root = document) => Array.from(root.querySelectorAll(selector));

  function iconPath(name) {
    const paths = {
      menu: '<path d="M4 7h16M4 12h16M4 17h16"/>',
      close: '<path d="m6 6 12 12M18 6 6 18"/>',
      external: '<path d="M14 4h6v6M10 14 20 4M20 14v5a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V5a1 1 0 0 1 1-1h5"/>',
      article: '<path d="M6 3h9l3 3v15H6zM9 11h6M9 15h6M15 3v4h4"/>',
      project: '<path d="M4 7h16v12H4zM8 7V4h8v3"/>',
      logout: '<path d="M10 5H5v14h5M14 8l4 4-4 4M8 12h10"/>',
      upload: '<path d="M12 16V4m0 0L8 8m4-4 4 4M5 20h14"/>',
      check: '<path d="m5 12 4 4L19 6"/>',
      alert: '<path d="M12 3 2.5 20h19zM12 9v4M12 17h.01"/>',
      github: '<path d="M12 2a10 10 0 0 0-3.16 19.49c.5.09.68-.22.68-.48v-1.69c-2.78.6-3.37-1.18-3.37-1.18-.45-1.16-1.11-1.47-1.11-1.47-.91-.62.07-.61.07-.61 1 .07 1.53 1.03 1.53 1.03.9 1.53 2.35 1.09 2.92.83.09-.65.35-1.09.64-1.34-2.22-.25-4.56-1.11-4.56-4.94 0-1.09.39-1.98 1.03-2.68-.1-.25-.45-1.27.1-2.64 0 0 .84-.27 2.75 1.02A9.6 9.6 0 0 1 12 6.7a9.6 9.6 0 0 1 2.5.34c1.91-1.29 2.75-1.02 2.75-1.02.55 1.37.2 2.39.1 2.64.64.7 1.03 1.59 1.03 2.68 0 3.84-2.34 4.68-4.57 4.93.36.31.68.92.68 1.86V21c0 .27.18.58.69.48A10 10 0 0 0 12 2z"/>',
      grip: '<path d="M9 5h.01M15 5h.01M9 12h.01M15 12h.01M9 19h.01M15 19h.01"/>'
    };
    return paths[name] || paths.alert;
  }

  $$("[data-icon]").forEach((node) => {
    const name = node.dataset.icon;
    node.innerHTML = `<svg aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="square" stroke-linejoin="miter">${iconPath(name)}</svg>`;
  });

  $$('[data-menu-toggle]').forEach((button) => {
    const target = document.getElementById(button.getAttribute("aria-controls"));
    if (!target) return;
    button.addEventListener("click", () => {
      const open = button.getAttribute("aria-expanded") === "true";
      button.setAttribute("aria-expanded", String(!open));
      target.classList.toggle("is-open", !open);
    });
  });

  function toast(message) {
    let stack = $(".toast-stack");
    if (!stack) {
      stack = document.createElement("div");
      stack.className = "toast-stack";
      stack.setAttribute("aria-live", "polite");
      document.body.appendChild(stack);
    }
    const item = document.createElement("div");
    item.className = "toast";
    item.textContent = message;
    stack.appendChild(item);
    window.setTimeout(() => item.remove(), 2600);
  }

  const dialogOrigins = new WeakMap();

  function focusable(dialog) {
    return $$('button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), a[href], [tabindex]:not([tabindex="-1"])', dialog)
      .filter((node) => !node.hidden && node.offsetParent !== null);
  }

  function openDialog(id) {
    const dialog = document.getElementById(id);
    if (!dialog) return;
    dialogOrigins.set(dialog, document.activeElement);
    dialog.hidden = false;
    const first = focusable(dialog)[0];
    if (first) first.focus();
  }

  function closeDialog(dialog) {
    dialog.hidden = true;
    const origin = dialogOrigins.get(dialog);
    if (origin && typeof origin.focus === "function") origin.focus();
  }

  $$('[data-dialog-open]').forEach((button) => {
    button.addEventListener("click", () => openDialog(button.dataset.dialogOpen));
  });
  $$('[data-dialog-close]').forEach((button) => {
    button.addEventListener("click", () => closeDialog(button.closest(".dialog-backdrop")));
  });
  $$(".dialog-backdrop").forEach((backdrop) => {
    backdrop.addEventListener("click", (event) => {
      if (event.target === backdrop && backdrop.dataset.dismissible !== "false") closeDialog(backdrop);
    });
    backdrop.addEventListener("keydown", (event) => {
      if (event.key === "Escape" && backdrop.dataset.dismissible !== "false") {
        event.preventDefault();
        closeDialog(backdrop);
        return;
      }
      if (event.key !== "Tab") return;
      const items = focusable(backdrop);
      if (!items.length) return;
      const first = items[0];
      const last = items[items.length - 1];
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        first.focus();
      }
    });
  });

  $$('[data-toast]').forEach((button) => {
    button.addEventListener("click", () => toast(button.dataset.toast));
  });

  $$('[data-state-set]').forEach((button) => {
    button.addEventListener("click", () => {
      document.documentElement.dataset.state = button.dataset.stateSet;
      $$('[data-state-set]').forEach((item) => item.removeAttribute("aria-pressed"));
      button.setAttribute("aria-pressed", "true");
    });
  });

  $$('[data-prototype-form]').forEach((form) => {
    let dirty = false;
    form.addEventListener("input", () => { dirty = true; });
    form.addEventListener("submit", (event) => {
      event.preventDefault();
      let valid = true;
      $$('[data-required]', form).forEach((field) => {
        const error = document.getElementById(`${field.id}-error`);
        if (!String(field.value || "").trim()) {
          field.setAttribute("aria-invalid", "true");
          if (error) error.hidden = false;
          valid = false;
        } else {
          field.removeAttribute("aria-invalid");
          if (error) error.hidden = true;
        }
      });
      const github = $('[data-github]', form);
      if (github && github.value && !/^https:\/\/github\.com\/[^/]+\/[^/]+\/?$/i.test(github.value.trim())) {
        github.setAttribute("aria-invalid", "true");
        const error = document.getElementById(`${github.id}-error`);
        if (error) { error.hidden = false; error.textContent = "请输入公开 GitHub 仓库地址。"; }
        valid = false;
      }
      if (!valid) {
        const firstInvalid = $('[aria-invalid="true"]', form);
        if (firstInvalid) firstInvalid.focus();
        return;
      }
      if (document.documentElement.dataset.state === "save-error") {
        const failure = $('[data-form-failure]', form);
        if (failure) failure.hidden = false;
        return;
      }
      dirty = false;
      toast(form.dataset.success || "保存成功");
    });
    form.addEventListener("click", (event) => {
      const leave = event.target.closest('[data-unsaved-demo]');
      if (leave && dirty) {
        event.preventDefault();
        openDialog("unsaved-dialog");
      }
    });
    window.addEventListener("beforeunload", (event) => {
      if (!dirty || form.dataset.beforeunload === "off") return;
      event.preventDefault();
      event.returnValue = "";
    });
  });

  $$('[data-upload]').forEach((upload) => {
    const input = $('input[type="file"]', upload);
    const progress = $(".upload__progress", upload);
    const bar = $(".upload__progress-bar", upload);
    const preview = $(".upload__preview", upload);
    const error = $(".field__error", upload);
    if (!input) return;
    input.addEventListener("change", () => {
      const file = input.files && input.files[0];
      upload.classList.remove("is-error");
      if (error) error.hidden = true;
      if (!file) return;
      const allowed = ["image/jpeg", "image/png", "image/webp"];
      if (!allowed.includes(file.type) || file.size > 5 * 1024 * 1024 || document.documentElement.dataset.state === "upload-error") {
        upload.classList.add("is-error");
        if (error) { error.hidden = false; error.textContent = "仅支持 JPEG、PNG、WebP，单张不超过 5 MB。"; }
        input.value = "";
        return;
      }
      progress.hidden = false;
      let value = 0;
      const timer = window.setInterval(() => {
        value += 20;
        bar.style.width = `${value}%`;
        bar.parentElement.setAttribute("aria-valuenow", String(value));
        if (value >= 100) {
          window.clearInterval(timer);
          const reader = new FileReader();
          reader.onload = () => {
            if (preview) { preview.src = reader.result; preview.hidden = false; }
            toast("图片上传成功");
          };
          reader.readAsDataURL(file);
        }
      }, 120);
    });
  });

  const articleSearch = $('[data-article-search]');
  const articleFilter = $('[data-article-filter]');
  const filterRows = () => {
    const query = articleSearch ? articleSearch.value.trim().toLowerCase() : "";
    const status = articleFilter ? articleFilter.value : "all";
    $$('[data-article-row]').forEach((row) => {
      const text = row.textContent.toLowerCase();
      const matchesQuery = !query || text.includes(query);
      const matchesStatus = status === "all" || row.dataset.status === status;
      row.hidden = !(matchesQuery && matchesStatus);
    });
  };
  if (articleSearch) articleSearch.addEventListener("input", filterRows);
  if (articleFilter) articleFilter.addEventListener("change", filterRows);

  let dragged = null;
  $$('[draggable="true"]').forEach((item) => {
    item.addEventListener("dragstart", () => {
      dragged = item;
      item.classList.add("is-dragging");
    });
    item.addEventListener("dragend", () => {
      item.classList.remove("is-dragging");
      dragged = null;
      const save = $('[data-save-order]');
      if (save) save.hidden = false;
    });
    item.addEventListener("dragover", (event) => {
      event.preventDefault();
      if (!dragged || dragged === item) return;
      const box = item.getBoundingClientRect();
      const after = event.clientY > box.top + box.height / 2;
      item.parentElement.insertBefore(dragged, after ? item.nextSibling : item);
    });
    const handle = $(".drag-handle", item);
    if (handle) {
      handle.addEventListener("keydown", (event) => {
        if (event.key !== "ArrowUp" && event.key !== "ArrowDown") return;
        event.preventDefault();
        const sibling = event.key === "ArrowUp" ? item.previousElementSibling : item.nextElementSibling;
        if (!sibling) return;
        if (event.key === "ArrowUp") item.parentElement.insertBefore(item, sibling);
        else item.parentElement.insertBefore(sibling, item);
        const save = $('[data-save-order]');
        if (save) save.hidden = false;
        handle.focus();
        toast("项目顺序已调整，记得保存");
      });
    }
  });

  $$('[data-retry]').forEach((button) => button.addEventListener("click", () => window.location.reload()));

  const loginForm = $('[data-login-form]');
  if (loginForm) {
    const notice = $('[data-login-error]');
    if (state === "error" && notice) notice.hidden = false;
    loginForm.addEventListener("submit", (event) => {
      event.preventDefault();
      if (document.documentElement.dataset.state === "error") {
        notice.hidden = false;
        $("input", loginForm).focus();
      } else {
        window.location.href = `articles.html${theme ? `?theme=${theme}` : ""}`;
      }
    });
  }

  $$('[data-action]').forEach((button) => {
    button.addEventListener("click", () => {
      const form = button.closest("form");
      const body = form && $("#article-body", form);
      const publishing = button.dataset.action === "publish";
      if (body) body.toggleAttribute("data-required", publishing);
      if (form) form.dataset.success = publishing ? "文章已发布" : "草稿已保存";
    });
  });

  $$('[data-project-action]').forEach((button) => {
    button.addEventListener("click", () => {
      const form = button.closest("form");
      const github = form && $("#project-github", form);
      const publishing = button.dataset.projectAction === "public";
      if (github) github.toggleAttribute("data-required", publishing);
      if (form) form.dataset.success = publishing ? "项目已公开并排在列表末尾" : "隐藏项目已保存";
    });
  });

  if (state === "session") {
    window.setTimeout(() => openDialog("session-dialog"), 180);
  }
})();
