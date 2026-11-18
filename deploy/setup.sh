#!/usr/bin/env bash
# Install or update the wiki on a Debian/Ubuntu server.
#
#   git clone https://github.com/Castux/lodge-wiki.git && cd lodge-wiki
#   sudo deploy/setup.sh            # first run: writes deploy/lodge.conf, then stops
#   (edit deploy/lodge.conf)
#   sudo deploy/setup.sh            # installs everything; run again any time to update
#
# Every run is safe to repeat: it pulls the latest code (and restarts
# itself if that changed this script), rebuilds, rewrites the service and
# web server configuration, restarts the wiki and checks that it answers.
# The wiki merges data changes pushed from elsewhere itself (SYNC=yes).
set -euo pipefail
# (With pipefail, never write "cmd | grep -q": grep -q stops reading early,
# cmd gets a broken pipe and the test fails. Capture the output first.)

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$HERE/.." && pwd)"
CONF="$HERE/lodge.conf"

say()  { printf '\n\033[1m== %s\033[0m\n' "$*"; }
note() { printf '   %s\n' "$*"; }
warn() { printf '\033[33m!! %s\033[0m\n' "$*"; }
die()  { printf '\033[31mxx %s\033[0m\n' "$*" >&2; exit 1; }

[ "$(id -u)" -eq 0 ] || die "Run with sudo: sudo $0"
command -v apt-get >/dev/null || die "This script expects Debian or Ubuntu (apt-get)."
OWNER="${SUDO_USER:-root}"   # who owns this lodge-wiki checkout

# ---------- update this checkout first ----------
# Pull before doing anything else and, if that brought new code, run the
# new version of this script instead of carrying on with the old one.
if [ -z "${LODGE_SETUP_PULLED:-}" ] && [ -d "$REPO/.git" ]; then
  BEFORE="$(sudo -u "$OWNER" git -C "$REPO" rev-parse HEAD)"
  if sudo -u "$OWNER" git -C "$REPO" pull --ff-only -q; then
    if [ "$(sudo -u "$OWNER" git -C "$REPO" rev-parse HEAD)" != "$BEFORE" ]; then
      note "updated lodge-wiki to $(sudo -u "$OWNER" git -C "$REPO" log -1 --format='%h %s'); restarting the script"
      export LODGE_SETUP_PULLED=1
      exec bash "$REPO/deploy/setup.sh" "$@"
    fi
  else
    warn "could not update lodge-wiki (local changes?); using what is here"
  fi
fi

# ---------- settings ----------

if [ ! -f "$CONF" ]; then
  cp "$HERE/lodge.conf.example" "$CONF"
  [ -n "${SUDO_USER:-}" ] && chown "$SUDO_USER" "$CONF"
  say "Created $CONF"
  note "Fill it in (at least DOMAIN and DATA_REPO), then run this script again."
  exit 0
fi
# shellcheck source=lodge.conf.example
. "$CONF"
: "${BASE:=/srv/lodge}" "${LISTEN:=127.0.0.1:8080}" "${SERVICE_USER:=lodge}"
# (not ${TITLE:=...}: bash would read the apostrophe as an opening quote)
[ -n "${TITLE:-}" ] || TITLE="The Chasers' Lodge"
: "${NOTIFY:=no}" "${SYNC:=${PUSH:-yes}}" "${DOMAIN:=}" "${DATA_REPO:=}"   # (PUSH: the old name of SYNC)
DATA="$BASE/lodge-data"
STATE="$BASE/lodge-state"
BIN=/usr/local/bin/lodge-wiki
UNIT=/etc/systemd/system/lodge-wiki.service

# ---------- packages ----------

say "Packages"
NEED=()
for p in git curl ca-certificates rsync; do dpkg -s "$p" >/dev/null 2>&1 || NEED+=("$p"); done
if [ ${#NEED[@]} -gt 0 ]; then
  apt-get update -qq && apt-get install -y -qq "${NEED[@]}" >/dev/null
  note "installed ${NEED[*]}"
else
  note "git, curl, rsync present"
fi

# Go: the version go.mod asks for, from go.dev (distribution packages lag behind).
GO_WANT="$(awk '/^go /{print $2; exit}' "$REPO/go.mod")"
GO_HAVE="$(/usr/local/go/bin/go env GOVERSION 2>/dev/null | sed 's/^go//' || true)"
if [ -z "$GO_HAVE" ] || [ "$(printf '%s\n%s\n' "$GO_WANT" "$GO_HAVE" | sort -V | head -1)" != "$GO_WANT" ]; then
  case "$(uname -m)" in
    x86_64) GOARCH=amd64 ;; aarch64|arm64) GOARCH=arm64 ;; *) die "Unsupported CPU: $(uname -m)" ;;
  esac
  note "installing Go $GO_WANT (had: ${GO_HAVE:-none})"
  curl -fsSL "https://go.dev/dl/go${GO_WANT}.linux-${GOARCH}.tar.gz" -o /tmp/go.tgz
  rm -rf /usr/local/go && tar -C /usr/local -xzf /tmp/go.tgz && rm /tmp/go.tgz
