# Lodge wiki: replacing DokuWiki

Plan for a custom wiki for the Chaser's Lodge group, replacing the current DokuWiki in `../public_html`.
Written 2026-11-17, from the live site synced into `../public_html` that day.

This is not a generic wiki product. It is built for this group and lives next to its data.

## Guiding principle

**Generic goes in core, specific goes in the data.**

- **Core (Go):** anything any wiki would need. Rendering, links, history, auth, permissions, listings, layouts, the write path.
- **Data (git repo):** anything about *this* group. Pages, JSON data files, layouts and styles, widgets (map, calendar, stats).

If a feature mentions a campaign, a place or a game mechanic, it is a widget or a data file, not Go code.
Keep the core small enough that one person can hold all of it in their head (aim for under about 5,000 lines of Go).

## Verdict

Feasible and worth doing. The wiki is modest in size, so a single Go binary on a small VPS handles it easily.
Three things are harder than they look:

- **Conversion is mostly, not fully, automatic.** Pages are in DokuWiki syntax and use plugin syntax. A converter handles most of it; the rest becomes a checklist.
- **Media does not belong in plain git.** 897 MB across 1,214 files would make every clone and backup slow.
- **Scripting is the one real design decision.** Running widgets in the browser, with every write going through git, avoids a server sandbox entirely. What matters is who may edit widget code.

The trade-off is ownership: nothing upgrades under you any more, but you maintain the whole stack.

## What exists today

| Area | Size | Notes |
| --- | --- | --- |
| Pages | 1,230 files, 8.8 MB | `foolslanding` 953, `skarnholm` 208, `sunfire` 34, `rules` 17, `wiki` 13 |
| Media | ~1,214 files, ~897 MB | Mostly jpg/png; 36 files over 5 MB; some mp3, wav, pdf |
| Page metadata | ~14 MB | Change logs, backlinks, titles: all rebuildable |
| History | `data/attic` (not synced yet) | One gzipped copy per revision; author and summary in `data/meta/*.changes` |
| Accounts | 45 in `users.auth.php` | About 12 regular editors |

Syntax in use (approximate counts):

| Construct | Count | Conversion |
| --- | --- | --- |
| `{{ns:image.jpg}}` media embeds | ~5,950 | Mechanical rewrite to markdown images |
| `[[page]]` links | ~2,900 | Keep as wiki-links; the renderer resolves them |
| DokuWiki headings and lists | most pages | Converter (pandoc has a DokuWiki reader) |
| Tables (`^` headers) | ~80 rows | Converter |
| `<nspages …>` listings | 21, in 5 variants | One built-in listing directive |
| `<wrap>` / `<WRAP>` | ~40 | Container directive or plain HTML |
| Inline `<script>`, `<html>` | 7 | Hand-port to widgets |

Custom pieces:

- **Notice boards:** a modified `nspages` plugin, including a picture-card variant sorted by creation date.
- **Map:** a Leaflet page in `/map` that reads and writes pins as JSON in `skarnholm:mapdata` over DokuWiki's JSON-RPC API, with a token for the `carto` account.
- **Calendar:** `lib/scripts/lodge-cal.lua` parses `name|page|date|duration` lines using the in-world calendar (named months such as Longnight).
- **Stats:** a Lua binary run by cron on the server (`wiki:stats`, `wiki:stats_script`).
- **Discord notifier:** the `lodgenotifier` plugin.
- **Theme per namespace:** the `loadskin` plugin maps `skarnholm` to the `skarn` template; everything else uses `lodge`. Both have their own fonts.
- **Access:** private wiki. Logged-out visitors see nothing, `@user` can edit everything, `admin:*` is hidden, `@bot` reads the `log`, `notice` and `story` namespaces, and `carto` may edit `skarnholm:mapdata` only.

Standard plugins also in use: `move`, `revert`, `pageredirect`, `captcha`, `smtp`, `edittable`, `backup`, `orphanswanted`, `iframe`.

