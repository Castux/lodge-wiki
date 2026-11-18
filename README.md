# lodge-wiki

A small wiki for a group of friends: one Go program, pages in standard
Markdown, everything stored in a git repository, every change a commit.
Written for a tabletop role-playing group's campaign wiki, but nothing in
it is specific to that.

- **Plain files.** Pages are Markdown files, data files are JSON, the look
  is an HTML template and a stylesheet: all in one git repository you can
  clone, read and edit anywhere. No database.
- **History.** Every save, rename, upload and widget change is a commit by
  the person who made it: page history, diffs, restoring old versions, a
  recent-changes list. Concurrent edits are never silently overwritten.
- **Writing.** An editor with syntax highlighting, live preview, a few
  formatting buttons, a fuzzy page finder for links (`[[` or Ctrl+K),
  images pasted or dropped straight in, tables that line themselves up.
- **Organising.** Folders with front pages, renames and moves that update
  every link, backlinks, page listings that stay up to date (`list`
  blocks), a search that finds pages by title and text.
- **Images.** Uploads with a bin, scaled copies served on demand, simple
  layout presets (`#small-right`), captions and galleries without any
  special syntax.
- **Widgets.** Small JavaScript tools that live in the wiki itself and
  read and write its data files (maps, calendars, statistics...).
- **Looks.** Any folder can have its own layout and stylesheet; a sidebar
  written in Markdown.
- **Running it.** Accounts with roles (reader, editor, tinkerer, admin),
  sync with a GitHub copy of the content, Discord notifications, a
  one-click backup of everything, and an install script for a Debian or
  Ubuntu server.

## Quick start

Needs Go 1.27 or newer (the version in `go.mod`) and `git`.

```sh
git clone https://github.com/Castux/lodge-wiki
mkdir lodge-data && git -C lodge-data init
cd lodge-wiki
go run . --root ../lodge-data
```

Open http://localhost:8080. On its first start the wiki creates an `admin`
account and prints its password; change it with
`go run . --root ../lodge-data --set-password admin`.

The content folder can be any git repository (an empty one is fine). Keep
it private: it holds the accounts' password hashes and emails.

## Documentation

- [User guide](docs/guide.md): pages, writing, images, listings, accounts.
  The wiki shows it at `/_/help`.
- [Running a wiki](docs/running.md): options, what is where in the
  content repository, roles, syncing, notifications, backups.
- [Layouts](docs/layouts.md): the page template and its fields,
  stylesheets, the sidebar.
- [Widgets](docs/widgets.md): writing one, and the API it uses.
- [Deploying](deploy/README.md): installing and updating on a server.
- [Design](docs/design.md): the main choices, and why.

## License

MIT, see [LICENSE](LICENSE). The bundled CodeMirror editor is MIT too
([its license](internal/server/static/cm-LICENSE.txt)).
