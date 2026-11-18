# Deploying

`setup.sh` installs the wiki on a Debian or Ubuntu server, and updates it: run it again after every change you want live. It is safe to repeat.

## First install

1. Point the wiki's domain (DNS A/AAAA record) at the server.
2. On the server:

   ```sh
   gh repo clone Castux/lodge-wiki   # or git clone, with access to the private repo
   cd lodge-wiki
   sudo deploy/setup.sh          # creates deploy/lodge.conf and stops
   nano deploy/lodge.conf        # DOMAIN, DATA_REPO, NOTIFY...
   sudo deploy/setup.sh
   ```

3. The script creates an SSH key for the wiki's service account, which needs read and write access to the data repo:
   - If you are logged in to the GitHub CLI (`gh auth login`), the script adds the key to the repo itself as a deploy key with write access, and carries on.
   - Otherwise it prints the key and stops. Add it on GitHub (the data repo → Settings → Deploy keys → Add deploy key, **allow write access**) and run `sudo deploy/setup.sh` again.

   The wiki uses only this key, which reaches the data repo alone, never your own GitHub login. You don't need your own clone of the data repo on the server; the script makes the wiki's.
4. Copy the media from your computer (it is not in git):

   ```sh
   rsync -av --chmod=D755,F644 --progress lodge-data/_media/ you@server:/srv/lodge/lodge-data/_media/
   # (--chmod: copies from Windows or WSL otherwise arrive as rwxrwxrwx)
   # then on the server:
   sudo chown -R lodge: /srv/lodge/lodge-data/_media && sudo systemctl restart lodge-wiki
   ```

## Updating

```sh
cd lodge-wiki && sudo deploy/setup.sh
```

It pulls the latest code, rebuilds, rewrites the service and Caddy configuration from `lodge.conf`, restarts the wiki and checks it answers. Admins see the running version on the wiki's Admin page, with the number of newer commits on GitHub.

## What it sets up

| What | Where |
| --- | --- |
| Go, git, Caddy | packages (the packaged Go downloads the toolchain `go.mod` names, into root's module cache) |
| Service account `lodge` and its deploy key | home `/srv/lodge`, key `/srv/lodge/.ssh/id_ed25519` |
| Content repo, with media in `_media/` | `/srv/lodge/lodge-data` |
| Session key and image cache (disposable) | `/srv/lodge/lodge-state` |
| The program | `/usr/local/bin/lodge-wiki` |
| Service (sandboxed, restarts on failure) | `/etc/systemd/system/lodge-wiki.service` |
| HTTPS (automatic certificate) | `/etc/caddy/sites/lodge-wiki.caddy`, imported by `/etc/caddy/Caddyfile` |

With `SYNC=yes` every change is pushed to the data repo a few seconds later, so GitHub always holds the pages, data, layouts and accounts. Uploaded files need their own backup: see Backups below.

**Editing the data outside the wiki.** Clone the data repo anywhere, edit, commit and push to GitHub: the wiki merges it within 5 minutes (admins can press **Sync now** on the Admin page). If a file you changed was also changed in the wiki meanwhile, in a way git cannot combine, the wiki keeps its own version of that file and the Admin page tells admins which files: pull, redo your change on top, push again. Your version stays in the history (`git log --full-history -p -- <file>`). Don't edit `_config/media.json` by hand: the wiki maintains it.

**Other services on the same machine.** The wiki only adds its own site file in `/etc/caddy/sites/` and one `import sites/*` line to the Caddyfile (or replaces Caddy's stock placeholder page); other sites are left as they are. The result is checked with `caddy validate` before Caddy is reloaded (not restarted), and the previous files are restored if it fails. If the wiki's local port (`LISTEN`) is taken by another program, the script stops and says so.

Useful commands: `journalctl -u lodge-wiki -f` (logs), `sudo systemctl restart lodge-wiki`, `systemctl status lodge-wiki`.

## Backups

Pages and their history are on GitHub (the data repo), but uploaded files are not in git: they live only on the server.

**One click:** admins have **Download a backup** on the Admin page. It gives one zip with everything needed to run the wiki elsewhere, without access to the server: every file (pages, settings, accounts, uploaded files and the bin), the whole history as a git bundle, and a `README.txt` saying how to restore it (`git clone` the bundle, copy the media into it, run lodge-wiki on it). Keep it private: it holds the accounts and the Discord webhooks.

**Regularly, from a machine you control** (pulling, so the server holds no keys to it):

```sh
rsync -a you@server:/srv/lodge/lodge-data/_media/ ~/lodge-media-backup/
```