## Architecture

One Go binary serves everything. A single git repository is the only source of truth for pages, data files, layouts, styles and widgets.
Everything else is a cache or a file store that can be rebuilt or restored.

```
Browser  (reading, markdown editor with live preview, widgets)
   |  HTTPS
Go server, one binary
   +-- HTTP + auth        sessions, roles, Discord login
   +-- Renderer           goldmark, wiki-links, directives, layout lookup
   +-- Data routes        read / write / list, for widgets
   +-- Write queue        single writer; every write is a commit
   |
   +--> Content repo (git)     pages/**/*.md, **/*.json, _layout/ folders, widgets
   |       \--> off-site remote, pushed after every commit
   +--> Index cache (SQLite)   search, backlinks, listings, frontmatter; rebuilt from git
   +--> Media store            files named by hash; manifest in git; backed up separately
```

- **Pages** are markdown with a small YAML frontmatter (title, tags, notice fields). The path is the page id: `foolslanding/notice/missingpet.md`.
- **Data files** (`.json`) sit next to pages and hold structured data such as map pins and calendar events.
- **Every save is a git commit,** authored as the logged-in user, with the edit summary as the message. History, diff, blame and revert are git operations.
- **One write queue** (a single goroutine) applies saves in order, so concurrent users never fight over git's lock. Each save carries the revision it was based on. On a conflict, try `git merge-file`; if that fails, show both versions instead of overwriting.
- **The index cache** (SQLite with FTS5) holds search, backlinks, listings and parsed frontmatter. It is rebuilt from the repo on start-up or after a pull.
- **Media** lives outside git, stored by content hash, with a manifest in git mapping names to hashes.
- **Auth:** "Log in with Discord" plus a role table (reader, editor, admin), and one local admin account as a fallback.
- **Notifications:** the Discord notifier is a post-commit hook.

Go libraries: `goldmark` (markdown), `modernc.org/sqlite` (no C compiler needed), and the `git` command-line tool for writes.

### Layouts and styles per namespace

Each namespace can have its own look, as `skarnholm` does today. This is generic, so it belongs in core.

- Any folder may hold a `_layout/` folder with `page.html` (Go `html/template`), `style.css` and assets such as fonts.
- To render a page, the core looks for `_layout/` in the page's own folder, then its parent, and so on up to the root `_layout/`. The nearest one wins.
- A namespace layout may also just add CSS on top of its parent's template, using `style.css` with no `page.html`, for small variations.
- Layouts are versioned and edited online by admins, like widgets.

Today's setup becomes `/_layout/` (from the `lodge` template) and `/skarnholm/_layout/` (from `skarn`).

### Access control

The first version keeps today's simple model: private wiki, roles per user (reader, editor, admin), and `admin/` visible to admins only.

Today's special accounts are not carried over:

- `carto` only existed so the separate map site could write `skarnholm:mapdata` with a token. As a widget, the map writes with the reader's own session, so each pin change is committed under that person's name.
- `@bot` read access was for the Discord notifier. It becomes a post-commit hook inside the server and needs no account.

Build every check through one function, `can(user, action, path)`. Per-namespace or per-user rules can be added later inside that function without touching the rest. A likely future addition: a `_access.json` per namespace, using the same nearest-ancestor lookup as layouts.

## Scripting: client-side widgets, every write through git

Custom features are JavaScript widgets that run in the reader's browser and talk to the wiki through a few generic routes.
Every write, whether from a page save or a widget, becomes a git commit, so any mistake can be reverted.
The first version has no server-side script runner.

**Widgets.** JavaScript and CSS in `_widgets/<name>/`, placed in a page with a `:::widget map` directive. They run in the page itself, not in an iframe, with the reader's own login: no tokens, no separate site.

**The scripting API is three generic routes:**

