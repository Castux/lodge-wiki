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
    if (["#write", "#preview", "#split"].includes(location.hash)) mode = location.hash.slice(1);
    // Non-page files (JSON, CSS, JS...) have no preview: always the text.
    if (isFile) mode = "write";

    let cm = null;
    const MODES = {
      markdown: { name: "gfm", fencedCodeBlockHighlighting: true },
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
    window.lwCM = cm; // for the link finder and image pasting below
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

  // Keyboard: "e" edits, "/" or Ctrl+K jumps to a page (when not typing;
  // Ctrl+K in the editor inserts a link instead, see below).
  document.addEventListener("keydown", e => {
    const t = e.target;
    const typing = t.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(t.tagName) || t.closest(".CodeMirror");
    if ((e.ctrlKey || e.metaKey) && !e.altKey && !e.shiftKey && e.key.toLowerCase() === "k" && !document.querySelector(".lw-editor")) {
      e.preventDefault();
      window.lwJump();
      return;
    }
    if (typing || e.ctrlKey || e.metaKey || e.altKey) return;
    if (e.key === "e") {
      const a = document.querySelector('a[data-tool="edit"]');
      if (a) location.href = a.href;
    } else if (e.key === "/") {
      e.preventDefault();
      window.lwJump();
    }
  });

  // A plain click on a link to the search page opens the finder instead,
  // which finds pages by title and offers the text search as its last row.
  // (Ctrl+click and the page itself still work, without JavaScript too.)
  document.addEventListener("click", e => {
    const a = e.target.closest && e.target.closest('a[href="/_/search"]');
    if (!a || e.button !== 0 || e.ctrlKey || e.metaKey || e.shiftKey || e.altKey || !window.lwJump) return;
    e.preventDefault();
    window.lwJump();
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

  // Images get the chosen layout preset (#small-right) and caption (the
  // markdown title); see docs/guide.md.
  const sizeSel = box.querySelector(".lw-media-size");
  const sideSel = box.querySelector(".lw-media-side");
  const caption = box.querySelector(".lw-media-caption");
  const insert = file => window.lwInsert(window.lwMediaMarkdown(file));

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
    btn.setAttribute("aria-pressed", !box.hidden);
    if (!box.hidden) load();
  });

  // lwUpload uploads a file to the picker's folder (the page's, by
  // default) and returns { name, url, image }; it asks before replacing
  // a file of the same name.
  window.lwUpload = async file => {
    const fd = new FormData();
    fd.append("ns", nsInput.value.trim());
    fd.append("file", file);
    let res = await fetch("/_/media", { method: "POST", body: fd, credentials: "same-origin" });
    if (res.status === 409 && confirm((await res.text()) + ". Replace it?")) {
      fd.append("overwrite", "1");
      res = await fetch("/_/media", { method: "POST", body: fd, credentials: "same-origin" });
    }
    if (!res.ok) throw new Error(await res.text());
    const info = await res.json();
    return { name: info.name, url: info.url, image: /\.(jpe?g|png|gif|webp|svg)$/i.test(info.name) };
  };
  // The markdown for a file, with the picker's size, placement and caption.
  window.lwMediaMarkdown = file => {
    if (!file.image) return `[${file.name}](${file.url})`;
    const alt = file.name.replace(/\.[^.]+$/, "").replace(/[-_]/g, " ");
    const preset = [sizeSel.value, sideSel.value].filter(Boolean).join("-");
    const cap = caption.value.trim().replace(/"/g, "'");
    caption.value = "";
    return `![${alt}](${file.url}${preset ? "#" + preset : ""}${cap ? ` "${cap}"` : ""})`;
  };

  box.querySelector(".lw-media-file").addEventListener("change", async ev => {
    for (const file of ev.target.files) {
      msg.textContent = "Uploading " + file.name + "…";
      try {
        const info = await window.lwUpload(file);
        insert(info);
        msg.textContent = "Uploaded " + info.name + " and inserted it.";
      } catch (e) {
        msg.textContent = "Upload failed: " + e.message;
      }
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

// Page finder: fuzzy search on page titles and addresses. Opened by "/"
// or Ctrl+K to jump to a page, and in the editor by "[[" or Ctrl+K to
// insert a link (standard markdown, the page's address).
(function () {
  "use strict";
  const esc = s => String(s).replace(/[&<>"]/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[c]);
  const fold = s => s.normalize("NFD").replace(/[̀-ͯ]/g, "").toLowerCase();
  let pages = null;
  async function loadPages() {
    if (!pages) {
      const res = await fetch("/_/titles", { credentials: "same-origin" });
      pages = res.ok ? (await res.json()).map(p => ({ ...p, ft: fold(p.t), fid: fold(p.id) })) : [];
    }
    return pages;
  }

  // Score of one query word in a string: a substring beats a scattered
  // match; the start of the string or of a word scores higher.
  function wordScore(w, s) {
    const i = s.indexOf(w);
    if (i >= 0) return 100 + (i === 0 ? 60 : /[\s\-\/'(]/.test(s[i - 1]) ? 40 : 0) - Math.min(i, 30) / 3;
    let j = 0, run = 0, score = 0;
    for (const c of w) {
      const k = s.indexOf(c, j);
      if (k < 0) return 0;
      run = k === j ? run + 1 : 0;
      score += 1 + run * 2;
      j = k + 1;
    }
    return score;
  }
  function rank(q, list) {
    const words = fold(q).split(/\s+/).filter(Boolean);
    if (!words.length) return [];
    const out = [];
    for (const p of list) {
      let total = 0;
      for (const w of words) {
        const s = Math.max(wordScore(w, p.ft), wordScore(w, p.fid) * 0.6);
        if (!s) { total = 0; break; }
        total += s;
      }
      if (total) out.push([total - p.t.length / 50, p]);
    }
    return out.sort((a, b) => b[0] - a[0]).slice(0, 12).map(x => x[1]);
  }

  // open shows the finder. opts: placeholder, query (initial text),
  // extra(q) -> [{label, run}] rows after the matches, pick(page), close().
  function open(opts) {
    const old = document.querySelector(".lw-finder");
    if (old) old.remove();
    const box = document.createElement("div");
    box.className = "lw-finder";
    box.innerHTML = '<div class="lw-finder-box" role="dialog" aria-label="Find a page"><input type="text" spellcheck="false" autocomplete="off" placeholder="' +
      esc(opts.placeholder) + '"><ul></ul><p class="lw-quiet">↑↓ to choose · Enter · Esc to close</p></div>';
    document.body.appendChild(box);
    const input = box.querySelector("input"), ul = box.querySelector("ul");
    let rows = [], sel = 0, closed = false;
    const close = picked => {
      if (closed) return;
      closed = true;
      box.remove();
      if (!picked && opts.close) opts.close();
    };
    const render = () => {
      ul.innerHTML = rows.map((r, i) => '<li class="' + (i === sel ? "lw-sel" : "") + (r.page ? "" : " lw-finder-extra") + '" data-i="' + i + '">' +
        (r.page ? esc(r.page.t) + ' <span class="lw-quiet">' + esc(r.page.u) + "</span>" : esc(r.label)) + "</li>").join("");
      const cur = ul.querySelector(".lw-sel");
      if (cur) cur.scrollIntoView({ block: "nearest" });
    };
    const update = async () => {
      const q = input.value.trim();
      const found = q ? rank(q, await loadPages()) : [];
      if (closed || q !== input.value.trim()) return;
      rows = found.map(p => ({ page: p })).concat(q && opts.extra ? opts.extra(q) : []);
      sel = 0;
      render();
    };
    const choose = i => {
      const r = rows[i];
      if (!r) return;
      close(true);
      if (r.page) opts.pick(r.page); else r.run();
    };
    input.addEventListener("input", update);
    input.addEventListener("keydown", e => {
      // the arrows wrap around: Up from the first row reaches the last (the text search)
      if (e.key === "ArrowDown" && rows.length) { e.preventDefault(); sel = (sel + 1) % rows.length; render(); }
      else if (e.key === "ArrowUp" && rows.length) { e.preventDefault(); sel = (sel - 1 + rows.length) % rows.length; render(); }
      else if (e.key === "Enter") { e.preventDefault(); choose(sel); }
      else if (e.key === "Escape") { e.preventDefault(); close(false); }
    });
    ul.addEventListener("mousedown", e => {
      const li = e.target.closest("li");
      if (li) { e.preventDefault(); choose(+li.dataset.i); }
    });
    box.addEventListener("mousedown", e => { if (e.target === box) close(false); });
    input.value = opts.query || "";
    input.focus();
    loadPages().then(update);
  }

  // Jump to a page; the last row searches all pages' text.
  window.lwJump = () => open({
    placeholder: "Find a page, or search the text…",
    pick: p => { location.href = p.u; },
    extra: q => [{ label: "Search all pages for “" + q + "”", run: () => { location.href = "/_/search?q=" + encodeURIComponent(q); } }],
  });

  // Editor: insert a link to a page, with the selected text (if any) as
  // the link's text; "[[" opens it too (and is replaced by the link).
  const form = document.querySelector(".lw-editor");
  const text = form && form.querySelector(".lw-text");
  if (!form || !text || form.classList.contains("lw-file") || text.readOnly) return;
  const cm = window.lwCM;
  const m = location.pathname.replace(/^\/_\/edit\//, "").match(/^(.*)\//);
  const folder = m ? m[1] : "";
  const slug = s => fold(s).replace(/[^a-z0-9._]+/g, "-").replace(/^[-._]+|[-._]+$/g, "");

  function linkFinder(dropBrackets) {
    const selected = cm ? cm.getSelection() : text.value.slice(text.selectionStart, text.selectionEnd);
    const done = () => { if (cm) cm.focus(); else text.focus(); };
    const insert = (label, url) => {
      if (dropBrackets) { // remove the "[[" just typed
        if (cm) {
          const c = cm.getCursor();
          cm.replaceRange("", { line: c.line, ch: c.ch - 2 }, c);
        } else {
          const p = text.selectionStart;
          text.setRangeText("", p - 2, p, "end");
        }
      }
      window.lwInsert("[" + (selected || label).replace(/[\[\]]/g, "") + "](" + url + ")");
      done();
    };
    open({
      placeholder: "Link to a page…",
      query: selected,
      pick: p => insert(p.t, p.u),
      // A page that does not exist yet: a link to it in this page's folder
      // (shown as missing until someone writes it).
      extra: q => [{ label: "New page “" + q + "” in " + (folder || "the top folder"), run: () => insert(q, "/" + (folder ? folder + "/" : "") + slug(q)) }],
      close: done,
    });
  }

  if (cm) {
    cm.addKeyMap({ "Ctrl-K": () => linkFinder(false), "Cmd-K": () => linkFinder(false) });
    cm.on("inputRead", (c, change) => {
      if (change.text.join("") !== "[") return;
      const cur = c.getCursor();
      if (c.getRange({ line: cur.line, ch: cur.ch - 2 }, cur) === "[[") linkFinder(true);
    });
  } else {
    text.addEventListener("keydown", e => {
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "k") { e.preventDefault(); linkFinder(false); }
    });
    text.addEventListener("input", e => {
      const p = text.selectionStart;
      if (e.data === "[" && text.value.slice(p - 2, p) === "[[") linkFinder(true);
    });
  }

  // The same few calls on CodeMirror and on the plain textarea, by offsets.
  const ed = {
    value: () => cm ? cm.getValue() : text.value,
    sel: () => cm ? [cm.indexFromPos(cm.getCursor("from")), cm.indexFromPos(cm.getCursor("to"))] : [text.selectionStart, text.selectionEnd],
    // replace [a, b) with s, then select [selA, selB) of the new text
    replace(a, b, s, selA, selB) {
      if (cm) {
        cm.operation(() => {
          cm.replaceRange(s, cm.posFromIndex(a), cm.posFromIndex(b), "+lwformat");
          cm.setSelection(cm.posFromIndex(selA), cm.posFromIndex(selB));
        });
        cm.focus();
      } else {
        text.setRangeText(s, a, b);
        text.setSelectionRange(selA, selB);
        text.focus();
        text.dispatchEvent(new Event("input"));
      }
    },
  };

  // Formatting buttons (and Ctrl+B, Ctrl+I): bold and italic wrap the
  // selection, or a placeholder to type over, and unwrap it when it is
  // already wrapped; the heading button cycles the cursor's line through
  // #, ##, ### and back to plain text; quotes and lists add or remove their mark at
  // the start of the selected lines; the table button inserts a small table.
  function wrap(mk, placeholder) {
    const v = ed.value();
    let [a, b] = ed.sel();
    while (a < b && /\s/.test(v[a])) a++; // a double-clicked word brings its space
    while (b > a && /\s/.test(v[b - 1])) b--;
    const n = mk.length;
    const stars = (i, d) => { let k = 0; while (v[d < 0 ? i - 1 - k : i + k] === "*") k++; return k; };
    const marked = k => n === 1 ? k % 2 === 1 : k >= 2; // * is italic in *x* and ***x***, not in **x**
    if (marked(stars(a, -1)) && marked(stars(b, 1))) return ed.replace(a - n, b + n, v.slice(a, b), a - n, b - n);
    if (b - a > 2 * n && marked(stars(a, 1)) && marked(stars(b, -1))) return ed.replace(a, b, v.slice(a + n, b - n), a, b - 2 * n);
    const inner = a === b ? placeholder : v.slice(a, b);
    ed.replace(a, b, mk + inner + mk, a + n, a + n + inner.length);
  }

  const PREFIX = {
    quote: { re: /^>\s?/, add: () => "> " },
    list: { re: /^([-*+]|\d+[.)])\s+/, add: () => "- " },
    numbered: { re: /^([-*+]|\d+[.)])\s+/, add: i => i + 1 + ". " },
  };
  function prefix(kind) {
    const { re, add } = PREFIX[kind], v = ed.value();
    let [a, b] = ed.sel();
    if (b > a && v[b - 1] === "\n") b--; // a selection ending at a line's start leaves that line alone
    const start = v.lastIndexOf("\n", a - 1) + 1;
    let end = v.indexOf("\n", b);
    if (end < 0) end = v.length;
    const lines = v.slice(start, end).split("\n");
    const own = kind === "list" ? /^[-*+]\s/ : kind === "numbered" ? /^\d+[.)]\s/ : re;
    const used = lines.filter(l => l.trim() || lines.length === 1);
    const off = used.every(l => own.test(l)); // all marked already: remove
    let k = 0;
    const out = lines.map(l => {
      if (!l.trim() && lines.length > 1) return l;
      const bare = l.replace(re, "");
      return off ? bare : add(k++) + bare;
    }).join("\n");
    if (a === b && lines.length === 1) { // keep the cursor where it was in the text
      const at = Math.max(start, a + out.length - (end - start));
      return ed.replace(start, end, out, at, at);
    }
    ed.replace(start, end, out, start, start + out.length);
  }

  function heading() {
    const v = ed.value(), [a, b] = ed.sel();
    const start = v.lastIndexOf("\n", a - 1) + 1;
    let end = v.indexOf("\n", a);
    if (end < 0) end = v.length;
    const line = v.slice(start, end), m = line.match(/^(#{1,6})\s+/) || ["", ""];
    const depth = m[1].length >= 3 ? 0 : m[1].length + 1, out = (depth ? "#".repeat(depth) + " " : "") + line.slice(m[0].length);
    const keep = p => Math.max(start, p + out.length - line.length); // the cursor stays on the same text
    ed.replace(start, end, out, keep(a), keep(b));
  }

  function table() {
    const v = ed.value(), b = ed.sel()[1];
    let at = v.indexOf("\n", b);
    if (at < 0) at = v.length;
    const before = v.slice(0, at), after = v.slice(at);
    const pre = !before.trim() ? "" : before.endsWith("\n\n") ? "" : before.endsWith("\n") || !before.slice(before.lastIndexOf("\n") + 1).trim() ? "\n" : "\n\n";
    const post = after.startsWith("\n\n") || !after.trim() ? "" : "\n";
    const snippet = "| Column | Column |\n| ------ | ------ |\n|        |        |" + post;
    ed.replace(at, at, pre + snippet, at + pre.length + 2, at + pre.length + 8);
  }

  const ACTIONS = {
    bold: () => wrap("**", "bold text"), italic: () => wrap("*", "italic text"),
    link: () => linkFinder(false), table,
    heading, quote: () => prefix("quote"),
    list: () => prefix("list"), numbered: () => prefix("numbered"),
  };
  form.querySelectorAll("[data-fmt]").forEach(btn => {
    btn.addEventListener("mousedown", e => e.preventDefault()); // keep the selection
    btn.addEventListener("click", () => ACTIONS[btn.dataset.fmt]());
  });
  if (cm) {
    cm.addKeyMap({ "Ctrl-B": ACTIONS.bold, "Cmd-B": ACTIONS.bold, "Ctrl-I": ACTIONS.italic, "Cmd-I": ACTIONS.italic });
  } else {
    text.addEventListener("keydown", e => {
      const act = (e.ctrlKey || e.metaKey) && !e.altKey && { b: ACTIONS.bold, i: ACTIONS.italic }[e.key.toLowerCase()];
      if (act) { e.preventDefault(); act(); }
    });
  }

  // Tables tidy themselves as you write: cells padded so the columns line
  // up in the text. The table being written in waits until the cursor
  // leaves it (padding a cell while you type in it would eat your spaces).
  const width = s => [...s].length;
  function cellsOf(line) {
    let s = line.trim().slice(1);
    if (s.endsWith("|") && !s.endsWith("\\|")) s = s.slice(0, -1);
    return s.split(/(?<!\\)\|/).map(c => c.trim());
  }
  const DELIM = /^\s*\|(\s*:?-+:?\s*\|)*\s*:?-+:?\s*\|?\s*$/;
  function tidyTable(rows, indent) {
    const cells = rows.map(cellsOf), n = cells[0].length;
    if (cells[1].length !== n || cells.some(r => r.length > n)) return null; // not a table, or cells GFM would drop
    const align = cells[1].map(c => c.startsWith(":") && c.endsWith(":") ? "c" : c.endsWith(":") ? "r" : c.startsWith(":") ? "l" : "");
    const w = align.map((_, k) => Math.max(3, ...cells.map((r, i) => i === 1 ? 0 : width(r[k] || ""))));
    return cells.map((r, i) => indent + "| " + align.map((al, k) => {
      if (i === 1) {
        const dashes = "-".repeat(w[k] - (al === "c" ? 2 : al ? 1 : 0));
        return al === "c" ? ":" + dashes + ":" : al === "l" ? ":" + dashes : al === "r" ? dashes + ":" : dashes;
      }
      const c = r[k] || "", pad = w[k] - width(c);
      if (al === "r") return " ".repeat(pad) + c;
      if (al === "c") return " ".repeat(pad >> 1) + c + " ".repeat(pad - (pad >> 1));
      return c + " ".repeat(pad);
    }).join(" | ") + " |");
  }
  // Returns the changed lines ({index: new text}) and whether the table
  // under the cursor (line `skip`) would have changed.
  function tidyTables(lines, skip) {
    const changes = {};
    let fence = null, waiting = false;
    for (let i = 0; i < lines.length; i++) {
      const f = lines[i].match(/^\s*(```+|~~~+)/);
      if (f) {
        if (!fence) fence = f[1];
        else if (lines[i].trim().startsWith(fence)) fence = null;
        continue;
      }
      if (fence || !lines[i].trim().startsWith("|") || !DELIM.test(lines[i + 1] || "")) continue;
      let j = i + 2;
      while (j < lines.length && lines[j].trim().startsWith("|")) j++;
      const rows = lines.slice(i, j), tidy = tidyTable(rows, lines[i].match(/^\s*/)[0]);
      const differs = tidy && tidy.some((l, k) => l !== rows[k]);
      if (differs && skip >= i && skip < j) waiting = true;
      else if (differs) tidy.forEach((l, k) => { if (l !== rows[k]) changes[i + k] = l; });
      i = j - 1;
    }
    return { changes, waiting };
  }
  let tidyTimer = null, waiting = false;
  function tidyNow() {
    const v = ed.value(), lines = v.split("\n");
    const head = cm ? cm.getCursor().line : v.slice(0, text.selectionEnd).split("\n").length - 1;
    const res = tidyTables(lines, head);
    waiting = res.waiting;
    const idx = Object.keys(res.changes).map(Number);
    if (!idx.length) return;
    if (cm) {
      cm.operation(() => idx.forEach(i => cm.replaceRange(res.changes[i], { line: i, ch: 0 }, { line: i, ch: lines[i].length }, "+lwtable")));
      return;
    }
    let shift = 0; // the textarea: keep the selection where it was
    idx.forEach(i => { if (i < head) shift += res.changes[i].length - lines[i].length; });
    const [a, b] = ed.sel();
    idx.forEach(i => { lines[i] = res.changes[i]; });
    text.value = lines.join("\n");
    text.setSelectionRange(a + shift, b + shift);
    text.dispatchEvent(new Event("input"));
  }
  const tidySoon = () => { clearTimeout(tidyTimer); tidyTimer = setTimeout(tidyNow, 400); };
  if (cm) {
    cm.on("changes", (c, changes) => {
      if (changes.some(ch => ch.origin === "undo" || ch.origin === "redo")) waiting = false; // let undo undo
      else if (!changes.every(ch => ch.origin === "+lwtable")) tidySoon();
    });
    cm.on("cursorActivity", () => { if (waiting) tidySoon(); });
  } else {
    text.addEventListener("input", e => { if (e.isTrusted) tidySoon(); });
    ["keyup", "click"].forEach(t => text.addEventListener(t, () => { if (waiting) tidySoon(); }));
  }

  // Images (and other files) pasted or dropped into the editor are
  // uploaded to the page's media folder and inserted where they land.
  // A placeholder marks the spot while the upload runs.
  if (!window.lwUpload) return;
  const page = location.pathname.split("/").pop() || "image";
  const stamp = () => { // local time, 20261118-014443
    const d = new Date(), p = n => String(n).padStart(2, "0");
    return d.getFullYear() + p(d.getMonth() + 1) + p(d.getDate()) + "-" + p(d.getHours()) + p(d.getMinutes()) + p(d.getSeconds());
  };
  async function uploadAll(files) {
    for (let file of files) {
      // Pasted screenshots are all called "image.png": name them after the page.
      if (/^image\.(png|jpe?g|gif|webp)$/i.test(file.name)) {
        file = new File([file], page + "-" + stamp() + "." + file.name.split(".").pop(), { type: file.type });
      }
      const mark = "![Uploading " + file.name + "…]()";
      window.lwInsert(mark);
      let md = "";
      try {
        md = window.lwMediaMarkdown(await window.lwUpload(file));
      } catch (e) {
        alert("Upload of " + file.name + " failed: " + e.message);
      }
      if (cm) {
        const i = cm.getValue().indexOf(mark);
        if (i >= 0) cm.replaceRange(md, cm.posFromIndex(i), cm.posFromIndex(i + mark.length));
      } else {
        text.value = text.value.replace(mark, md);
        text.dispatchEvent(new Event("input"));
      }
    }
  }
  const filesOf = dt => (dt && dt.files ? Array.from(dt.files) : []);
  const onPaste = e => {
    const files = filesOf(e.clipboardData);
    if (files.length) { e.preventDefault(); uploadAll(files); }
  };
  if (cm) {
    cm.on("paste", (c, e) => onPaste(e));
    cm.on("drop", (c, e) => {
      const files = filesOf(e.dataTransfer);
      if (!files.length) return;
      e.preventDefault();
      c.setCursor(c.coordsChar({ left: e.clientX, top: e.clientY }));
      uploadAll(files);
    });
  } else {
    text.addEventListener("paste", onPaste);
    text.addEventListener("drop", e => {
      const files = filesOf(e.dataTransfer);
      if (files.length) { e.preventDefault(); uploadAll(files); }
    });
  }
})();
