(function () {
  "use strict";

  // Editor: write / preview / side-by-side, live preview, Ctrl+S, unsaved-changes guard.
  const form = document.querySelector(".lw-editor");
  if (form) {
    const panes = form.querySelector(".lw-editor-panes");
    const text = form.querySelector(".lw-text");
    const out = form.querySelector(".lw-preview .page");
    let dirty = false, timer = null, mode = localStorage.getItem("lw-editor-mode") || "write";

    async function preview() {
      const res = await fetch(form.dataset.preview, { method: "POST", body: text.value, credentials: "same-origin" });
      out.innerHTML = await res.text();
    }
    function setMode(m) {
      mode = m;
      localStorage.setItem("lw-editor-mode", m);
      panes.className = "lw-editor-panes lw-mode-" + m;
      form.querySelectorAll(".lw-tab").forEach(b => b.classList.toggle("lw-active", b.dataset.tab === m));
      if (m !== "write") preview();
    }
    form.querySelectorAll(".lw-tab").forEach(b => b.addEventListener("click", () => setMode(b.dataset.tab)));
    setMode(mode);
    text.addEventListener("input", () => {
      dirty = true;
      if (mode === "write") return;
      clearTimeout(timer);
      timer = setTimeout(preview, 400);
    });
    // Tab inserts spaces in lists and code instead of leaving the field.
    text.addEventListener("keydown", e => {
      if (e.key === "Tab" && !e.ctrlKey && !e.altKey) {
        e.preventDefault();
        const s = text.selectionStart, en = text.selectionEnd;
        text.setRangeText("    ", s, en, "end");
        dirty = true;
      }
      if ((e.ctrlKey || e.metaKey) && e.key === "s") {
        e.preventDefault();
        dirty = false;
        form.submit();
      }
    });
    form.addEventListener("submit", () => { dirty = false; });
    window.addEventListener("beforeunload", e => { if (dirty) { e.preventDefault(); e.returnValue = ""; } });
  }

  // Table of contents: collapsible, state remembered.
  document.querySelectorAll(".dw__toc h3.toggle").forEach(h => {
    const box = h.parentElement;
    if (localStorage.getItem("lw-toc-closed") === "1") box.classList.add("lw-closed");
    h.addEventListener("click", () => {
      box.classList.toggle("lw-closed");
      localStorage.setItem("lw-toc-closed", box.classList.contains("lw-closed") ? "1" : "0");
    });
  });

  // Keyboard: "e" edits, "/" searches (when not typing).
  document.addEventListener("keydown", e => {
    const t = e.target;
    if (t.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(t.tagName) || e.ctrlKey || e.metaKey || e.altKey) return;
    if (e.key === "e") {
      const a = document.querySelector('a[data-tool="edit"]');
      if (a) location.href = a.href;
    } else if (e.key === "/") {
      const q = document.querySelector('input[name="q"]');
      if (q) { e.preventDefault(); q.focus(); }
    }
  });
})();
