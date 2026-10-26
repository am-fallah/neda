#!/bin/sh
# One-time, non-destructive setup for a Docker Compose deployment.
# Creates .env, ./data, and ./secrets/encryption_key. It refuses to overwrite
# an existing file so an existing database/key pair cannot be replaced.
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
REPO_DIR=$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)
cd "$REPO_DIR"

die() {
    printf 'neda setup: %s\n' "$*" >&2
    exit 1
}

say() {
    printf '%s\n' "$*"
}

[ -f compose.yaml ] || die "compose.yaml not found; run this from a Neda checkout"
[ -f Dockerfile ] || die "Dockerfile not found; run this from a Neda checkout"
command -v openssl >/dev/null 2>&1 || die "openssl is required to generate the encryption key"

if [ -e .env ]; then
    die ".env already exists; refusing to overwrite it (see docs/DEPLOYMENT.md)"
fi
if [ -e secrets/encryption_key ]; then
    die "secrets/encryption_key already exists; refusing to replace a key that may decrypt the existing database"
fi
if [ -e data/neda.db ]; then
    die "data/neda.db already exists; refusing first-time setup over an existing database"
fi

runtime_uid=$(id -u)
runtime_gid=$(id -g)
[ "$runtime_uid" -ne 0 ] || die "run as the non-root deployment user that will own ./data and ./secrets"

allow_insecure_http=${NEDA_ALLOW_INSECURE_HTTP:-false}
case "$allow_insecure_http" in
    true|false) ;;
    *) die "NEDA_ALLOW_INSECURE_HTTP must be true or false" ;;
esac
if [ "$allow_insecure_http" = "true" ]; then
    default_publish_host=0.0.0.0
    prompt='Public HTTP origin (http://SERVER_IP:PORT): '
    trust_proxy=${NEDA_TRUST_PROXY:-false}
else
    default_publish_host=127.0.0.1
    prompt='Public HTTPS origin (https://neda.example.com): '
    trust_proxy=${NEDA_TRUST_PROXY:-true}
fi
publish_host=${NEDA_PUBLISH_HOST:-$default_publish_host}
publish_port=${NEDA_PUBLISH_PORT:-}

public_url=${NEDA_PUBLIC_URL:-}
if [ -z "$public_url" ]; then
    [ -t 0 ] || die "NEDA_PUBLIC_URL is required when stdin is not a terminal"
    printf '%s' "$prompt"
    read -r public_url
fi
vapid_subject=${NEDA_VAPID_SUBJECT:-}
if [ -z "$vapid_subject" ]; then
    [ -t 0 ] || die "NEDA_VAPID_SUBJECT is required when stdin is not a terminal"
    printf 'VAPID contact (mailto:owner@example.com or https://...): '
    read -r vapid_subject
fi

case "$public_url" in
    *[[:space:]]*|*"'"*) die "NEDA_PUBLIC_URL must not contain whitespace or single quotes" ;;
esac
if [ "$allow_insecure_http" = "true" ]; then
    case "$public_url" in
        http://*) host_part=${public_url#http://} ;;
        *) die "NEDA_PUBLIC_URL must start with http:// when NEDA_ALLOW_INSECURE_HTTP=true" ;;
    esac
else
    case "$public_url" in
        https://*) host_part=${public_url#https://} ;;
        *) die "NEDA_PUBLIC_URL must start with https:// (set NEDA_ALLOW_INSECURE_HTTP=true only for direct IP HTTP access)" ;;
    esac
