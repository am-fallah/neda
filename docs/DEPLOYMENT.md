# Neda — Deployment Plan (design only)

**Status:** Design for owner review. **No server was accessed, no container was
deployed, and no production data was touched.** Deployment requires explicit
approval of `GATE-004` after this plan and `OPS-005` are reviewed. Nothing in
this document authorizes live MrBilit validation (`GATE-003`) or real
Telegram/Push credentials.

## 1. Target and assumptions

| Item | Assumption / required input |
|---|---|
| Host | Existing low-resource Ubuntu server; owner supplies OS version, CPU/RAM, free disk |
| Container runtime | Docker Engine with Compose v2 |
| Existing services | Owner supplies occupied ports and any existing reverse proxy |
| Domain/DNS | Owner supplies a dedicated hostname resolving to the server |
| TLS | Owner supplies certificate automation (Caddy, certbot, or existing proxy) |
| Public exposure | Only 80/443 reachable; Neda binds `127.0.0.1:8080` |
| Runtime user | A non-root UID/GID (default `1000:1000`; macOS dev user may differ) |
| Data | Bind-mounted host directory `./data`; backup target off-host |
| Secrets | Key file outside Git and outside the database; Compose secret with mode 0400 |
| Resource budget | 1 CPU, 512 MiB memory, 256 PIDs, 10 MiB × 3 log files as starting limits |

Do not invent server facts. Record actual values during the approved deployment
window and adjust limits only from measured behavior.

## 2. Files and ownership

Recommended layout on the host:

```text
/opt/neda/
├── compose.yaml
├── .env                    # owner-only, not in Git
├── data/                   # SQLite database + WAL/SHM
└── secrets/
    └── encryption_key      # base64, exactly 32 bytes, mode 0400, owner = runtime UID
```

The repository's `compose.yaml` bind-mounts `./data` and mounts
`./secrets/encryption_key` through a long-form Compose secret with explicit
`uid`, `gid`, and `mode: 0400`. The runtime UID must own `data/`:

```sh
sudo install -d -m 0700 -o 1000 -g 1000 /opt/neda/data /opt/neda/secrets
sudo install -m 0400 -o 1000 -g 1000 /dev/null /opt/neda/secrets/encryption_key
# Generate the key locally with a trusted tool, then install it with mode 0400.
```

Do not place the key in `.env`, Compose environment variables, image layers, or
the Git working tree. Do not paste it into chat or tickets.

## 3. Build and transfer

Preferred: build on the server from a reviewed, signed-off commit:

```sh
cd /opt/neda
git fetch --tags
git checkout <reviewed-commit>
docker compose build --pull
docker image inspect neda:local --format '{{.Id}} {{.Size}}'
```

If building on the server is undesirable, build in CI, push to a private
registry, pin the digest in `compose.yaml`, and pull by digest. Do not use a
floating `latest` tag. Record the image digest in the deployment log.

The image is multi-stage, runs as `nonroot:nonroot` by default, has no shell or
package manager, uses a read-only root filesystem under Compose, drops all
capabilities, and sets `no-new-privileges`. The Compose override runs as the
owner of `data/` and the secret; that UID must never be 0.

## 4. Configuration

Create `/opt/neda/.env` with owner-only permissions:

```text
NEDA_PUBLIC_URL=https://neda.example.com
NEDA_VAPID_SUBJECT=mailto:owner@example.com
NEDA_RUNTIME_UID=1000
NEDA_RUNTIME_GID=1000
NEDA_TRUST_PROXY=true
NEDA_LOG_LEVEL=info
NEDA_WORKERS=2
```

- `NEDA_PUBLIC_URL` must be the exact public HTTPS origin. Host mismatch returns
  421; do not include a path, query, or fragment.
- `NEDA_TRUST_PROXY=true` is appropriate only when the reverse proxy is the
  local trusted ingress. If the app is exposed directly, keep it `false`.
- `NEDA_VAPID_SUBJECT` is required for Push. A `mailto:` contact or HTTPS URL is
  accepted.
- Validate without starting:

```sh
cd /opt/neda
docker compose config >/dev/null
```

## 5. Reverse proxy and TLS

Terminate TLS at the host proxy. Forward to `127.0.0.1:8080`; preserve the
public `Host` header; set `X-Forwarded-For`/`X-Forwarded-Proto` only from the
trusted proxy; do not add CORS headers.

Example Caddy site (replace the address):

```caddy
neda.example.com {
    encode zstd gzip
    reverse_proxy 127.0.0.1:8080 {
        header_up Host {host}
        header_up X-Forwarded-Proto {scheme}
    }
    log {
        output file /var/log/caddy/neda-access.log
        format json
    }
}
```

If using nginx, set `proxy_set_header Host $host;` and forward client IPs only
from the local proxy. Keep the app port bound to loopback. Do not expose
SQLite, the admin API, or Docker's socket to the internet.

