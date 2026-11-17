# Catching up with the live DokuWiki

Until the switch-over, people keep editing the old wiki at
the-chasers-lodge.net. To bring their changes into `lodge-data`:

1. download the live site into `../public_html` (read-only on the server);
2. list what changed since the last time;
3. re-convert those pages and copy the new media into `lodge-data`;
4. review and commit.

## 1. Download

From Windows, in WSL. `lftp` reads the FTP login from `~/.netrc` in WSL
(machine `ftp.the-chasers-lodge.net`). `mirror` only downloads: nothing on
the server is changed. `--delete` removes *local* files that no longer exist
online. `ssl:check-hostname no` is needed because the shared host's
certificate is issued for the host's own name, not for this domain.

Dry run first (lists what would be downloaded or deleted locally):

```sh
wsl -e bash -c 'lftp -e "set ssl:check-hostname no; mirror --dry-run --delete --verbose --only-newer --parallel=8 --exclude-glob data/cache/ --exclude-glob data/attic/ --exclude-glob apbot/ --exclude-glob plonk/ --exclude-glob polyglobe/ . /mnt/d/dev/the-chasers-lodge.net/public_html; bye" ftp.the-chasers-lodge.net'
```

Then the same without `--dry-run`:

```sh
wsl -e bash -c 'lftp -e "set ssl:check-hostname no; mirror --delete --verbose --only-newer --parallel=8 --exclude-glob data/cache/ --exclude-glob data/attic/ --exclude-glob apbot/ --exclude-glob plonk/ --exclude-glob polyglobe/ . /mnt/d/dev/the-chasers-lodge.net/public_html; bye" ftp.the-chasers-lodge.net' > sync.log 2>&1
```

Excluded: DokuWiki's cache, the page history (`data/attic`, large and not
needed to catch up: only current pages are imported) and unrelated
experiments (`apbot`, `plonk`, `polyglobe`). To also refresh the history
(only needed to recreate `lodge-data` from scratch, which is no longer
done):

```sh
wsl -e bash -c 'lftp -e "set ssl:check-hostname no; mirror --verbose --only-newer --parallel=8 data/attic /mnt/d/dev/the-chasers-lodge.net/public_html/data/attic; bye" ftp.the-chasers-lodge.net'
```

## 2. List the changes

From `lodge-wiki`, with the date of the newest change already imported
(see the log at the bottom; a day of overlap is harmless):

```sh
go run ./migrate --changed-since 2026-11-02
```

It prints the pages changed since then (created, edited, reverted,
deleted; with author and summary), the media files modified since then,
media deleted on the old wiki, and the command for step 3. It reads each
page's own `data/meta/<page>.changes` file, which DokuWiki never trims
(the global recent-changes log is trimmed after a few days).

## 3. Import

```sh
go run ./migrate --pages skarnholm/notice/olumides-treasure,skarnholm/vault/start
go run ./migrate --changed-since 2026-11-02 --copy-media
```

- `--pages` re-converts these pages from DokuWiki syntax and overwrites
  them in `../lodge-data`. Nothing else is touched.
- `--copy-media` copies only the media files modified since the date into
  `../lodge-data/_media/`. It never deletes, and files moved or deleted in
  the new wiki do not come back unless they changed on the old one.
- Pages deleted on the old wiki are only listed: remove them by hand
  (`git rm`) if they should go.

## 4. Review and commit

```sh
cd ../lodge-data
git diff
```

Check two things in the diff:

- **Fixes made by hand after the migration.** Some pages were corrected
  in `lodge-data` (see `migrate/REPORT.md` and `git log` of the page). A
  re-converted page loses them: re-apply them, or keep only the new part.
- **Edits made in the new wiki.** If a page was also changed in
  `lodge-data` since the migration, the re-conversion replaces those
  changes: merge by hand.

Then commit, crediting the old wiki's author (same form as the migrated
history, `<name>@lodge.invalid`):

```sh
git add -A
git commit --author "loviisa <loviisa@lodge.invalid>" -m "Catch up with the old wiki: Olumide's treasure"
```

Re-converting a page brings in only its current text, not each
intermediate DokuWiki revision; the old wiki keeps those until it is
retired. A running wiki picks up the new files on its next restart (media
is listed in `_config/media.json` at start-up).

Once the wiki runs on the server, which pushes its own edits to GitHub:
`git pull` in `lodge-data` before importing, push after committing, then
run `deploy/setup.sh` on the server (it merges the data repo) and copy the
new media there with `rsync` (see `deploy/README.md`).

## Log

| Downloaded | Newest change on the old wiki included |
| --- | --- |
| 2026-11 (first download, before the migration on 2026-11-17) | 2026-11-02 21:40 |
