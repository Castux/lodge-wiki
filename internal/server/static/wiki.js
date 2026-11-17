// wiki.js: the API widgets use to read and write wiki files.
// Every write is a git commit made as the logged-in reader; a write must
// name the revision it started from, so concurrent edits are never lost.
(function () {
  "use strict";

  async function read(path) {
    const res = await fetch("/_/raw/" + path, { credentials: "same-origin" });
    if (!res.ok) throw new Error("read " + path + ": " + res.status);
    const rev = res.headers.get("X-Revision") || "";
    const text = await res.text();
    const data = path.endsWith(".json") ? JSON.parse(text) : text;
    return { data, rev };
  }

  // write(path, data, rev, {summary, widget}) -> new revision.
  // Throws an error with .conflict = true if the file changed since rev.
  async function write(path, data, rev, opts) {
    opts = opts || {};
    const body = typeof data === "string" ? data : JSON.stringify(data, null, 2) + "\n";
    const headers = { "X-Base-Revision": rev || "new" };
    if (opts.summary) headers["X-Summary"] = opts.summary;
    if (opts.widget) headers["X-Widget"] = opts.widget;
    const res = await fetch("/_/raw/" + path, { method: "PUT", credentials: "same-origin", headers, body });
    if (res.status === 409) {
      const err = new Error("conflict: " + path + " changed since it was loaded");
      err.conflict = true;
      err.rev = res.headers.get("X-Revision");
      throw err;
    }
    if (!res.ok) throw new Error("write " + path + ": " + res.status + " " + (await res.text()));
    return res.headers.get("X-Revision") || "";
  }

  async function list(ns) {
    const res = await fetch("/_/list?ns=" + encodeURIComponent(ns || ""), { credentials: "same-origin" });
    if (!res.ok) throw new Error("list " + ns + ": " + res.status);
    return res.json();
  }

  // Widgets register a function called for each element showing them.
  const widgets = {};
  function widget(name, init) {
    widgets[name] = init;
    document.querySelectorAll('[data-widget="' + name + '"]').forEach(el => {
      let cfg = {};
      try { cfg = JSON.parse(el.dataset.config || "{}") || {}; } catch (e) {}
      try { init(el, cfg); } catch (e) { el.textContent = "Widget error: " + e.message; console.error(e); }
    });
  }

  window.wiki = { read, write, list, widget };
})();
