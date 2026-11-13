#!/usr/bin/env bash
# Installs a self-hosted Envi instance: Postgres, Redis, the API and the dashboard, on Docker or pm2,
# with Caddy for HTTPS when there is a domain. Run it through install.envisecrets.com and choose "instance".
#
# Every question can be answered ahead, for an unattended install:
#   ENVI_ADMIN_EMAIL, ENVI_ADMIN_PASSWORD, ENVI_DOMAIN (or "none"), ENVI_RUNTIME (docker or pm2), ENVI_YES=1
# and the local ports, if 3000 or 8080 is taken: ENVI_WEB_PORT, ENVI_API_PORT.
set -euo pipefail

REPO="shellhaki/envi"
DOCS="https://docs.envisecrets.com/self-hosting"
ROOT="${ENVI_HOME:-/opt/envi}"
WEB_PORT="${ENVI_WEB_PORT:-3000}"
API_PORT="${ENVI_API_PORT:-8080}"
LOG="/tmp/envi-install.log"

if [ -t 1 ]; then B=$'\e[1m'; D=$'\e[2m'; G=$'\e[32m'; R=$'\e[31m'; Y=$'\e[33m'; C=$'\e[36m'; N=$'\e[0m'; else B='' D='' G='' R='' Y='' C='' N=''; fi
step_no=0
step() { step_no=$((step_no + 1)); printf '\n%s[%d/7]%s %s%s%s\n' "$C" "$step_no" "$N" "$B" "$*" "$N"; }
ok() { printf '  %s✓%s %s\n' "$G" "$N" "$*"; }
note() { printf '  %s%s%s\n' "$D" "$*" "$N"; }
warn() { printf '  %s!%s %s\n' "$Y" "$N" "$*"; }
fail() {
  printf '\n  %s✗ %s%s\n' "$R" "$*" "$N" >&2
  [ -s "$LOG" ] && { printf '\n  %sLast lines of %s:%s\n' "$D" "$LOG" "$N" >&2; tail -15 "$LOG" | sed 's/^/    /' >&2; }
  printf '\n  Help: %s\n\n' "$DOCS" >&2
  exit 1
}
ask() { local prompt="$1" default="${2:-}" answer; printf '  %s' "$prompt" >/dev/tty; read -r answer </dev/tty || true; printf '%s' "${answer:-$default}"; }
secret() { local prompt="$1" answer; printf '  %s' "$prompt" >/dev/tty; read -rs answer </dev/tty || true; printf '\n' >/dev/tty; printf '%s' "$answer"; }
random_hex() { head -c "$1" /dev/urandom | od -An -tx1 | tr -d ' \n'; }

# run shows a spinner while a long command writes to the log, and stops the install if it fails.
run() {
  local label="$1"; shift
  printf '%s\n--- %s\n' "$(date)" "$label" >>"$LOG"
  ("$@") >>"$LOG" 2>&1 &
  local pid=$! frames='|/-\' i=0
  while kill -0 "$pid" 2>/dev/null; do
    [ -t 1 ] && printf '\r  %s %s ' "${frames:i++%4:1}" "$label"
    sleep 0.2
  done
  if [ -t 1 ]; then printf '\r\033[K'; fi
  wait "$pid" || fail "$label failed"
  ok "$label"
}

banner() {
  printf '\n  %sEnvi%s self-hosted instance\n' "$B" "$N"
  note "Postgres, Redis, the API and the dashboard on this server. Everything is free; nothing is billed."
  note "Docs for every step: $DOCS"
}

# --- 1. checks ----------------------------------------------------------------