else
  note "Go $GO_HAVE"
fi
export PATH="/usr/local/go/bin:$PATH"

# Caddy: HTTPS with automatic certificates, only when a domain is set.
if [ -n "$DOMAIN" ] && ! command -v caddy >/dev/null; then
  note "installing Caddy"
  apt-get install -y -qq debian-keyring debian-archive-keyring apt-transport-https gnupg >/dev/null
  curl -fsSL 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | gpg --batch --yes --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
  curl -fsSL 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' > /etc/apt/sources.list.d/caddy-stable.list
  apt-get update -qq && apt-get install -y -qq caddy >/dev/null
fi

# ---------- service account and folders ----------

say "Service account"
if ! id "$SERVICE_USER" >/dev/null 2>&1; then
  useradd --system --home-dir "$BASE" --create-home --shell /usr/sbin/nologin "$SERVICE_USER"
  note "created user $SERVICE_USER (home $BASE)"
fi
install -d -o "$SERVICE_USER" -g "$SERVICE_USER" -m 750 "$BASE" "$STATE"
as_svc() { sudo -u "$SERVICE_USER" -H "$@"; }

# An SSH key so the server can pull and push the data repo.
KEY="$BASE/.ssh/id_ed25519"
if [ ! -f "$KEY" ]; then
  install -d -o "$SERVICE_USER" -g "$SERVICE_USER" -m 700 "$BASE/.ssh"
  as_svc ssh-keygen -q -t ed25519 -N '' -C "lodge-wiki@$(hostname)" -f "$KEY"
  note "created the deploy key $KEY"
fi
if ! grep -q github.com "$BASE/.ssh/known_hosts" 2>/dev/null; then
  ssh-keyscan -t ed25519 github.com 2>/dev/null | as_svc tee -a "$BASE/.ssh/known_hosts" >/dev/null
fi
as_svc git config --global user.name lodge-wiki
as_svc git config --global user.email wiki@lodge.invalid
as_svc git config --global pull.rebase false

# ---------- code ----------

say "Code"
[ -d "$REPO/.git" ] && note "lodge-wiki at $(sudo -u "$OWNER" git -C "$REPO" log -1 --format='%h %s')"
OLD_SUM="$(sha256sum "$BIN" 2>/dev/null | cut -d' ' -f1 || true)"
( cd "$REPO" && GOFLAGS="-trimpath -buildvcs=false" go build -o /tmp/lodge-wiki.new . )
install -m 755 /tmp/lodge-wiki.new "$BIN" && rm /tmp/lodge-wiki.new
NEW_SUM="$(sha256sum "$BIN" | cut -d' ' -f1)"
[ "$OLD_SUM" = "$NEW_SUM" ] && note "binary unchanged" || note "built $BIN"

# ---------- data ----------

say "Data"
DATA_CHANGED=no
if [ ! -d "$DATA/.git" ]; then
  if [ -z "$DATA_REPO" ]; then
    as_svc git init -q -b main "$DATA"
    printf '# The wiki\n' | as_svc tee "$DATA/start.md" >/dev/null
    printf '/_media/\n' | as_svc tee "$DATA/.gitignore" >/dev/null
    as_svc git -C "$DATA" add -A && as_svc git -C "$DATA" commit -qm "New wiki"
    note "no DATA_REPO: started an empty wiki in $DATA"
  elif ! LSREMOTE="$(as_svc git ls-remote -q "$DATA_REPO" 2>&1 >/dev/null)"; then
    warn "The server cannot read $DATA_REPO yet. Git says:"
    printf '%s\n' "$LSREMOTE" | head -n 4 | sed 's/^/     /'
    note "If it is a GitHub repository, add this key to it as a deploy key, with write access"
    note "(GitHub: repository > Settings > Deploy keys > Add deploy key):"
    echo; cat "$KEY.pub"; echo
    note "Then run this script again."
    exit 1
  else
    as_svc git clone -q "$DATA_REPO" "$DATA"
    note "cloned $DATA_REPO"
  fi
elif [ -n "$(as_svc git -C "$DATA" remote)" ] && [ "$SYNC" = yes ]; then
  # The wiki merges changes pushed from elsewhere itself (--sync), seconds
  # after starting and every few minutes, keeping its own version of a file
  # on conflict, and pushes its own commits.
  note "the wiki syncs with the remote itself (at start-up, then every 5 minutes)"
