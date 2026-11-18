# Design

The main choices behind lodge-wiki, and why.

## Generic code, specific content

Anything generic is Go code in this repository; anything specific to one
wiki (its pages, data, look, widgets, settings) lives in its content
repository. A new need is first met with content (a page, a `list` block,
a widget, a layout); only what any wiki could use goes into the program.

## Plain files in git

- **Pages are standard Markdown** (CommonMark, the GitHub extensions,
  footnotes, raw HTML). No wiki-link syntax, no front matter, no custom
  containers: a page reads the same on GitHub or in any editor. Links use
  the page's address, so an address copied from the browser is a valid
  link. The only additions are fenced blocks named `list` and `widget`,
  which other viewers show as code.
- **Every change is a commit**, by the person who made it. History, diffs,
  restoring and backups all come from git. Concurrent edits are detected
  by revision (the commit a change started from), and never silently
  overwritten.
- **No database.** The index (titles, links, backlinks, dates, search) is
  built in memory at start-up from the files and one `git log` pass. That
  is quick for a few thousand pages.
- **Uploaded files are not in git** (they would make it huge), but a
  manifest of their hashes is (`_config/media.json`), so they have a
  visible history and backups can be checked. Deleting moves them to a
  bin, and only files no page uses can be deleted.

## Syncing

The wiki merges its upstream, it does not rebase: rebasing would change
commit ids, which open editors use as their base revision. On a conflict
the wiki's version of the whole file wins (a line-by-line mix could break
a JSON file), and the other version stays in the history, reported to
admins.

## Images

Authors write plain Markdown images; the wiki lays them out: an image
alone is centred and kept to a sensible height, a title becomes a caption,
images together become a gallery. The only knobs are a few presets in the
address's fragment (`#small-right`), which is valid Markdown that other
viewers ignore. Images are served scaled to the size they are shown at.

## Pages, not records

The wiki has no typed records with fixed fields: those break on the first
exception, and bring back "the feature we need doesn't exist". Folders,
conventions in the text, `list` blocks and widgets cover it.

## Trust and security

- **Editors can put HTML, and so scripts, in pages**, as in most small
  wikis: editing is for people you trust, and every change can be traced
  and undone. Layouts and widgets, whose code runs for everyone, need the
  tinkerer role; accounts and settings need an admin.
- Every path from a request is checked to stay inside the repository;
  revisions from requests must be commit hashes and are passed to git
  after `--end-of-options`; git runs with literal pathspecs, never
  prompts, and times out.
- Cross-site form posts are refused (Go's `http.CrossOriginProtection`).
- Failed logins are limited per address; sessions are signed cookies,
  logged out on password change.
- The state folder holds only disposable things; everything that matters
  is in the content repository, which must stay private.
