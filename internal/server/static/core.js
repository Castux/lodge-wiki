(function () {
  "use strict";

  // Editor: write / preview / side-by-side, live preview, Ctrl+S, unsaved-changes guard.
  // Uses CodeMirror (syntax highlighting) when it is loaded, else the plain textarea.
  const form = document.querySelector(".lw-editor");
  if (form) {
    const panes = form.querySelector(".lw-editor-panes");
    const text = form.querySelector(".lw-text");
    const out = form.querySelector(".lw-preview .page");
    const isFile = form.classList.contains("lw-file");
    let dirty = false, timer = null, mode = localStorage.getItem("lw-editor-mode") || "write";
    // Non-page files (JSON, CSS, JS...) have no preview: always the text.
    if (isFile) mode = "write";

    let cm = null;
    const MODES = {
      markdown: { name: "yaml-frontmatter", base: { name: "gfm", fencedCodeBlockHighlighting: true } },
      javascript: "javascript", json: { name: "javascript", json: true }, css: "css",
      html: "htmlmixed", yaml: "yaml", svg: "xml", text: null,
    };
    const save = () => { dirty = false; if (cm) cm.save(); form.submit(); };
    if (window.CodeMirror) {
      // `list` and `widget` blocks hold YAML settings.
      const find = CodeMirror.findModeByName;
      CodeMirror.findModeByName = n => (n === "list" || n === "widget" ? find("yaml") : find(n));
      cm = CodeMirror.fromTextArea(text, {
        mode: MODES[form.dataset.lang || "markdown"],
        readOnly: text.readOnly,
        lineWrapping: !isFile,
        lineNumbers: isFile,
        styleActiveLine: isFile,
        matchBrackets: true,
        autoCloseBrackets: isFile,
        indentUnit: isFile ? 2 : 4,
        tabSize: 4,
        indentWithTabs: false,
        viewportMargin: 50,
        extraKeys: {
          Tab: c => c.somethingSelected() ? c.indentSelection("add") : c.replaceSelection(" ".repeat(c.getOption("indentUnit"))),
          "Shift-Tab": c => c.indentSelection("subtract"),
          "Ctrl-S": save, "Cmd-S": save,
        },
      });
      cm.on("change", () => { cm.save(); text.dispatchEvent(new Event("input")); });
      cm.focus();
    }
    window.lwInsert = snippet => {
      if (cm) { cm.replaceSelection(snippet); cm.focus(); return; }
      text.setRangeText(snippet, text.selectionStart, text.selectionEnd, "end");
      text.focus();
      text.dispatchEvent(new Event("input"));
    };

    async function preview() {
      const res = await fetch(form.dataset.preview, { method: "POST", body: text.value, credentials: "same-origin" });
      out.innerHTML = await res.text();
    }
    function setMode(m) {
      mode = m;
      if (!isFile) localStorage.setItem("lw-editor-mode", m);
      panes.className = "lw-editor-panes lw-mode-" + m;
      form.querySelectorAll(".lw-tab").forEach(b => b.classList.toggle("lw-active", b.dataset.tab === m));
      if (m !== "write") preview();
      if (cm) cm.refresh();
    }
    form.querySelectorAll(".lw-tab").forEach(b => b.addEventListener("click", () => setMode(b.dataset.tab)));
    setMode(mode);
    text.addEventListener("input", () => {
      dirty = true;
      if (mode === "write") return;
      clearTimeout(timer);
      timer = setTimeout(preview, 400);
    });
    if (!cm) {
      // Plain textarea: Tab inserts spaces, Ctrl+S saves.
      text.addEventListener("keydown", e => {
        if (e.key === "Tab" && !e.ctrlKey && !e.altKey) {
          e.preventDefault();
          text.setRangeText("    ", text.selectionStart, text.selectionEnd, "end");
          dirty = true;
        }
        if ((e.ctrlKey || e.metaKey) && e.key === "s") { e.preventDefault(); save(); }
      });
    }
    form.addEventListener("submit", () => { dirty = false; });
    window.addEventListener("beforeunload", e => { if (dirty) { e.preventDefault(); e.returnValue = ""; } });
  }

  // Keyboard: "e" edits, "/" searches (when not typing).
  document.addEventListener("keydown", e => {
    const t = e.target;
    if (t.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(t.tagName) || e.ctrlKey || e.metaKey || e.altKey) return;
    if (e.key === "e") {
      const a = document.querySelector('a[data-tool="edit"]');
      if (a) location.href = a.href;
    } else if (e.key === "/") {
      const q = document.querySelector('input[name="q"]');
      e.preventDefault();
      if (q) q.focus(); else location.href = "/_/search";
    }
  });
})();