elif [ -n "$(as_svc git -C "$DATA" remote)" ]; then
  # SYNC=no: merge what was pushed from elsewhere now, with the wiki stopped.
  if as_svc git -C "$DATA" fetch -q; then
    BEHIND="$(as_svc git -C "$DATA" rev-list --count 'HEAD..@{u}' 2>/dev/null || echo 0)"
    if [ "$BEHIND" -gt 0 ]; then
      systemctl stop lodge-wiki 2>/dev/null || true
      if as_svc git -C "$DATA" merge --no-edit -q '@{u}'; then
        note "merged $BEHIND new commit(s) from the remote"; DATA_CHANGED=yes
      else
        as_svc git -C "$DATA" merge --abort || true
        warn "the remote's changes conflict with the server's; merge by hand in $DATA"
      fi
    else
      note "data up to date with the remote"
    fi
  else
    warn "could not fetch the data repo (network, or the deploy key lacks access)"
  fi
fi

# Media is not in git: check it is there.
MEDIA_LISTED="$(grep -c '"sha256"' "$DATA/_config/media.json" 2>/dev/null || true)"; MEDIA_LISTED="${MEDIA_LISTED:-0}"
MEDIA_HERE=0
[ -d "$DATA/_media" ] && MEDIA_HERE="$(find "$DATA/_media" -type f -not -path '*/.trash/*' | wc -l)"
if [ "$MEDIA_HERE" -eq 0 ] && [ "$MEDIA_LISTED" -gt 0 ]; then
  warn "No media yet ($MEDIA_LISTED files listed in _config/media.json). Copy it from your computer:"
  note "rsync -av --progress lodge-data/_media/ $(hostname -f 2>/dev/null || hostname):$DATA/_media/"
  note "then: sudo chown -R $SERVICE_USER: $DATA/_media && sudo systemctl restart lodge-wiki"
elif [ "$MEDIA_HERE" -gt 0 ]; then
  note "media: $MEDIA_HERE files"
fi
chown -R "$SERVICE_USER": "$DATA" "$STATE"

# ---------- service ----------

say "Service"
FLAGS=(--root "$DATA" --state "$STATE" --addr "$LISTEN" --title "$TITLE")
[ "$NOTIFY" = yes ] && FLAGS+=(--notify)
[ "$SYNC" = yes ] && [ -n "$(as_svc git -C "$DATA" remote)" ] && FLAGS+=(--sync)
# Quote one argument for systemd's ExecStart: double quotes, with \ " escaped
# and systemd's own % and $ doubled.
sd_quote() {
  local s="$1"
  s="${s//\\/\\\\}"; s="${s//\"/\\\"}"; s="${s//%/%%}"; s="${s//\$/\$\$}"
  printf '"%s"' "$s"
}
EXEC="$BIN"; for f in "${FLAGS[@]}"; do EXEC+=" $(sd_quote "$f")"; done
NEW_UNIT="$(cat <<EOF
# Written by lodge-wiki/deploy/setup.sh: edit deploy/lodge.conf and rerun instead.
[Unit]
Description=The Chasers' Lodge wiki
After=network-online.target
Wants=network-online.target

[Service]
User=$SERVICE_USER
Group=$SERVICE_USER
Environment=HOME=$BASE
ExecStart=$EXEC
Restart=on-failure
RestartSec=3
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
ReadWritePaths=$BASE

[Install]
WantedBy=multi-user.target
EOF
)"
if [ "$(cat "$UNIT" 2>/dev/null)" != "$NEW_UNIT" ]; then
  printf '%s\n' "$NEW_UNIT" > "$UNIT"
  systemctl daemon-reload
  note "wrote $UNIT"
fi
# Another service may already use the port (several services on one machine).
PORT="${LISTEN##*:}"
OTHER="$(ss -ltnpH "sport = :$PORT" 2>/dev/null | grep -v '"lodge-wiki"' || true)"
if [ -n "$OTHER" ]; then
  printf '%s\n' "$OTHER"
  die "port $PORT is already used by another program (above): choose another LISTEN in $CONF"
fi
systemctl enable -q lodge-wiki
systemctl restart lodge-wiki
EXTRA="${FLAGS[*]:8}"
note "restarted${EXTRA:+ with $EXTRA}"
for i in $(seq 1 30); do
  if curl -fs -o /dev/null "http://$LISTEN/_/login"; then
    note "the wiki answers on http://$LISTEN"
    break
  fi
  sleep 1
  [ "$i" -eq 30 ] && { journalctl -u lodge-wiki -n 30 --no-pager; die "the wiki did not start (log above)"; }