preflight() {
  step "Checking this server"
  [ "$(uname -s)" = "Linux" ] || fail "an Envi instance installs on Linux; this is $(uname -s). For your own machine, install the CLI instead."
  if [ "$(id -u)" -ne 0 ]; then
    command -v sudo >/dev/null || fail "run this as root"
    note "needs root for Docker, $ROOT and ports 80/443; asking sudo"
    exec sudo --preserve-env=ENVI_HOME,ENVI_SOURCE,ENVI_REF,ENVI_WEB_PORT,ENVI_API_PORT,ENVI_ADMIN_EMAIL,ENVI_ADMIN_PASSWORD,ENVI_DOMAIN,ENVI_RUNTIME,ENVI_YES bash "$0" "$@"
  fi
  : >"$LOG"
  for tool in curl tar od; do command -v "$tool" >/dev/null || fail "'$tool' is needed; install it and run this again"; done
  if [ -f "$ROOT/.env" ]; then
    printf '\n  Envi is already installed in %s.\n' "$ROOT"
    note "Reinstalling would replace its encryption key and lose every secret, so this stops here."
    printf '  Update it with:  %ssudo envi-instance update%s\n  See its URLs:    %senvi-instance urls%s\n\n' "$B" "$N" "$B" "$N"
    exit 0
  fi
  if ! command -v docker >/dev/null; then
    warn "Docker is not installed. Postgres and Redis run in Docker even with pm2."
    [ "$(ask "Install Docker now with get.docker.com? [Y/n] " y)" = n ] && fail "Docker is needed; install it and run this again"
    run "Installing Docker" sh -c 'curl -fsSL https://get.docker.com | sh'
  fi
  docker compose version >/dev/null 2>&1 || fail "the Docker Compose plugin is missing (docker compose version fails)"
  for port in "$WEB_PORT" "$API_PORT"; do
    if (: </dev/tcp/127.0.0.1/"$port") 2>/dev/null; then
      fail "port $port is already in use; free it, or choose others: ENVI_WEB_PORT=3100 ENVI_API_PORT=8180"
    fi
  done
  ok "Linux, root, Docker $(docker version --format '{{.Server.Version}}' 2>/dev/null || echo ready)"
}

# --- 2. questions ---------------------------------------------------------------