fi
[ -n "$host_part" ] || die "NEDA_PUBLIC_URL must include a host"
case "$host_part" in
    */*|*\?*|*#*|*@*) die "NEDA_PUBLIC_URL must be an origin only (no path, query, fragment, or userinfo)" ;;
esac

if [ "$allow_insecure_http" = "true" ]; then
    case "$host_part" in
        \[*\]:*)
            url_port=${host_part##*:}
            host_only=${host_part%:*}
            host_only=${host_only#[}
            host_only=${host_only%]}
            ;;
        \[*\])
            url_port=80
            host_only=${host_part#[}
            host_only=${host_only%]}
            ;;
        *:*)
            url_port=${host_part##*:}
            host_only=${host_part%:*}
            ;;
        *)
            url_port=80
            host_only=$host_part
            ;;
    esac
    case "$host_only" in
        *:*) ;;
        *[!0-9.]*|.*|*..*|*.) die "direct HTTP mode requires an IPv4 or IPv6 literal in NEDA_PUBLIC_URL" ;;
        *)
            old_ifs=$IFS
            IFS=.
            set -- $host_only
            IFS=$old_ifs
            [ "$#" -eq 4 ] || die "direct HTTP mode requires a valid IPv4 or IPv6 literal in NEDA_PUBLIC_URL"
            for octet in "$@"; do
                case "$octet" in
                    ""|*[!0-9]*) die "direct HTTP mode requires a valid IPv4 or IPv6 literal in NEDA_PUBLIC_URL" ;;
                esac
                [ "$octet" -ge 0 ] 2>/dev/null && [ "$octet" -le 255 ] || die "direct HTTP mode requires a valid IPv4 or IPv6 literal in NEDA_PUBLIC_URL"
            done
            ;;
    esac
    if [ -z "$publish_port" ]; then
        publish_port=$url_port
    fi
    [ "$publish_port" = "$url_port" ] || die "NEDA_PUBLISH_PORT must match the port in NEDA_PUBLIC_URL ($url_port)"
else
    if [ -z "$publish_port" ]; then
        publish_port=8080
    fi
fi
case "$publish_port" in
    *[!0-9]*|"") die "NEDA_PUBLISH_PORT must be a TCP port number" ;;
esac
[ "$publish_port" -ge 1 ] 2>/dev/null && [ "$publish_port" -le 65535 ] || die "NEDA_PUBLISH_PORT must be between 1 and 65535"

case "$vapid_subject" in
    *[[:space:]]*|*"'"*) die "NEDA_VAPID_SUBJECT must not contain whitespace or single quotes" ;;
    mailto:*) [ -n "${vapid_subject#mailto:}" ] || die "NEDA_VAPID_SUBJECT mailto address is empty" ;;
    https://*) [ -n "${vapid_subject#https://}" ] || die "NEDA_VAPID_SUBJECT HTTPS URL is empty" ;;
    *) die "NEDA_VAPID_SUBJECT must be a mailto: address or https:// URL" ;;
esac

umask 077
mkdir -p data secrets
[ -w data ] || die "./data is not writable by $(id -un); run: sudo chown -R $(id -u):$(id -g) data"
[ -w secrets ] || die "./secrets is not writable by $(id -un); run: sudo chown -R $(id -u):$(id -g) secrets"
chmod 700 data secrets

key_tmp=$(mktemp ./secrets/.encryption_key.XXXXXX)
env_tmp=$(mktemp ./.env.XXXXXX)
cleanup() {
    [ -z "${key_tmp:-}" ] || rm -f "$key_tmp"
    [ -z "${env_tmp:-}" ] || rm -f "$env_tmp"
}
trap cleanup EXIT HUP INT TERM

openssl rand -base64 32 > "$key_tmp"
chmod 0400 "$key_tmp"
key_bytes=$(openssl base64 -d -A < "$key_tmp" | wc -c | tr -d '[:space:]')
[ "$key_bytes" = "32" ] || die "generated key did not decode to 32 bytes"

chmod 0600 "$env_tmp"
{
    printf '# Generated by scripts/setup.sh. Keep owner-only; never commit.\n'
    printf "NEDA_PUBLIC_URL='%s'\n" "$public_url"
    printf "NEDA_VAPID_SUBJECT='%s'\n" "$vapid_subject"
    printf 'NEDA_MRBILIT_ENABLED=true\n'
    printf 'NEDA_ALLOW_INSECURE_HTTP=%s\n' "$allow_insecure_http"
    printf 'NEDA_PUBLISH_HOST=%s\n' "$publish_host"
    printf 'NEDA_PUBLISH_PORT=%s\n' "$publish_port"
    printf 'NEDA_RUNTIME_UID=%s\n' "$runtime_uid"
    printf 'NEDA_RUNTIME_GID=%s\n' "$runtime_gid"
    if [ "$allow_insecure_http" = "true" ]; then
        printf '# Direct public-IP HTTP mode: unencrypted. Firewall the published port.\n'
    else
        printf '# The container port is loopback-only; a local TLS proxy is the ingress.\n'
    fi
    printf 'NEDA_TRUST_PROXY=%s\n' "$trust_proxy"
} > "$env_tmp"

[ ! -e .env ] || die ".env appeared during setup; refusing to overwrite it"
mv "$env_tmp" .env
env_tmp=""

[ ! -e secrets/encryption_key ] || die "encryption key appeared during setup; refusing to overwrite it"
mv "$key_tmp" secrets/encryption_key
key_tmp=""

file_uid() {
    stat -c '%u' "$1" 2>/dev/null || stat -f '%u' "$1"
}
file_mode() {
    stat -c '%a' "$1" 2>/dev/null || stat -f '%Lp' "$1"
}
[ "$(file_uid secrets/encryption_key)" = "$runtime_uid" ] || die "encryption key is not owned by uid $runtime_uid"
[ "$(file_mode secrets/encryption_key)" = "400" ] || die "encryption key mode is not 0400"

say "Created .env (mode 0600), ./data, and ./secrets/encryption_key (owner uid $runtime_uid, mode 0400)."
say "The key value was not printed. Back up the database and key together."
say ""
if [ "$allow_insecure_http" = "true" ]; then
    say "WARNING: plain HTTP is unencrypted. Use it only on a network you trust"
    say "or behind a VPN/TLS tunnel, and expose only TCP $publish_port."
    say "Web Push requires a secure browser context and will be unavailable."
    say ""
fi
say "Next:"
say "  docker compose run --rm -it --entrypoint /neda neda set-password"
say "  docker compose up -d"
say "Then check: docker compose ps; docker compose logs --tail=50 neda"
if [ "$allow_insecure_http" = "true" ]; then
    say "Open: $public_url"
    say "Health: curl -fsS $public_url/readyz"
else
    say "See docs/DEPLOYMENT.md for DNS/TLS, backup, update, and rollback steps."
fi

if command -v docker >/dev/null 2>&1; then
    if docker compose version >/dev/null 2>&1; then
        if ! docker compose config >/dev/null; then
            die "docker compose config rejected the generated configuration (see the error above)"
        fi
        say "Compose configuration validated."
    else
        say "WARNING: Docker Compose v2 is not available; install it before starting."
    fi
else
    say "WARNING: docker was not found; install Docker Engine with Compose v2 before starting."
fi
