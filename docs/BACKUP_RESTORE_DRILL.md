# Local Backup/Restore Drill — Evidence

**Date:** 2026-10-25
**Tasks:** `SEC-003` (key lifecycle/restore), `OPS-009` (backup/restore drill)
**Environment:** macOS arm64 development machine, local Go binary, temporary
directories. **Not** a Docker volume, staging host, or production server.
No real Telegram/Push/provider credentials were used.

## What was exercised

1. Generated a fresh random 32-byte base64 encryption key in a temporary
   `secrets/` directory with mode `0400`.
2. Started the real `neda serve` binary with a fresh SQLite database,
   `NEDA_VAPID_SUBJECT=mailto:drill@example.com`, and the fake provider enabled.
3. Confirmed `/healthz` returned `{"status":"ok"}` and the settings table
   contained a plaintext VAPID public key and an encrypted
   `push.vapid_private_key` (`is_secret=1`).
4. Stopped the service cleanly, checkpointed WAL, and copied the database and
   key to a fresh restore directory with `cp -p`.
5. Confirmed the key mode was preserved (`0400` → `0400`) and the VAPID public
   key was identical in the restored database.
6. Started the restored service on a new port; `/healthz` returned 200 and
   `PRAGMA integrity_check` returned `ok`.
7. Started the restored database with a **different** valid key; startup failed
   closed with `VAPID key initialization failed`, exit status 1.
8. Scanned the original, restored, and wrong-key logs for the key value; no
   match.

## Results

| Check | Result |
|---|---|
| Original health | `{"status":"ok"}` |
| Original DB integrity | `ok` |
| VAPID public key storage | plaintext, `is_secret=0` |
| VAPID private key storage | encrypted, `is_secret=1` |
| Restored key mode | `400` preserved |
| Restored VAPID public key | identical to original |
| Restored health | `{"status":"ok"}` |
| Restored DB integrity | `ok` |
| Wrong key startup | exit 1, failed closed |
| Encryption key in any log | not found |

## Exact command shape

The drill used the built binary
`go build -o <tmp>/neda-drill ./cmd/neda`, then:

```sh
NEDA_ENV=development \
NEDA_HTTP_ADDR=127.0.0.1:<port> \
NEDA_DB_PATH=<drill>/data/neda.db \
NEDA_ENCRYPTION_KEY_FILE=<drill>/secrets/enc.key \
NEDA_VAPID_SUBJECT=mailto:drill@example.com \
NEDA_FAKE_PROVIDER_ENABLED=true \
<tmp>/neda-drill serve
```

After stopping with `SIGTERM`, the database was checkpointed and copied with the
key to a fresh directory. The restored service used the same command with the
restored paths. The wrong-key run used a second independently generated key.

## 2026-10-26 production-mode re-check

A second local drill used the production-mode binary with
`NEDA_MRBILIT_ENABLED=true`, a temporary SQLite database, a temporary
owner-only key, and an empty monitor set. It verified:

- `/readyz` returned 200 after first startup and after restoring the database
  and key to a fresh directory;
- the VAPID public key was identical before and after restore;
- the database contained all five migrations and `PRAGMA integrity_check`
  returned `ok`;
- starting with an independently generated key exited 1 at the pre-migration
  key/database consistency check (`cannot be decrypted with the configured key`);
  the older `VAPID key initialization failed` path remains the fallback for a
  missing or corrupt VAPID pair;
- no provider request was made because the temporary database had no monitors.

The updated Docker workflow is intended to repeat the valid-but-different-key
check against a restored copy of the volume; it has not been run locally because
Docker is unavailable. This remains a local filesystem drill, not a Docker
volume or target-host drill.

## Limitations / not claimed

- A host directory is not a Docker named volume. The Compose/CI tasks
  (`OPS-001`–`OPS-003`, `OPS-009`, `OPS-010`) must repeat this on a real volume
  and verify `/data` ownership/permissions.
- No Telegram credentials or real Push subscriptions were present, so the drill
  proves VAPID private-key decryption and generic encrypted-setting behavior,
  not Telegram/Push end-to-end delivery after restore.
- No production server, staging host, or deployment was accessed.
- The database contains only synthetic/dev settings; no private runtime data is
  recorded here.