questions() {
  step "A few questions"

  note "The admin sees every project and adds everyone else. Sign in with this email and password."
  ADMIN_EMAIL="${ENVI_ADMIN_EMAIL:-}"
  while :; do
    [ -n "$ADMIN_EMAIL" ] || ADMIN_EMAIL=$(ask "Admin email: ")
    [[ "$ADMIN_EMAIL" =~ ^[^[:space:]@]+@[^[:space:]@]+\.[^[:space:]@]+$ ]] && break
    warn "that doesn't look like an email address"; ADMIN_EMAIL=""
  done
  ADMIN_PASSWORD="${ENVI_ADMIN_PASSWORD:-}"; GENERATED=0
  while [ -z "${ENVI_ADMIN_PASSWORD:-}" ]; do
    ADMIN_PASSWORD=$(secret "Admin password (at least 10 characters, or Enter to generate one): ")
    if [ -z "$ADMIN_PASSWORD" ]; then ADMIN_PASSWORD="$(random_hex 4)-$(random_hex 4)-$(random_hex 4)"; GENERATED=1; break; fi
    if [ "${#ADMIN_PASSWORD}" -lt 10 ]; then warn "at least 10 characters"; continue; fi
    [ "$(secret "Repeat it: ")" = "$ADMIN_PASSWORD" ] && { GENERATED=0; break; }
    warn "they don't match"
  done
  [ "${#ADMIN_PASSWORD}" -ge 10 ] || fail "the admin password needs at least 10 characters"

  printf '\n'
  note "With a domain, Envi gets HTTPS from Let's Encrypt. Without one, it runs on this server's IP over plain HTTP."
  DOMAIN="${ENVI_DOMAIN:-}"
  [ -n "$DOMAIN" ] || DOMAIN=$(ask "Domain (e.g. envi.example.com, or Enter for none): ")
  if [ "$DOMAIN" = none ]; then DOMAIN=""; fi
  DOMAIN=$(printf '%s' "$DOMAIN" | tr '[:upper:]' '[:lower:]')
  DOMAIN="${DOMAIN#http://}"; DOMAIN="${DOMAIN#https://}"; DOMAIN="${DOMAIN%%/*}"
  IP=$(curl -fsS --max-time 5 https://checkip.amazonaws.com 2>/dev/null | tr -d '[:space:]' || true)
  [ -n "$IP" ] || IP=$(hostname -I 2>/dev/null | awk '{print $1}')
  if [ -n "$DOMAIN" ]; then
    note "These three names must point at this server ($IP) with A records:"
    for name in "$DOMAIN" "api.$DOMAIN" "docs.$DOMAIN"; do
      resolved=$(getent ahostsv4 "$name" 2>/dev/null | awk 'NR==1{print $1}')
      if [ "$resolved" = "$IP" ]; then ok "$name → $IP"
      elif [ -z "$resolved" ]; then warn "$name doesn't resolve yet"
      else warn "$name points at $resolved, not $IP"
      fi
    done
    note "Certificates are issued once they do; until then HTTPS fails. You can fix DNS after installing."
  else
    warn "no domain: sign-ins and secrets cross the network unencrypted. Use this on a private network or add a domain later."
  fi

  printf '\n'
  note "How should the API and dashboard run? Postgres and Redis are in Docker either way."
  printf '  %s1)%s Docker   everything in containers, nothing else to install %s(recommended)%s\n' "$B" "$N" "$D" "$N"
  printf '  %s2)%s pm2      the API and dashboard as processes on this server (needs Node.js 20+; Bun and pm2 are installed)\n' "$B" "$N"
  RUNTIME="${ENVI_RUNTIME:-}"
  [ -n "$RUNTIME" ] || case "$(ask "Choose [1]: " 1)" in 2) RUNTIME=pm2 ;; *) RUNTIME=docker ;; esac
  [ "$RUNTIME" = docker ] || [ "$RUNTIME" = pm2 ] || fail "ENVI_RUNTIME must be docker or pm2"

  if [ -n "$DOMAIN" ]; then
    WEB_URL="https://$DOMAIN"; API_URL="https://api.$DOMAIN"; DOCS_URL="https://docs.$DOMAIN"; PUBLISH_HOST=127.0.0.1; PLAIN=0
  else
    WEB_URL="http://$IP:$WEB_PORT"; API_URL="http://$IP:$API_PORT"; DOCS_URL="https://docs.envisecrets.com"; PUBLISH_HOST=0.0.0.0; PLAIN=1
  fi

  printf '\n  %sAbout to install:%s\n' "$B" "$N"
  printf '    Admin       %s\n    Dashboard   %s\n    API         %s\n    Runs on     %s\n    Folder      %s\n' "$ADMIN_EMAIL" "$WEB_URL" "$API_URL" "$RUNTIME" "$ROOT"
  [ "${ENVI_YES:-}" = 1 ] || { [ "$(ask "Go ahead? [Y/n] " y)" = n ] && { printf '  Nothing was changed.\n\n'; exit 0; }; }
}

# --- 3. source ----------------------------------------------------------------

fetch_source() {
  step "Getting Envi"
  mkdir -p "$ROOT/data"
  if [ -n "${ENVI_SOURCE:-}" ]; then
    run "Copying $ENVI_SOURCE" cp -R "$ENVI_SOURCE" "$ROOT/src"
    REF="local"
    return
  fi
  REF="${ENVI_REF:-}"
  if [ -z "$REF" ]; then
    REF=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" | grep '"tag_name"' | head -1 | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/' || true)
  fi
  [ -n "$REF" ] || fail "couldn't find the latest release; set ENVI_REF, e.g. ENVI_REF=v1.5.0"
  run "Downloading $REF" sh -c "mkdir -p '$ROOT/src' && curl -fsSL 'https://github.com/$REPO/archive/$REF.tar.gz' | tar -xz -C '$ROOT/src' --strip-components=1"
  [ -f "$ROOT/src/selfhost/compose.yml" ] || fail "$REF is older than self-hosting; set ENVI_REF to a newer release"
}

# --- 4. configuration -----------------------------------------------------------

