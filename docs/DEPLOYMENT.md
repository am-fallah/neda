# Neda — Docker Compose Deployment

**Status:** Deployment files and a first-time setup script are present. **No
server was accessed, no container was deployed, and no production data was
touched.** Deployment requires explicit approval of `GATE-004` after this plan
and `OPS-005` are reviewed. Nothing in this document authorizes live MrBilit
traffic, real Telegram/Push credentials, or deployment.

## 1. Target and assumptions

| Item | Assumption / required input |
|---|---|
| Host | Existing low-resource Ubuntu server; owner supplies OS version, CPU/RAM, free disk |
| Container runtime | Docker Engine with Compose v2 |
| Existing services | Owner supplies occupied ports and any existing reverse proxy |
| Domain/DNS | Required for the HTTPS proxy mode; not required for direct public-IP HTTP mode |
| TLS | Required for the HTTPS proxy mode; absent by design in direct HTTP mode |
| Public exposure | Proxy mode: only 80/443 reachable; direct HTTP mode: only the chosen `NEDA_PUBLISH_PORT` |
| Runtime user | Non-root deployment user's UID/GID; `scripts/setup.sh` records the current values |
| Data | Bind-mounted host directory `./data`; backup target off-host |
| Secrets | Key file outside Git and the database; read-only bind mount, host owner = runtime UID, mode `0400`/`0600` |
| Prerequisites | Docker Engine with Compose v2, `openssl`, `git`, and either DNS/TLS for proxy mode or a public IP and chosen port for direct HTTP mode |
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

Run the first-time setup as the non-root deployment user that owns the checkout:

```sh
cd /opt/neda
./scripts/setup.sh
# Non-interactive HTTPS proxy form:
# NEDA_PUBLIC_URL=https://neda.example.com \
# NEDA_VAPID_SUBJECT=mailto:owner@example.com ./scripts/setup.sh
# Non-interactive direct public-IP HTTP form (unencrypted):
# NEDA_ALLOW_INSECURE_HTTP=true \
# NEDA_PUBLIC_URL=http://203.0.113.10:8080 \
# NEDA_VAPID_SUBJECT=mailto:owner@example.com ./scripts/setup.sh
```

The script creates `.env` (mode 0600), `data/`, and
`secrets/encryption_key` (owner = current UID/GID, mode 0400), without printing
the key. It refuses to run if `.env`, the key, or the database already exists,
so it cannot silently replace a key or data. It also refuses to run as root.
Re-run it only for a genuinely new install. In direct HTTP mode it writes
`NEDA_PUBLISH_HOST=0.0.0.0`, sets the published port from the URL, and keeps
`NEDA_TRUST_PROXY=false`; the secure proxy default remains loopback-only.

The repository's `compose.yaml` bind-mounts `./data` read-write and
`./secrets/encryption_key` read-only at `/run/secrets/encryption_key`. The host
key owner must equal `NEDA_RUNTIME_UID` and its mode must remain `0400` or `0600`;
the service enforces both. File-backed Compose secrets are bind mounts in
non-swarm Compose and their `uid`/`gid`/`mode` fields are ignored, so this
repository deliberately uses an explicit bind mount instead. This design targets
Linux Docker Engine/Compose v2. It has not been run against Docker Desktop,
rootless UID remapping, or a remote Docker daemon. On any of those platforms,
confirm the container sees the key as a regular file owned by the runtime UID
before relying on the deployment.

Do not place the key in `.env`, Compose environment variables, image layers, or
the Git working tree. Do not paste it into chat or tickets. The key and the
database must be backed up together.

## 3. Build and transfer

The Compose service sets `pull_policy: build`: it builds `neda:local` from this
checkout and does not try to pull that mutable tag from a registry. The normal
first start is therefore:

```sh
cd /opt/neda
docker compose up -d
docker image inspect neda:local --format '{{.Id}} {{.Size}}'
```

