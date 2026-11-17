// One-off: compile a DokuWiki template's stylesheets the way lib/exe/css.php
// does (core styles, plugin styles, template styles, style.ini replacements,
// one LESS pass), and copy the assets it references.
//
//   node build.mjs <dokuwiki root> <template> <output _layout folder>
import fs from "node:fs";
import path from "node:path";
import less from "less";

const [dw, tpl, out] = process.argv.slice(2);
if (!dw || !tpl || !out) {
  console.error("usage: node build.mjs <dokuwiki root> <template> <output folder>");
  process.exit(1);
}
const PLUGINS = ["nspages", "wrap"]; // the only plugins whose styles show on pages

function parseIni(file) {
  const res = {};
  if (!fs.existsSync(file)) return res;
  let section = "";
  for (let line of fs.readFileSync(file, "utf8").split(/\r?\n/)) {
    line = line.replace(/;.*$/, "").trim();
    if (!line) continue;
    const m = line.match(/^\[(.+)\]$/);
    if (m) { section = m[1]; res[section] ??= {}; continue; }
    const kv = line.match(/^(.+?)\s*=\s*(.*)$/);
    if (kv) res[section][kv[1].trim()] = kv[2].trim().replace(/^"(.*)"$/, "$1");
  }
  return res;
}

const tplDir = path.join(dw, "lib/tpl", tpl);
const ini = parseIni(path.join(tplDir, "style.ini"));
const local = parseIni(path.join(dw, "conf/tpl", tpl, "style.ini"));
const repl = { ...(ini.replacements || {}), ...(local.replacements || {}) };

fs.mkdirSync(path.join(out, "assets"), { recursive: true });
const copied = new Map();

function fixUrls(css, fileDir, baseDir) {
  return css.replace(/url\(\s*(['"]?)([^'")]+)\1\s*\)/g, (all, q, u) => {
    if (/^(data:|https?:|\/\/|#|@)/.test(u) || u.includes("__")) return all;
    const clean = u.split(/[?#]/)[0];
    let src = path.resolve(fileDir, clean);
    if (!fs.existsSync(src)) src = path.resolve(baseDir, clean);
    if (!fs.existsSync(src)) { console.warn("missing asset", u, "in", fileDir); return all; }
    if (!copied.has(src)) {
      let name = path.basename(src);
      while ([...copied.values()].includes(name)) name = "x" + name;
      fs.copyFileSync(src, path.join(out, "assets", name));
      copied.set(src, name);
    }
    return `url("assets/${copied.get(src)}")`;
  });
}

function load(file, baseDir) {
  if (!fs.existsSync(file)) return "";
  let css = fs.readFileSync(file, "utf8");
  css = css.replace(/@import\s+(['"])([^'"]+)\1\s*;/g, (all, q, f) =>
    /^(https?:|\/\/)/.test(f) ? all : load(path.resolve(path.dirname(file), f), baseDir));
  css = fixUrls(css, path.dirname(file), baseDir);
  return `\n/* ${path.relative(dw, file).replace(/\\/g, "/")} */\n${css}\n`;
}

const media = { screen: "", all: "", print: "" };
for (const m of Object.keys(media)) {
  media[m] += load(path.join(dw, "lib/styles", m + ".css"), path.join(dw, "lib/styles"));
  for (const p of PLUGINS) {
    const pd = path.join(dw, "lib/plugins", p);
    const names = [m + ".css", m + ".less"];
    if (m === "screen") names.push("style.css", "style.less");
    for (const n of names) media[m] += load(path.join(pd, n), pd);
  }
  for (const [file, mt] of Object.entries(ini.stylesheets || {})) {
    if (mt === m) media[m] += load(path.join(tplDir, file), tplDir);
  }
}

let src = "";
for (const [k, v] of Object.entries(repl)) src += `@ini_${k.replace(/^__|__$/g, "")}: ${v};\n`;
src += media.all + "\n@media screen {\n" + media.screen + "\n}\n@media print {\n" + media.print + "\n}\n";
for (const [k, v] of Object.entries(repl)) src = src.split(k).join(v);

const res = await less.render(src, { math: "always", filename: path.join(tplDir, "style.less") });
fs.writeFileSync(path.join(out, "style.css"),
  `/* Compiled from the DokuWiki "${tpl}" template by lodge-wiki/tools/dokuwiki-css. */\n` + res.css);
console.log(`wrote ${path.join(out, "style.css")} (${res.css.length} bytes, ${copied.size} assets)`);
