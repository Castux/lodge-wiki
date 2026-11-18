# lodge-wiki

A custom wiki for the Chasers' Lodge group: one Go binary, markdown pages and JSON data files in a git repository, every write a commit.

Built for this group, not as a generic product. The rule: anything generic is core Go code; anything specific to the group (pages, data files, layouts, widgets) lives in the content repo.

- Plan and architecture: [PLAN.md](PLAN.md)
- Calls made along the way: [DECISIONS.md](DECISIONS.md)
- User guide (page names, syntax, listings, widgets), also served at `/_/help`: [docs/guide.md](docs/guide.md)
- Content repo: [`lodge-data`](https://github.com/Castux/lodge-data), checked out as the sibling folder `../lodge-data`
- Current DokuWiki copy (migration source, and catching up with the live wiki until the switch-over): `../public_html`, see [migrate/SYNC.md](migrate/SYNC.md)

## Run it

Needs Go 1.27+ (the version in `go.mod`) and `git` on the PATH.

```sh
go run . --root ../lodge-data
```

Then open http://localhost:8080. On the first start it creates an `admin` account and prints its password; change it with:

```sh
go run . --root ../lodge-data --set-password admin
```

Everything that matters (pages, data, layouts, widgets, accounts in `_config/users.json`) is in the content repo; media is in `_media/` next to it. The state folder (default `../lodge-state`, set with `--state`) only holds the session key and the scaled-image cache: it can be deleted at any time (everyone is logged out). Keep the content repo private: it holds password hashes and emails.

| Flag | Default | Meaning |
| --- | --- | --- |
| `--root` | `../lodge-data` | content repository |
| `--state` | `<root>/../lodge-state` | session key, image cache (disposable) |
| `--addr` | `localhost:8080` | listen address |
| `--title` | `The Chasers' Lodge` | site title |
| `--public` | off | allow reading without logging in |
| `--as <user>` | off | development only: act as that user on every request |
| `--notify` | off | send the notifications in `_config/notifications.json`; without it they are only logged |
| `--sync` | off | sync the content repo with its upstream branch (GitHub): push the wiki's changes seconds after they are made, merge changes pushed there from elsewhere (on conflict the wiki's version of the file wins, and admins are told) |
| `--sync-every` | `5m` | with `--sync`: how often to check the upstream (admins also have **Sync now** on Recent changes) |

Media files (images, sounds, pdfs) are in `../lodge-data/_media/`, which is git-ignored: copy them separately when moving the wiki.

## Layout of this repo

| Path | What |
| --- | --- |
| `main.go` | command line |
| `internal/wiki` | page ids, the content store and in-memory index (titles, links, backlinks, media use), the git write queue, syncing with the upstream |
| `internal/render` | markdown to HTML (goldmark): heading ids, link classes (`missing`, `external`), image layout (presets, captions, galleries, scaled copies), the table of contents, `list` and `widget` blocks |
| `internal/server` | HTTP: pages, editor, history, diff, search, the page finder, move/rename/delete, media and files, accounts, notifications, sync status, the widget API, help; built-in UI in `ui/` (templates) and `static/` (CSS, JS, the vendored CodeMirror editor) |
| `docs/guide.md` | the user guide, embedded in the program and shown at `/_/help` |
| `deploy/` | the server install/update script and its guide |
| `migrate/` | the one-off DokuWiki migration script, its report, and how to catch up with the live wiki until the switch-over ([SYNC.md](migrate/SYNC.md)) |
| `tools/checkrender` | renders every page and lists leftover syntax and broken links |

## Widget API

Widgets (`_widgets/<name>/widget.js` in the content repo) run in the reader's browser and use `window.wiki`:

```js
wiki.widget("name", (element, config) => { ... });   // config = YAML from the ```widget block
const { data, rev } = await wiki.read("ns/file.json"); // or a page's markdown
const newRev = await wiki.write("ns/file.json", data, rev, { summary: "…", widget: "name" });
const { pages, namespaces } = await wiki.list("ns");
```

Every write is a git commit by the logged-in user (editors and up; readers can only read). A write based on an outdated `rev` fails with `err.conflict = true` instead of overwriting; reload and retry. Writes are limited to 30 a minute per user. `GET /_/log?since=YYYY-MM-DD` returns the commit history as JSON.

## Deploying

See [deploy/README.md](deploy/README.md): one script installs and updates everything on a Debian/Ubuntu server.

## Backups

- **Pages, data, layouts, widgets, accounts:** the content repo, every change a commit. With `--sync` it is pushed to its private GitHub remote seconds after each change.
- **Media:** `_media/` is not in git. `_config/media.json` (committed with every media change) lists each file under `files` (paths under `_media/`) and `trash` (paths under `_media/.trash/`) with its `size`, `sha256` and `modified` time. A backup tool pulls the content repo, then copies every listed file whose hash it doesn't already have, from the server's `_media/` folder (rsync/SFTP) or over HTTP as an admin (`/_media/<path>`; bin files are only reachable on disk). Checking the hashes afterwards proves the copy is complete.
- **State folder:** nothing to back up.