configure() {
  step "Writing the configuration"
  umask 077
  cat >"$ROOT/.env" <<EOF
# Envi instance settings. Back this file up somewhere safe:
# ENVI_ENCRYPTION_KEY decrypts every secret, and it can't be recovered.
ENVI_MODE=self-hosted
ENVI_RUNTIME=$RUNTIME
ENVI_SOURCE_REF=$REF
ENVI_ENCRYPTION_KEY=$(random_hex 16)
ENVI_SESSION_SECRET=$(random_hex 32)
POSTGRES_PASSWORD=$(random_hex 16)
ENVI_WEB_URL=$WEB_URL
ENVI_PUBLIC_API_URL=$API_URL
ENVI_DOCS_URL=$DOCS_URL
ENVI_PLAIN_HTTP=$PLAIN
PUBLISH_HOST=$PUBLISH_HOST
WEB_PORT=$WEB_PORT
API_PORT=$API_PORT
COMPOSE_PROFILES=$( [ "$RUNTIME" = docker ] && printf app )$( [ -n "$DOMAIN" ] && { [ "$RUNTIME" = docker ] && printf ,; printf https; } )
EOF
  ok "$ROOT/.env with fresh keys"

  local escaped; escaped=$(printf '%s' "$ADMIN_PASSWORD" | sed 's/\\/\\\\/g; s/"/\\"/g')
  cat >"$ROOT/data/users.toml" <<EOF
# The people who can sign in to this Envi instance.
#
# Forgot a password? Replace that person's hash line with
#   password = "a new password"
# then sign in with it. Envi hashes it and rewrites the line.

[[user]]
email = "$ADMIN_EMAIL"
role = "admin"
password = "$escaped"
EOF
  ok "$ROOT/data/users.toml with the admin (the password is hashed on first start)"

  cp "$ROOT/src/selfhost/compose.yml" "$ROOT/compose.yml"
  if [ "$RUNTIME" = pm2 ]; then cp "$ROOT/src/selfhost/ecosystem.config.cjs" "$ROOT/ecosystem.config.cjs"; fi
  if [ -n "$DOMAIN" ]; then
    printf '%s, docs.%s {\n\treverse_proxy 127.0.0.1:%s\n}\n\napi.%s {\n\treverse_proxy 127.0.0.1:%s\n}\n' "$DOMAIN" "$DOMAIN" "$WEB_PORT" "$DOMAIN" "$API_PORT" >"$ROOT/Caddyfile"
    ok "$ROOT/Caddyfile for $DOMAIN, api.$DOMAIN and docs.$DOMAIN"
  fi
  install -m 755 "$ROOT/src/selfhost/envi-instance" /usr/local/bin/envi-instance
  ok "envi-instance command"
  umask 022
}

# --- 5. database ----------------------------------------------------------------

database() {
  step "Starting Postgres and Redis"
  compose() { docker compose --project-directory "$ROOT" -f "$ROOT/compose.yml" "$@"; }
  run "Starting the database containers" compose up -d postgres redis
  # Over TCP: on first start the image runs a socket-only server while it initialises, then restarts.
  run "Waiting for Postgres" sh -c "for i in \$(seq 90); do docker compose --project-directory '$ROOT' -f '$ROOT/compose.yml' exec -T postgres pg_isready -h 127.0.0.1 -U envi >/dev/null 2>&1 && exit 0; sleep 1; done; exit 1"
  run "Creating the tables" sh -c "docker compose --project-directory '$ROOT' -f '$ROOT/compose.yml' exec -T postgres psql -q -U envi envi -v ON_ERROR_STOP=1 < '$ROOT/src/migrations/schema.sql'"
}

# --- 6. the app -------------------------------------------------------------------