No manual `docker build` is required. Repeated `up` commands rebuild from source
but reuse Docker's layer cache when nothing changed. `docker compose restart
neda` restarts the existing container without invoking a build. For a reviewed
update, build from the checked-out commit before recreating the container:

```sh
git fetch --tags
git checkout <reviewed-commit>
docker compose build --pull
docker compose up -d
```

If building on the server is undesirable, build in CI, push to a private
registry, pin the digest in `compose.yaml`, and pull by digest. That path
requires removing/replacing the local `build` section and `pull_policy: build`
with the digest-pinned `image` and an appropriate pull policy. Do not use a
floating `latest` tag. Record the image ID/digest in the deployment log.

The image is multi-stage, runs as `nonroot:nonroot` by default, has no shell or
package manager, uses a read-only root filesystem under Compose, drops all
capabilities, and sets `no-new-privileges`. Compose runs the container as the
owner of `data/` and the key (`NEDA_RUNTIME_UID`/`NEDA_RUNTIME_GID`); that UID
must never be 0, and the application independently refuses UID 0 in production.

## 4. Configuration

`scripts/setup.sh` creates `/opt/neda/.env` with owner-only permissions and the
current non-root UID/GID. Its effective contents are:

```text
NEDA_PUBLIC_URL=https://neda.example.com
NEDA_VAPID_SUBJECT=mailto:owner@example.com
NEDA_MRBILIT_ENABLED=true
NEDA_ALLOW_INSECURE_HTTP=false
NEDA_PUBLISH_HOST=127.0.0.1
NEDA_PUBLISH_PORT=8080
NEDA_RUNTIME_UID=<deployment user uid>
NEDA_RUNTIME_GID=<deployment user gid>
NEDA_TRUST_PROXY=true
```

Edit `.env` only for deliberate changes; do not put the encryption key in it.
`NEDA_LOG_LEVEL` and `NEDA_WORKERS` are optional and default through Compose.

- `NEDA_PUBLIC_URL` must be the exact public origin. Host mismatch returns 421;
  do not include a path, query, or fragment. The secure default is HTTPS.
- `NEDA_ALLOW_INSECURE_HTTP=true` is the explicit opt-in for direct HTTP access
  by IP. It permits only `http://<IP-literal>[:PORT]` in production and is
  rejected for hostnames. It disables Secure cookies and HSTS.
- `NEDA_PUBLISH_HOST`/`NEDA_PUBLISH_PORT` select the host interface and port
  published by Compose. Keep `127.0.0.1` for proxy mode; use `0.0.0.0` and the
  chosen public port for direct HTTP access.
- `NEDA_TRUST_PROXY=true` is appropriate only when the reverse proxy is the
  local trusted ingress. Direct HTTP exposure must use `false` because there is
  no trusted proxy.
- `NEDA_VAPID_SUBJECT` is required for Push. A `mailto:` contact or HTTPS URL is
  accepted. Browser Push needs a secure context, so it is unavailable in direct
  HTTP mode.
- `NEDA_MRBILIT_ENABLED=true` is required in production. The image does not
  enable it by default, and `compose.yaml` refuses to render if the variable is
  unset, so production registration is an explicit opt-in. Startup fails before
  opening the database if the real provider is not enabled; it also fails if any
  active monitor references an unregistered provider. A development database
  containing active fake-provider targets must have those targets removed or
  deactivated before production startup.
- The application itself refuses to start in production when its effective UID
  is `0`, even if a Compose override or launcher supplies root.
- Validate without starting:

```sh
cd /opt/neda
docker compose config >/dev/null
```

### 4.1 Direct public-IP HTTP mode

Use this only when there is no domain/TLS and the network is trusted, or when
traffic is protected by a VPN or another secure tunnel. Plain HTTP exposes the
login password and session cookies to anyone who can observe the path.

Example `/opt/neda/.env` for `http://203.0.113.10:8080`:

```text
NEDA_PUBLIC_URL=http://203.0.113.10:8080
NEDA_ALLOW_INSECURE_HTTP=true
NEDA_PUBLISH_HOST=0.0.0.0
NEDA_PUBLISH_PORT=8080
NEDA_TRUST_PROXY=false
NEDA_VAPID_SUBJECT=mailto:owner@example.com
NEDA_MRBILIT_ENABLED=true
NEDA_RUNTIME_UID=<deployment user uid>
NEDA_RUNTIME_GID=<deployment user gid>
```

Then start normally:

```sh
docker compose up -d
curl -fsS http://203.0.113.10:8080/readyz
```

