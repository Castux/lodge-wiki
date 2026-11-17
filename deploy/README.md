# Deploying

`setup.sh` installs the wiki on a Debian or Ubuntu server, and updates it: run it again after every change you want live. It is safe to repeat.

## First install

1. Point the wiki's domain (DNS A/AAAA record) at the server.
2. On the server:

   ```sh
   git clone https://github.com/Castux/lodge-wiki.git
   cd lodge-wiki
   sudo deploy/setup.sh          # creates deploy/lodge.conf and stops
   nano deploy/lodge.conf        # DOMAIN, DATA_REPO, NOTIFY...
   sudo deploy/setup.sh
   ```

3. The script creates an SSH key for the wiki's service account and stops if it cannot read the data repo. Add the printed key on GitHub (lodge-data → Settings → Deploy keys → Add deploy key, **allow write access**) and run `sudo deploy/setup.sh` again.
4. Copy the media from your computer (it is not in git):

   ```sh
   rsync -av --progress lodge-data/_media/ you@server:/srv/lodge/lodge-data/_media/
   # then on the server:
   sudo chown -R lodge: /srv/lodge/lodge-data/_media && sudo systemctl restart lodge-wiki
   ```

## Updating

```sh
cd lodge-wiki && sudo deploy/setup.sh
```

It pulls the latest code, rebuilds, merges content pushed to the data repo from elsewhere, rewrites the service and Caddy configuration from `lodge.conf`, restarts the wiki and checks it answers.

## What it sets up

| What | Where |
| --- | --- |
| Go (the version in `go.mod`), git, Caddy | `/usr/local/go`, packages |
| Service account `lodge` and its deploy key | home `/srv/lodge`, key `/srv/lodge/.ssh/id_ed25519` |
| Content repo, with media in `_media/` | `/srv/lodge/lodge-data` |
| Session key and image cache (disposable) | `/srv/lodge/lodge-state` |
| The program | `/usr/local/bin/lodge-wiki` |
| Service (sandboxed, restarts on failure) | `/etc/systemd/system/lodge-wiki.service` |
| HTTPS (automatic certificate) | `/etc/caddy/sites/lodge-wiki.caddy`, imported by `/etc/caddy/Caddyfile` |

With `PUSH=yes` every change is pushed to the data repo a few seconds later, so GitHub always holds the pages, data, layouts and accounts. Media needs its own backup (see the to-do list in `PLAN.md`).

**Other services on the same machine.** The wiki only adds its own site file in `/etc/caddy/sites/` and one `import sites/*` line to the Caddyfile (or replaces Caddy's stock placeholder page); other sites are left as they are. The result is checked with `caddy validate` before Caddy is reloaded (not restarted), and the previous files are restored if it fails. If the wiki's local port (`LISTEN`) is taken by another program, the script stops and says so.

Useful commands: `journalctl -u lodge-wiki -f` (logs), `sudo systemctl restart lodge-wiki`, `systemctl status lodge-wiki`.