app() {
  step "Starting Envi on $RUNTIME"
  if [ "$RUNTIME" = docker ]; then
    note "Building the images takes a few minutes the first time."
    run "Building and starting the API and dashboard" docker compose --project-directory "$ROOT" -f "$ROOT/compose.yml" up -d --build
  else
    command -v node >/dev/null && [ "$(node -p 'process.versions.node.split(".")[0]')" -ge 20 ] || fail "pm2 needs Node.js 20 or newer; install it, or choose Docker"
    if ! command -v bun >/dev/null; then
      run "Installing Bun" sh -c 'for i in 1 2 3; do curl -fsSL https://bun.sh/install | bash && exit 0; echo "retrying in 5s"; sleep 5; done; exit 1'
      export PATH="$HOME/.bun/bin:$PATH"
    fi
    printf 'BUN_PATH=%s\n' "$(command -v bun)" >>"$ROOT/.env"
    command -v pm2 >/dev/null || run "Installing pm2" npm install -g pm2
    run "Installing the API" sh -c "cd '$ROOT/src/server' && bun install --frozen-lockfile --production"
    note "Building the dashboard takes a few minutes."
    run "Building the dashboard" sh -c "cd '$ROOT/src/web' && bun install --frozen-lockfile && bunx --bun next build --webpack && cp -r .next/static .next/standalone/.next/static && cp -r public .next/standalone/public"
    run "Starting the API and dashboard" pm2 start "$ROOT/ecosystem.config.cjs"
    if { pm2 save && pm2 startup systemd -u root --hp /root; } >>"$LOG" 2>&1; then ok "Starting them again after a reboot"
    else warn "pm2 couldn't set itself up to start on boot (no systemd?). After a reboot, run: pm2 resurrect"
    fi
    if [ -n "$DOMAIN" ]; then run "Starting Caddy" docker compose --project-directory "$ROOT" -f "$ROOT/compose.yml" up -d caddy; fi
  fi
}

# --- 7. checks and the summary ----------------------------------------------------

verify() {
  step "Checking it works"
  run "API answering" sh -c "for i in \$(seq 90); do curl -fsS http://127.0.0.1:$API_PORT/health >/dev/null 2>&1 && exit 0; sleep 2; done; exit 1"
  run "Dashboard answering" sh -c "for i in \$(seq 90); do curl -fsS -o /dev/null http://127.0.0.1:$WEB_PORT/auth 2>/dev/null && exit 0; sleep 2; done; exit 1"
  if [ -n "$DOMAIN" ]; then
    if curl -fsS --max-time 60 -o /dev/null "$API_URL/health" 2>/dev/null; then ok "HTTPS on $DOMAIN"
    else warn "HTTPS isn't answering yet. It starts once DNS points here; check with: envi-instance status"
    fi
  fi
}

summary() {
  printf '\n  %s%sEnvi is running.%s\n\n' "$G" "$B" "$N"
  printf '  %-11s %s\n  %-11s %s\n' "Dashboard" "$WEB_URL" "" "${D}sign in, create projects, add people on the Users page${N}"
  printf '  %-11s %s\n  %-11s %s\n' "API" "$API_URL" "" "${D}connect the CLI: envi set-url $API_URL && envi auth${N}"
  printf '  %-11s %s\n\n' "Docs" "$DOCS_URL"
  printf '  %-11s %s\n' "Admin" "$ADMIN_EMAIL"
  if [ "${GENERATED:-0}" = 1 ]; then
    printf '  %-11s %s%s%s  %s(shown once; change it in Settings)%s\n' "Password" "$B" "$ADMIN_PASSWORD" "$N" "$D" "$N"
  else
    printf '  %-11s %s\n' "Password" "the one you chose"
  fi
  printf '\n  %-11s %s  %s\n' "Users file" "$ROOT/data/users.toml" "${D}who can sign in; reset a forgotten password here${N}"
  printf '  %-11s %s  %s\n' "Settings" "$ROOT/.env" "${D}back it up: its ENVI_ENCRYPTION_KEY decrypts every secret${N}"
  printf '  %-11s %s\n' "Manage" "envi-instance status | logs | restart | backup | update | urls"
  printf '  %-11s %s\n\n' "Help" "$DOCS"
}

banner
preflight "$@"
questions
fetch_source
configure
database
app
verify
summary