Firewall: allow 22/tcp only from owner-approved administration networks, and
80/443 from the internet (or the proxy's required ports). Deny 8080 externally.

## 6. Initial password and first-run verification

The web app has no unauthenticated bootstrap. Set the owner password offline
with a TTY:

```sh
cd /opt/neda
docker compose run --rm -it --entrypoint /neda neda set-password
```

The command reads the password without echo and writes only an Argon2id hash.
It revokes all sessions. Then:

```sh
docker compose up -d
docker compose ps
curl -fsS https://neda.example.com/healthz
curl -fsS https://neda.example.com/readyz
docker compose logs --tail=50 neda
```

Verify:

1. `/healthz` and `/readyz` return 200 through HTTPS.
2. `http://<server>:8080` is not reachable externally.
3. Login works with the owner password; wrong-password attempts receive 429
   after the configured soft limit.
4. Session/CSRF cookies are `Secure`, `HttpOnly`, `SameSite=Lax` in the browser.
5. Create a monitor with the fake provider only in a non-production environment;
   production intentionally has an empty provider registry until `PRV-009`.
6. Configure Telegram only if authorized; never paste the token into logs.
7. Open Settings → Browser Push, enable this browser, and send a test **only if**
   the owner authorizes a real Push from the reviewed deployment. Otherwise mark
   it not run.
8. Confirm `docker inspect` shows a non-root user, read-only root, dropped
   capabilities, and the expected mounts.

## 7. Backup and restore

Stop-and-copy is the simplest consistent backup:

```sh
cd /opt/neda
docker compose stop neda
sqlite3 data/neda.db 'PRAGMA wal_checkpoint(TRUNCATE);'
tar -C /opt -czf /secure/backups/neda-$(date -u +%Y%m%dT%H%M%SZ).tgz neda/data neda/secrets/encryption_key
docker compose start neda
```

Alternatively use SQLite's online `.backup` while the service runs, then copy
the key separately. Backups must include the database **and** the key; the key
must retain owner-only permissions. Encrypt backups at rest and store them
off-host. Test restore on a fresh directory/volume before relying on them.

Restore:

1. Stop the service.
2. Restore the database and key together to a fresh `data/`/`secrets/` pair.
3. Keep the key owner/mode; start the service.
4. Check `/readyz`, `PRAGMA integrity_check`, and that encrypted settings/Push
   keys decrypt (a wrong key fails startup closed).
5. Re-enable scheduling and verify a synthetic/fake-provider check only.

A local drill on 2026-10-25 restored a database and key to a fresh directory,
preserved mode 0400, kept the VAPID public key stable, passed
`PRAGMA integrity_check`, and failed closed with a wrong key. See
`docs/IMPLEMENTATION_PLAN.md` (`SEC-003`/`OPS-009`) for exact commands. This is
not a substitute for the production-volume drill.

## 8. Rolling update and rollback

Update:

```sh
cd /opt/neda
docker compose pull   # or build from the reviewed commit
docker compose up -d
docker compose ps
curl -fsS https://neda.example.com/readyz
```

Migrations are forward-only. Before an update, take a database+key backup and
record the current image digest. Rollback means:

1. Stop the service.
2. Restore the previous database+key backup if the new version migrated data.
3. Start the previous image digest.
4. Verify health, readiness, login, and scheduling.

Do not run an older binary against a database migrated by a newer schema. Do not
force-push images or overwrite the only backup.

## 9. Operations

- Log rotation: Compose json-file driver is capped at 10 MiB × 3; the proxy must
  also rotate its logs.
- Disk: monitor `data/neda.db`, `-wal`, and backups. SQLite needs free space for
  WAL/checkpoint and vacuum; do not fill the volume.
- Resource limits: start at 1 CPU/512 MiB and adjust only from measured RSS/CPU.
- Health: use the container healthcheck and an external HTTPS probe; alert on
  `/readyz` failures, repeated provider-degraded events, and delivery failures.
- Time: the host clock must be NTP-synced; Tehran display/expiry relies on IANA
  zones embedded in the binary.
- Security: keep Docker and the host patched; do not expose the API without TLS;
  review `docs/SECURITY.md` before changing the proxy, Push allowlist, or secrets.

## 10. Approval checklist

- [ ] Owner supplies host specs, OS, free disk, proxy, domain/TLS, and existing
      port constraints.
- [ ] Owner reviews this plan and `docs/SECURITY.md`.
- [ ] Owner approves `GATE-004` explicitly.
- [ ] Build/digest and rollback image recorded.
- [ ] Key generated locally, installed 0400, owner matches runtime UID, backed up
      separately from the database.
- [ ] `docker compose config`, image smoke, read-only/non-root run, and volume
      write test pass in CI or on the host.
- [ ] Backup/restore drill on the target volume passes.
- [ ] Push/Telegram/live-provider actions are separately authorized; none are
      implied by deployment approval.
