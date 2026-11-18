# Running a wiki

## Options

```sh
lodge-wiki --root ../lodge-data [options]
```

| Option | Default | Meaning |
| --- | --- | --- |
| `--root` | `../lodge-data` | the content repository |
| `--state` | `<root>/../lodge-state` | the state folder: session key and scaled-image cache, both disposable |
| `--addr` | `localhost:8080` | listen address |
| `--title` | `Wiki` | the site's name |
| `--public` | off | anyone can read without logging in |
| `--notify` | off | send the notifications in `_config/notifications.json`; without it they are only logged |
| `--sync` | off | sync the content repository with its upstream branch, see below |
| `--sync-every` | `5m` | with `--sync`: how often to look for changes pushed elsewhere |
| `--set-password <name>` | | create or update that admin account (password read from the terminal) and exit |
| `--as <name>` | | development only: every request acts as that account |

The first start of a wiki without accounts creates `admin` and prints its
password.

## The content repository

Everything that matters is in one git repository:

| Path | What |
| --- | --- |
| `start.md`, `<folder>/<page>.md` | pages; a page's address is its path without `.md`, and `<folder>/start.md` is the folder's front page |
| `<folder>/<name>.json` (and other text files) | data files, for widgets |
| `_layout/`, `<folder>/_layout/` | the look: page template, stylesheets, sidebar ([layouts](layouts.md)) |
| `_widgets/<name>/` | widgets ([widgets](widgets.md)) |
| `_config/users.json` | accounts: bcrypt hashes, roles, real names, emails |
| `_config/notifications.json` | notification rules (below) |
| `_config/media.json` | the list of uploaded files with their hashes, kept by the wiki |
| `_media/` | uploaded files; not in git (add it to `.gitignore`) |
| `_media/.trash/` | the bin: deleted and replaced uploads |

Page addresses are lowercase letters, digits, `-`, `_` and `.`. Folders
starting with `_` are special: pages never live there. `_config` is only
visible to admins; `_layout` and `_widgets` can only be changed by
tinkerers and admins, because their code runs in every reader's browser.

The state folder can be deleted at any time: everyone is logged out and
images are scaled again. Never put it in git (its session key can forge
logins).

## Roles

| Role | Can |
| --- | --- |
| reader | read every page and its history |
| editor | also edit, create, rename, upload, use widgets that write |
| tinkerer | also change layouts and widgets |
| admin | also accounts, `_config`, syncing, backups |

Admins create accounts on the Users page. **Editors can put HTML, and so
scripts, in pages**, as in most wikis of this kind: give editing rights to
people you trust. Every change is a commit with their name, and can be
undone.

## Syncing with GitHub

With `--sync`, the wiki keeps its repository in step with its upstream
branch (usually a private GitHub repository): it pushes its own commits
seconds after they are made, and merges commits pushed there from
elsewhere every few minutes (admins also have **Sync now** on Recent
changes). So the content can be edited with any editor and pushed.

If the same file was changed on both sides in a way git cannot combine,
the wiki keeps its own version of that file, and Recent changes tells
admins which ones. The other version stays in the history, as the merge's
second parent.

## Notifications

`_config/notifications.json` posts a message to Discord webhooks when
pages change:

```json
{
  "base_url": "https://wiki.example.org",
  "hooks": [
    {
      "webhook": "https://discord.com/api/webhooks/…",
      "on": ["create"],
      "namespaces": ["stories"],
      "attach-image": true,
      "color": "#8a6d3b",
      "template": {
        "title": "%title%",
        "url": "%url%",
        "description": "%abstract%",
        "footer": { "text": "%type-past% by %user%" }
      }
    }
  ]
}
```

- `on`: `create`, `edit`, `delete`, `revert` (all if absent).
- `namespaces`: folders whose pages trigger it, not their sub-folders
  (all pages if absent).
- `template`: a Discord embed; its texts can hold `%id%`, `%url%`,
  `%title%`, `%abstract%` (the page's first lines), `%user%` (real name if
  set), `%summary%`, `%type%` and `%type-past%`.
- `attach-image`: upload the page's first image with the message.
- `color`, or `color-create`, `color-edit`, `color-delete`,
  `color-revert`: the embed's colour.

Renames and data files send nothing. Nothing at all is sent unless the
wiki runs with `--notify`; otherwise each message is logged. Webhook
addresses are secrets: `_config` is only visible to admins.

## Backups

- **One click:** admins have **Download a backup** on Recent changes: a zip
  with every file, the uploaded files, the whole history as a git bundle,
  and instructions to run the wiki again from it. It is written while it
  downloads.
- **Pages and data:** the repository itself. With `--sync`, the upstream
  always holds a copy.
- **Uploaded files** are not in git. `_config/media.json` lists each one
  (under `files`, and the bin under `trash`) with its size, SHA-256 and
  modification time, and is committed with every change: a backup tool can
  copy the files it lacks (from `_media/` on disk, or over HTTP as an
  admin at `/_media/<path>`) and check the hashes.
- **The state folder:** nothing to back up.

## Deploying

[deploy/README.md](../deploy/README.md): one script installs and updates
the wiki on a Debian or Ubuntu server, behind Caddy for HTTPS.