done

# ---------- web server ----------
#
# The wiki's site lives in its own file, imported by the main Caddyfile, so
# Caddy can serve other sites on the same machine. Other sites are never
# edited, the result is validated before Caddy reloads (restoring the old
# files if it does not validate), and Caddy is reloaded, not restarted.

# is_stock_caddyfile: the file holds only the placeholder site that the
# caddy package installs (comments and spacing aside).
is_stock_caddyfile() {
  [ "$(sed -e 's/#.*//' "$1" | tr -d ' \t\n')" = ':80{root*/usr/share/caddyfile_server}' ]
}

if [ -n "$DOMAIN" ]; then
  say "Caddy (HTTPS for $DOMAIN)"
  CADDY_DIR=/etc/caddy
  CADDYFILE="$CADDY_DIR/Caddyfile"
  SITE="$CADDY_DIR/sites/lodge-wiki.caddy"
  NEW_SITE="$(cat <<EOF
# Written by lodge-wiki/deploy/setup.sh: edit deploy/lodge.conf and rerun instead.
$DOMAIN {
	encode zstd gzip
	request_body {
		max_size 70MB
	}
	reverse_proxy $LISTEN
}
EOF
)"
  install -d "$CADDY_DIR/sites"
  BACKUP="$(mktemp -d)"
  cp -a "$CADDYFILE" "$BACKUP/Caddyfile" 2>/dev/null || true
  cp -a "$SITE" "$BACKUP/site" 2>/dev/null || true
  CHANGED=no

  if [ "$(cat "$SITE" 2>/dev/null)" != "$NEW_SITE" ]; then
    printf '%s\n' "$NEW_SITE" > "$SITE"; CHANGED=yes
    note "wrote $SITE"
  fi

  IMPORT="import sites/*"
  if [ ! -f "$CADDYFILE" ] || is_stock_caddyfile "$CADDYFILE"; then
    # No Caddyfile, or only Caddy's stock placeholder page: just the import.
    printf '# Sites are in %s/sites/, one file per service.\n%s\n' "$CADDY_DIR" "$IMPORT" > "$CADDYFILE"; CHANGED=yes
    note "wrote $CADDYFILE (imports $CADDY_DIR/sites/*)"
  elif ! grep -qE '^\s*import\s+(/etc/caddy/)?sites/\*' "$CADDYFILE"; then
    printf '\n# Added by lodge-wiki/deploy/setup.sh: one file per service in %s/sites/.\n%s\n' "$CADDY_DIR" "$IMPORT" >> "$CADDYFILE"; CHANGED=yes
    note "added '$IMPORT' to $CADDYFILE (your other sites are untouched)"
  fi

  if [ "$CHANGED" = yes ]; then
    if ! caddy validate --config "$CADDYFILE" --adapter caddyfile >/tmp/caddy-validate.log 2>&1; then
      cp -a "$BACKUP/Caddyfile" "$CADDYFILE" 2>/dev/null || rm -f "$CADDYFILE"
      if [ -f "$BACKUP/site" ]; then cp -a "$BACKUP/site" "$SITE"; else rm -f "$SITE"; fi
      tail -n 15 /tmp/caddy-validate.log
      die "Caddy rejected the configuration (above); the previous files are restored"
    fi
    if systemctl is-active -q caddy; then
      systemctl reload caddy && note "reloaded Caddy"
    elif systemctl cat caddy.service >/dev/null 2>&1; then
      systemctl enable -q --now caddy && note "started Caddy"
    else
      warn "Caddy is not run by systemd here: make it import $SITE, then reload it yourself"
    fi
  else
    note "configuration unchanged"
    if ! systemctl is-active -q caddy && systemctl cat caddy.service >/dev/null 2>&1; then
      systemctl enable -q --now caddy && note "started Caddy"
    fi
  fi
  rm -rf "$BACKUP"

  if command -v ufw >/dev/null && [ "$(ufw status 2>/dev/null | head -n 1)" = "Status: active" ]; then
    ufw allow 80/tcp >/dev/null && ufw allow 443/tcp >/dev/null && note "firewall: 80 and 443 open"
  fi
fi

# ---------- summary ----------

say "Summary"
if [ "$(journalctl -u lodge-wiki --since "2 min ago" --no-pager 2>/dev/null | grep -c "Created account" || true)" -gt 0 ]; then
  warn "New wiki: an admin account was created. Its password is in: journalctl -u lodge-wiki | grep 'Created account'"
fi
[ -n "$DOMAIN" ] && note "open https://$DOMAIN"
note "logs: journalctl -u lodge-wiki -f"
say "Done"