| Route | Does |
| --- | --- |
| `GET /raw/<path>` | Read a page's markdown or a data file, with its current revision |
| `PUT /raw/<path>` with base revision and summary | Write a page or data file |
| `GET /list?ns=…&sort=…` | List pages with their frontmatter fields |

A small `wiki.js` helper wraps them (`wiki.read`, `wiki.write`, `wiki.list`), so widgets never build requests by hand.

**Writes are git commits, with no other way in.** This is the core rule of the API:

- A `PUT` goes through the same write queue as a page save: one commit per write, authored as the reader, with the widget's name in the message (`map: move pin "Old Mill" (via widget)`).
- The reader's own permissions apply (`can()`), so a widget cannot write anywhere its user cannot.
- A write must name the revision it was based on. If the file changed since, the server answers 409 Conflict instead of overwriting; the widget reloads and tries again.
- History is never deleted or rewritten. A bad write is undone with an ordinary revert, and the history page shows which widget made it.
- A per-user rate limit (for example 30 writes a minute) stops a widget stuck in a loop from flooding the history.

**Who can edit widgets.** Widget code runs in every reader's browser with their login, so only admins can edit `_widgets/` and `_layout/` folders.

**Later, if needed:** server-side Lua (`gopher-lua`) for jobs a browser cannot do well, such as scheduled stats over the whole history. Add it only when a real need appears. Note that `gopher-lua` is Lua 5.1 (no `goto`).

## Migration

Migration is a repeatable import program, not a one-off job. Run it against a fresh DokuWiki copy as often as needed, diff the output, and switch over only after a clean full run.

- **Pages:** pandoc's DokuWiki reader plus a pre-pass for media embeds, `[[ns:page|label]]` links and plugin tags. Pages the converter could not fully handle go into a report, so you check that list instead of every page.
- **History:** replay `data/attic` plus `data/meta/*.changes` into git, oldest change first, one commit per change with the original author and date. Anonymous edits get a placeholder author. **Pull `data/attic` from the server first**; today's sync left it out.
- **Media:** copy into the hashed store and write the manifest; paths in pages stay the same.
- **Users:** import the 45 accounts and link each one to Discord at first login. DokuWiki's bcrypt hashes can be checked directly; older formats are re-hashed at the next login.
- **Layouts:** `lodge` becomes `/_layout/`; `skarn` becomes `/skarnholm/_layout/`.

| Feature today | In the new system | Effort |
| --- | --- | --- |
| Notice boards (modified `nspages`) | Built-in `:::list` directive with sort and card options | Small |
| Map (Leaflet + JSON-RPC on `skarnholm:mapdata`) | Same Leaflet code as a widget; pins in `skarnholm/map.json`, read and written through the routes under the reader's own account (no `carto` token) | Small |
| Calendar (`lodge-cal.lua`) | Widget; parser ported to JavaScript, events in `calendar/events.json` | Small |
| Stats (cron + Lua binary) | Widget over a fourth generic route, `GET /history?since=…`, or server-side Lua later | Small to medium |
| Discord notifier | Post-commit hook in Go | Small |
| `lodge` and `skarn` templates | Namespace `_layout/` folders; fonts carried over | Medium: visual tuning |
| `move`, `revert`, `pageredirect` | Rename with link rewrite; revert via git; redirect field in frontmatter | Medium |
| Inline `<script>` / `<html>` in 7 pages | Hand-port to widgets or directives | Small |

## Hosting and operations

A small VPS (2 vCPU, 2–4 GB RAM, ~40 GB disk) is the better home; your home server becomes the backup target.

| Concern | Approach |
| --- | --- |
| TLS and domain | Caddy in front for automatic HTTPS. Badaawi and the other subfolders stay on Hostinger under subdomains, or move later. |
| Deploys | One binary plus a systemd unit; deploy is copy and restart. Content is never part of a deploy. |
| Backups | Repo: `git push` to a private remote after every commit, plus a nightly pull to the home server. Media: nightly `restic` or `rclone`. A restore is clone, copy media, start. |
| Load | 25 users is very light; pages render from cache; saves take milliseconds. |
| Monitoring | Uptime check, plus `/healthz` reporting the last commit, last push and rejected writes. |
| Editing offline | Clone, edit, push. The server pulls, reindexes and treats your commits like web edits. |