The public URL host and port must exactly match the browser address. For a
non-default port, put it in both `NEDA_PUBLIC_URL` and `NEDA_PUBLISH_PORT`.
For a direct (non-Compose) binary, bind the process with
`NEDA_HTTP_ADDR=0.0.0.0:<port>` instead of using `NEDA_PUBLISH_*`.
Firewall only the chosen port; do not expose Docker's control socket, the
database, or any admin interface. Direct HTTP mode intentionally omits
`Strict-Transport-Security` and does not mark browser cookies `Secure`. HTTPS
domain mode remains unchanged and is strongly preferred outside an isolated
network. Compose still requires a VAPID contact (`NEDA_VAPID_SUBJECT`); set a
`mailto:` value, but note that browser Push cannot be enabled from an HTTP
origin.

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

Complete section 2 (`scripts/setup.sh`) first. `docker compose run` builds the
image on first use if it is not present. The web app has no unauthenticated
bootstrap; set the owner password offline with a TTY:

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

Verify the mode you deployed.

For HTTPS proxy mode:

1. `/healthz` and `/readyz` return 200 through HTTPS.
2. `http://<server>:8080` is not reachable externally.
3. Login works with the owner password; wrong-password attempts receive 429
   after the configured soft limit.
4. Session/CSRF cookies are `Secure`, `HttpOnly`, `SameSite=Lax` in the browser.

For direct public-IP HTTP mode:

1. `http://<public-ip>:<port>/healthz` and `/readyz` return 200.
2. Only the chosen port is reachable externally; the Docker control socket and
   database are not exposed.
3. Login works, the Host/Origin checks accept the same IP and port, and cookies
   are `HttpOnly`/`SameSite=Lax` but **not** `Secure`.
4. Responses do not include `Strict-Transport-Security`. Browser Push is
   unavailable because the origin is not a secure context.

In both modes:

1. Do not create a fake-provider monitor in production. Verify the authenticated
   Providers view lists the real MrBilit provider with sane health metadata.
   Production startup must have failed closed if `NEDA_MRBILIT_ENABLED` was
   missing or false.
2. Configure Telegram only if authorized; never paste the token into logs.
3. Open Settings → Browser Push, enable this browser, and send a test **only if**
   the owner authorizes a real Push from the reviewed deployment. Otherwise mark
   it not run. This is not available in direct HTTP mode.
4. Confirm `docker inspect` shows a non-root user, read-only root, dropped
   capabilities, and the expected mounts.

## 7. Backup and restore

Stop-and-copy is the simplest consistent backup. A clean stop closes SQLite and
the archive includes any WAL/SHM sidecars, so no host `sqlite3` package is
required:

```sh
cd /opt/neda
docker compose stop neda
tar -czf /secure/backups/neda-$(date -u +%Y%m%dT%H%M%SZ).tgz data secrets/encryption_key
docker compose start neda
```

Alternatively use SQLite's online `.backup` while the service runs, then copy
the key separately. Backups must include the database **and** the key; the key
must retain owner-only permissions. Encrypt backups at rest and store them
off-host. Test restore on a fresh directory/volume before relying on them.

Restore:

1. Stop the service.
2. Restore the database and key together to a fresh `data/`/`secrets/` pair.
   Never restore only one of them.
3. As the deployment user, restore the numeric owner and mode:

   ```sh
   sudo chown -R "$(id -u):$(id -g)" data secrets/encryption_key
   sudo chmod 0400 secrets/encryption_key
   ```

4. Start the service and check `/readyz`, `PRAGMA integrity_check`, and that
   encrypted settings/Push keys decrypt (a wrong key fails startup closed before
   migrations or settings seeding).
5. Re-enable scheduling and verify a synthetic/fake-provider check only.

A database restored with a different valid key is not recoverable by retrying:
startup fails because a persisted secret setting cannot be authenticated.
Restore the original key or the matching database+key backup.

A local drill on 2026-10-25 restored a database and key to a fresh directory,
preserved mode 0400, kept the VAPID public key stable, passed
`PRAGMA integrity_check`, and failed closed with a wrong key. See
`docs/IMPLEMENTATION_PLAN.md` (`SEC-003`/`OPS-009`) for exact commands. This is
not a substitute for the production-volume drill.

