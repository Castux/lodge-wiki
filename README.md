# lodge-wiki

A custom wiki for the Chaser's Lodge group: one Go binary, markdown pages and JSON data files in a git repository, every write a commit.

Built for this group, not as a generic product. The rule: anything generic is core Go code; anything specific to the group (pages, data files, layouts, widgets) lives in the content repo.

- Plan and architecture: [PLAN.md](PLAN.md)
- Calls made along the way: [DECISIONS.md](DECISIONS.md)
- Page syntax: [docs/syntax.md](docs/syntax.md)
- Content repo: [`lodge-data`](https://github.com/Castux/lodge-data), checked out as the sibling folder `../lodge-data`
- Current DokuWiki copy (migration source): `../public_html`

## Run it

Needs Go 1.25+ and `git` on the PATH.

```sh
go run . --root ../lodge-data
```

Then open http://localhost:8080. On the first start it creates an `admin` account and prints its password; change it with:

```sh
go run . --root ../lodge-data --set-password admin
```

To bring over the DokuWiki accounts (everyone keeps their password):

```sh
go run . --root ../lodge-data --import-users ../public_html/conf/users.auth.php
```

Accounts, the session key and the scaled-image cache live in a state folder outside the content repo (default `../lodge-state`, set with `--state`). They are never committed.

| Flag | Default | Meaning |
| --- | --- | --- |
| `--root` | `../lodge-data` | content repository |
| `--state` | `<root>/../lodge-state` | accounts, session key, image cache |
| `--addr` | `localhost:8080` | listen address |
| `--title` | `The Chasers' Lodge` | site title |
| `--public` | off | allow reading without logging in |
| `--as <user>` | off | development only: act as that user on every request |

Media files (images, sounds, pdfs) are in `../lodge-data/_media/`, which is git-ignored: copy them separately when moving the wiki.

## Layout of this repo

| Path | What |
| --- | --- |
| `main.go` | command line |
| `internal/wiki` | page ids, the content store, the git write queue, the in-memory index |
| `internal/render` | markdown to HTML (goldmark), shaped like DokuWiki's output so the old CSS applies; `list` blocks |
| `internal/server` | HTTP: pages, editor, history, diff, search, rename, media, the widget API; built-in UI in `ui/` and `static/` |
| `migrate/` | the one-off DokuWiki migration script and its report |
| `tools/dokuwiki-css` | compiles the old DokuWiki templates' LESS into the layouts' `style.css` |
| `tools/checkrender` | renders every page and lists leftover syntax and broken links |

## Widget API

Widgets (`_widgets/<name>/widget.js` in the content repo) run in the reader's browser and use `window.wiki`:

```js
wiki.widget("name", (element, config) => { ... });   // config = YAML from the ```widget block
const { data, rev } = await wiki.read("ns/file.json"); // or a page's markdown
const newRev = await wiki.write("ns/file.json", data, rev, { summary: "…", widget: "name" });
const { pages, namespaces } = await wiki.list("ns");
```

Every write is a git commit as the logged-in reader. A write based on an outdated `rev` fails with `err.conflict = true` instead of overwriting; reload and retry. Writes are limited to 30 a minute per user. `GET /_/log?since=YYYY-MM-DD` returns the commit history as JSON.
