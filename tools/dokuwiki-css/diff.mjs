// Keep only the rules of <child.css> that are not identical in <base.css>,
// so a namespace layout can be loaded on top of its parent's.
//   node diff.mjs base.css child.css out.css
import fs from "node:fs";
import postcss from "postcss";

const [baseFile, childFile, outFile] = process.argv.slice(2);
const key = node => {
  let ctx = "";
  for (let p = node.parent; p && p.type !== "root"; p = p.parent) ctx = `@${p.name} ${p.params}|` + ctx;
  return ctx + node.toString().replace(/\s+/g, " ");
};
const base = postcss.parse(fs.readFileSync(baseFile, "utf8"));
const seen = new Set();
base.walk(n => { if (n.type === "rule" || n.type === "atrule" && n.name === "font-face") seen.add(key(n)); });

const child = postcss.parse(fs.readFileSync(childFile, "utf8"));
child.walk(n => { if ((n.type === "rule" || n.type === "atrule" && n.name === "font-face") && seen.has(key(n))) n.remove(); });
child.walk(n => { if (n.type === "comment") n.remove(); });
// drop now-empty at-rules
let changed = true;
while (changed) {
  changed = false;
  child.walkAtRules(a => { if (a.nodes && a.nodes.length === 0) { a.remove(); changed = true; } });
}
fs.writeFileSync(outFile, "/* Differences from the root layout, compiled from the DokuWiki \"skarn\" template. */\n" + child.toString().replace(/\n{3,}/g, "\n\n"));
console.log("wrote", outFile, fs.statSync(outFile).size, "bytes");