// Editor: image picker and upload (files go to _media/<folder>/).
(function () {
  "use strict";
  const box = document.querySelector(".lw-media");
  const btn = document.querySelector(".lw-media-btn");
  const text = document.querySelector(".lw-text");
  if (!box || !btn || !text) return;
  const nsInput = box.querySelector(".lw-media-ns");
  const list = box.querySelector(".lw-media-list");
  const msg = box.querySelector(".lw-media-msg");
  const esc = s => String(s).replace(/[&<>"]/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[c]);

  function insert(file) {
    const alt = file.name.replace(/\.[^.]+$/, "").replace(/[-_]/g, " ");
    const snippet = file.image ? `![${alt}](${file.url})` : `[${file.name}](${file.url})`;
    window.lwInsert(snippet);
  }

  async function load() {
    const ns = nsInput.value.trim().replace(/^\/+|\/+$/g, "");
    list.innerHTML = "Loading…";
    const res = await fetch("/_/media?ns=" + encodeURIComponent(ns), { credentials: "same-origin" });
    if (!res.ok) { list.textContent = "Cannot list this folder."; return; }
    const data = await res.json();
    const parts = [];
    if (ns) parts.push(`<button type="button" class="lw-media-folder" data-ns="${esc(ns.split("/").slice(0, -1).join("/"))}">⬑ up</button>`);
    for (const f of data.folders) parts.push(`<button type="button" class="lw-media-folder" data-ns="${esc((ns ? ns + "/" : "") + f)}">📁 ${esc(f)}</button>`);
    for (const f of data.files) {
      parts.push(`<button type="button" class="lw-media-item" title="${esc(f.name)}" data-file='${esc(JSON.stringify(f))}'>` +
        (f.image ? `<img src="${esc(f.url)}?w=110" loading="lazy" alt="">` : `<span class="lw-media-icon">📄</span>`) +
        `<span>${esc(f.name)}</span></button>`);
    }
    list.innerHTML = parts.join("") || "<p class='lw-quiet'>No files in this folder yet.</p>";
  }

  list.addEventListener("click", ev => {
    const folder = ev.target.closest(".lw-media-folder");
    if (folder) { nsInput.value = folder.dataset.ns; load(); return; }
    const item = ev.target.closest(".lw-media-item");
    if (item) insert(JSON.parse(item.dataset.file));
  });
  nsInput.addEventListener("change", load);
  btn.addEventListener("click", () => {
    box.hidden = !box.hidden;
    btn.classList.toggle("lw-active", !box.hidden);
    if (!box.hidden) load();
  });

  box.querySelector(".lw-media-file").addEventListener("change", async ev => {
    for (const file of ev.target.files) {
      msg.textContent = "Uploading " + file.name + "…";
      const fd = new FormData();
      fd.append("ns", nsInput.value.trim());
      fd.append("file", file);
      let res = await fetch("/_/media", { method: "POST", body: fd, credentials: "same-origin" });
      if (res.status === 409 && confirm((await res.text()) + ". Replace it?")) {
        fd.append("overwrite", "1");
        res = await fetch("/_/media", { method: "POST", body: fd, credentials: "same-origin" });
      }
      if (!res.ok) { msg.textContent = "Upload failed: " + (await res.text()); continue; }
      const info = await res.json();
      insert({ name: info.name, url: info.url, image: /\.(jpe?g|png|gif|webp|svg)$/i.test(info.name) });
      msg.textContent = "Uploaded " + info.name + " and inserted it.";
    }
    ev.target.value = "";
    load();
  });
})();

// "copy address" buttons (Files view, media).
document.addEventListener("click", async ev => {
  const b = ev.target.closest(".lw-copy");
  if (!b) return;
  try {
    await navigator.clipboard.writeText(b.dataset.copy);
    const old = b.textContent;
    b.textContent = "copied!";
    setTimeout(() => { b.textContent = old; }, 1200);
  } catch (e) {
    prompt("Copy this address:", b.dataset.copy);
  }
});