## 8. Rolling update and rollback

Update from the reviewed commit, or pull a digest-pinned image if the deployment
uses a registry. The current Compose file builds `neda:local`, so the
single-host path uses `build --pull`, not `pull`:

```sh
cd /opt/neda
git fetch --tags
git checkout <reviewed-commit>
docker compose build --pull
docker compose up -d
docker compose ps
curl -fsS https://neda.example.com/readyz
```

Use `http://<public-ip>:<port>/readyz` instead when the deployment is in
direct HTTP mode.

Migrations are forward-only. Before an update, take a database+key backup,
preserve the current image under a rollback tag, and record its ID (Compose tags
the local build `neda:local`, so that tag alone is not a rollback target):

```sh
docker tag neda:local "neda:rollback-$(date -u +%Y%m%dT%H%M%SZ)"
docker image inspect neda:local --format '{{.Id}}'
```

Rollback means:

1. Remove the service with `docker compose down` and record/retain the failing
   version for diagnosis.
2. Restore the previous database+key backup if the new version migrated data.
   The database and key must come from the same backup set.
3. Retag the preserved previous image as `neda:local` (or run the recorded
   image ID directly), then start it without rebuilding:

   ```sh
   docker tag neda:rollback-<timestamp> neda:local
   docker compose up -d --no-build
   ```

4. Verify `/healthz`, `/readyz`, login, scheduling, and decryption of stored
   settings.

Do not run an older binary against a database migrated by a newer schema. Do not
force-push images or overwrite the only backup. A rollback that pairs the old
database with a new key (or the new database with an old key) must be treated as
failed: restore the matching database+key backup before retrying.

## 9. Operations

- Log rotation: Compose json-file driver is capped at 10 MiB × 3; the proxy must
  also rotate its logs.
- Disk: monitor `data/neda.db`, `-wal`, and backups. SQLite needs free space for
  WAL/checkpoint and vacuum; do not fill the volume.
- Resource limits: start at 1 CPU/512 MiB and adjust only from measured RSS/CPU.
- Health: the container healthcheck calls `/readyz`, which verifies SQLite and
  the applied migration count. Use an external HTTPS probe of `/readyz` as well;
  alert on readiness failures, repeated provider-degraded events, and delivery
  failures. `/healthz` remains the process-liveness endpoint, not a database or
  provider check.
- Time: the host clock must be NTP-synced; Tehran display/expiry relies on IANA
  zones embedded in the binary.
- Security: keep Docker and the host patched; prefer an HTTPS reverse proxy.
  Direct HTTP mode is an explicit, unencrypted exception for trusted networks
  or VPN/TLS-tunneled traffic. Review `docs/SECURITY.md` before changing the
  proxy, exposed interface/port, Push allowlist, or secrets.

## 10. Approval checklist

- [ ] Owner supplies host specs, OS, free disk, proxy, domain/TLS, and existing
      port constraints.
- [ ] Owner reviews this plan and `docs/SECURITY.md`.
- [ ] Owner approves `GATE-004` explicitly.
- [ ] Build/digest and rollback image recorded.
- [ ] `scripts/setup.sh` ran as the deployment user; key is a regular file owned
      by the runtime UID with mode 0400, and is backed up with the database.
- [ ] `docker compose config`, an actual `docker compose up` smoke, read-only/non-root
      run, key mount, container recreation, and volume write test pass in CI or on
      the host.
- [ ] `NEDA_MRBILIT_ENABLED=true` is set explicitly (the image does not default
      it); startup log shows the real provider registered, and active monitors
      have no missing-provider coverage errors.
- [ ] For direct HTTP mode, `NEDA_PUBLIC_URL=http://<IP>:<port>` and
      `NEDA_ALLOW_INSECURE_HTTP=true` are set, only `NEDA_PUBLISH_PORT` is
      exposed externally, `NEDA_TRUST_PROXY=false`, and the owner accepts that
      traffic is unencrypted (or has added a VPN/TLS tunnel).
- [ ] Previous image ID, database+key backup, and matching restore procedure are
      recorded before the first update.
- [ ] Backup/restore drill on the target volume passes.
- [ ] Push/Telegram/live-provider actions are separately authorized; none are
      implied by deployment approval.