## Risks and what not to build

| Risk | Mitigation |
| --- | --- |
| Core keeps growing until it is as hard to touch as DokuWiki | If it is specific to this group, it is a widget or a data file, not Go code. |
| You are the only maintainer | Plain formats (markdown, JSON, git, SQLite cache) mean the data outlives the code. |
| A buggy widget writes bad data | Every write is a commit, so revert it; a per-user rate limit caps the damage; only admins edit widgets. |
| Players have to relearn syntax | Markdown is close to what they type today; a cheat-sheet page and an editor toolbar. Expect a few weeks of questions. |
| Media bloats the repo | Media never enters git; only the manifest does. |
| A migration loses content | The import is re-runnable; DokuWiki stays read-only at its old address for a few months. |

Do not build, at least at first:

- A WYSIWYG editor. Use CodeMirror with live preview and a small toolbar.
- Real-time co-editing. Merge on save is enough for 25 people.
- A Go plugin system. Widgets and data files are the plugin system.
- Your own password crypto or email flows. Use Discord login.
- Full DokuWiki syntax compatibility. Convert once; do not support both.
- Fine-grained access control. Route everything through `can()` and extend it when needed.

## Phased plan

Each phase ends in something usable, and at a gate where you decide whether to carry on.

1. **Read-only core.** Markdown rendering, wiki-links, namespace layouts, the index cache with search and listings.
   - Gate: the converted wiki can be browsed locally with both layouts, and spot checks look right.
2. **Importer.** DokuWiki to markdown, history replay into git, media store, users, and the report of pages needing a hand fix.
   - Gate: a full re-run from a fresh copy finishes cleanly; the report is short enough to fix by hand.
3. **Editing.** Discord login and roles, `can()`, editor with preview, write queue with conflict handling, history, diff, revert, rename with link rewrite, Discord notifier.
   - Gate: you and two or three players use it alongside DokuWiki for a session or two.
4. **Widgets.** Data files, the three routes and `wiki.js`; port the map, calendar and stats.
   - Gate: every custom feature works without DokuWiki.
5. **Switch-over.** Final import, DNS to the VPS, DokuWiki frozen read-only at a subdomain, backups confirmed by a test restore.
   - Gate: one quiet month, then retire DokuWiki.

## Open questions

- Should any players be able to edit widgets or layouts, or only you?
- Does Badaawi move with the wiki, or stay on Hostinger?

## Repositories

- `lodge-wiki` (this folder): the software. GitHub: `Castux/lodge-wiki`, private.
- `lodge-data` (sibling folder `../lodge-data`): the content repo, generated by the importer and served by the wiki. GitHub: `Castux/lodge-data`, private.
- `public_html` (sibling folder): the DokuWiki copy synced from the live site, used as the migration source. Not a repo.

## Status (2026-11-18)

| Phase | State |
| --- | --- |
| 1. Read-only core | Done: rendering shaped like DokuWiki's HTML, namespace layouts (`lodge`, `skarnholm`), TOC, listings, search, recent changes, scaled images. |
| 2. Importer | Done: 1,229 pages + 11,775 history commits; conversion leftovers and the markdown-plugin pages fixed. See `migrate/REPORT.md`. |
| 3. Editing | Done: highlighted editor with preview, conflict handling, history, diff, restore, new page, rename/move of pages, folders and media with link rewriting, Files view, uploads, accounts (imported with passwords, users and account pages). |
| 4. Widgets | Done: map, calendar (with add-event form), stats. Data files editable in the browser. |
| 5. Switch-over | Not started. |

## To do

Before going live:

- [x] **Notifications**: done (`_config/notifications.json`, the old rules); turn on with `--notify` when the wiki goes live.
- [x] **Deployment**: `deploy/setup.sh` (see `deploy/README.md`); still to do: restic or the backup tool for media on the server.
- [ ] **Media backup tool**: `lodge-wiki backup-media --from <server> --to <folder>` reading `_config/media.json`, copying missing or changed files (and the bin), then verifying hashes. Decide the transport (rsync/SFTP on the server's `_media`, or HTTP as an admin, which needs an admin-only way to fetch bin files).
- [ ] **Security before going public**: CSRF tokens on forms; HTTP handler tests.
- [x] **User guide**: `docs/guide.md`, served at `/_/help` (Help in the sidebar), with each widget's `_widgets/<name>/README.md`.
- [x] **Finding and linking**: quick jump (`/`, Ctrl+K) with fuzzy matching on titles; links by title in the editor (`[[`, Ctrl+K); images pasted or dropped into the editor are uploaded and inserted.
- [x] **Sync with GitHub** (`--sync`): manual edits pushed to the data repo are merged within 5 minutes or on **Sync now**; on conflict the wiki's version of the file wins and admins are told.
- [x] **Image layout**: automatic sizing, captions, galleries, `#small`/`#medium`/`#full`/`#left`/`#right` presets, picker options (see the guide).
- [x] **Delete pages**: Delete in the sidebar (a confirmation page; deleting is saving the page empty, so its history is kept and it can be restored).
- [ ] **Catch up with the old wiki** before switching over: `migrate/SYNC.md`.

Browsing (software and layout only, no content changes):

- [ ] **"Mentioned in"** at the bottom of each page (the backlinks, shown directly).
- [ ] **Previous / next** between pages of a folder, in creation order (logs and stories read like a book).
- [ ] **Sidebar readability**: body font in small caps for the small labels; Edit as a clear button.
- [ ] **Night mode** following the device setting (a candlelit palette; colours are already variables).
- [ ] **Image viewer**: clicking an image opens it over the page instead of a new tab.

Later:

- [ ] **Discord login**: needs a Discord application (client id, secret) and the public address for the redirect.
- [ ] Empty the media bin (admins).
- [ ] Drop the `pending` role: accounts are created by admins, there is no sign-up page.
- [ ] **Convert the old HTML images to markdown** (after the switch-over, like other content edits; ask the players first, they may have opinions). About 950 `<img>` tags outside the Domination glyphs. A one-off script, reviewed in the diff:
  - `align="center"` or no align, width 400 or more, or no width → `![alt](src)` (laid out automatically);
  - width 250 or less → `#small`; 300 to 350 → `#medium`;
  - `align="right"` / `"left"` → `#right` / `#left` (with the size word if any), moved to the start of the paragraph it sat in. Many floats came from DokuWiki's space-based alignment (`{{ img}}`) and were probably accidental: list them for a decision instead of converting blindly;
  - images inside links or tables, and anything else unusual: leave as HTML and list;
  - glyphs (`width="40"`/`"45"`, `domination/glyphs/`): leave as HTML, or a small glyph helper later.
- [ ] **Explore: records from mostly-structured pages.** No page format change: parse the conventions pages already follow (character sheets, items, notices) into records, see how regular they are, and whether a record could drive a fixed layout for its page (or a listing) while the page stays free text. Keeps the wiki's flexibility; exceptions stay plain pages.
- [ ] **Clean up DokuWiki leftovers in the content**, once the old wiki is retired: the old help pages in `wiki/` (`syntax`, `dokuwiki`, `newlines`, `spoiler-tag`, `welcome`, `orphans`, `stats_script`, `notices-tech`...) and mentions of DokuWiki syntax or plugins elsewhere; point to Help instead.
- [ ] Ideas that need content edits, once the old wiki is retired: a navigation bar per campaign (`_layout/nav.md`), "recently updated" on campaign home pages (`limit` on `list` blocks), page templates for new logs and stories, character infoboxes.
