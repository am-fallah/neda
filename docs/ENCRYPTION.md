# Encryption and Key Lifecycle

This document covers the encryption foundation implemented by `GATE-018` P2. It
does not authorize deployment or any production server access. The first-time
setup script may create a random key; the service itself never generates or
rotates a production key silently.

## Key file

`NEDA_ENCRYPTION_KEY_FILE` must point to a regular file:

- owned by the runtime user (checked on Unix);
- mode `0400` or `0600` (group/other access is rejected);
- not a symbolic link;
- containing base64 text that decodes to exactly 32 bytes;
- at most 128 bytes of file content.

The service never generates or rotates a production key automatically. On a
Docker Compose host, `scripts/setup.sh` creates the first key without printing it
and refuses to replace an existing file. To create one manually with a trusted
local tool:

```sh
umask 077
mkdir -p secrets
test ! -e ./secrets/encryption_key
set -C   # noclobber: fail rather than overwrite an existing key
openssl rand -base64 32 > ./secrets/encryption_key
chmod 0400 ./secrets/encryption_key
```

The key file must stay outside the SQLite database and outside Git. Do not paste
a real key into chat, logs, tickets, or documentation. Compose mounts this file
read-only at `/run/secrets/encryption_key`; the host owner must equal the
container runtime UID (`NEDA_RUNTIME_UID`), and the mode must remain `0400` or
`0600`. File-backed Compose secrets do not remap `uid`/`gid`/`mode` in non-swarm
Compose, so the host file is the source of truth.

## Encrypted setting format

Secret settings use AES-256-GCM with a fresh random 96-bit nonce for every
value. Stored values have the form:

```text
v1:base64(nonce || ciphertext || GCM tag)
```

The associated data is derived from the setting key, so a ciphertext copied from
`telegram.bot_token` to `telegram.chat_id` (or vice versa) fails authentication.
The `settings.key_version` column stores the current key version (`1`). A
missing `is_secret` flag, an unexpected key version, a malformed envelope, a
tampered value, or a wrong key fails closed and returns no plaintext.

At startup the configured key is checked against every encrypted settings row
before migrations or settings seeding. A database restored with a different valid
key therefore fails before schema or settings changes, rather than after a
partial startup.

## Telegram settings

`internal/notify/telegramsettings` stores:

- `telegram.bot_token` (encrypted, secret);
- `telegram.chat_id` (encrypted, secret);
- last-test timestamp/result/error category (plaintext, non-secret metadata).

`Status` returns only `configured`, `readable`, a masked chat label, and test
metadata. There is no API or helper that returns the stored token. The bounded
Telegram client lives in `internal/notify/telegram.go`; the delivery worker owns
retry orchestration (`internal/notify/delivery`).

## VAPID and Push subscriptions

`internal/push` stores:

- `push.vapid_public_key` (plaintext; intentionally returned to the
  authenticated dashboard);
- `push.vapid_private_key` (encrypted with AAD `push.vapid_private_key`);
- each subscription endpoint, p256dh, and auth value (encrypted with AAD
  `push.subscription.<id>.endpoint`, `.p256dh`, or `.auth`).

The VAPID pair is generated once at startup when the encryption key is present,
validated to ensure the public key matches the private key, and persisted in one
transaction. A missing public/private counterpart, tampered ciphertext, wrong
master key, or mismatched pair fails closed. The private key is decrypted only
inside the sender for a VAPID header and is never returned by an API or written
to a log. Subscription rows are deduplicated by SHA-256 endpoint hash; the
endpoint and key material are never logged or returned. A lost master key makes
VAPID keys and subscriptions unrecoverable: reset Push, generate a new VAPID
pair, and re-register every browser.

## Backup and restore

A usable backup requires both the SQLite database and the key file:

1. Stop the service so WAL/SHM files are not changing.
2. Copy the database and any `-wal`/`-shm` sidecars.
3. Copy the key file with its owner and `0400`/`0600` mode.
4. Store the backup with access controls equivalent to production.
5. Restore both components together; verify `PRAGMA integrity_check` and that a
   known secret setting can be decrypted.

A database restored without the matching key cannot decrypt Telegram settings,
the VAPID private key, or Push subscription material. In that case, remove and
re-enter the Telegram configuration, reset the Push key material, and re-register
each browser; do not attempt to recover by weakening authentication.

## Rotation and key loss

Key rotation is not implemented in v1. The version prefix and
`settings.key_version` column leave room for a future transactional
decrypt/re-encrypt migration, but no automatic rotation exists.

If the key is lost, encrypted settings are unrecoverable. The operator must
reset the affected configuration through an authenticated settings flow once it
exists. Losing the key does not expose plaintext and does not corrupt the
database, but it makes encrypted values unusable by design.

## Verification

```bash
go test ./internal/crypto/...
go test ./internal/notify/...
go test ./internal/notify/telegramsettings/...
go test ./internal/push/...
go test ./internal/config/...
```

The tests cover key-file permissions/ownership/symlink rejection, AES-GCM round
trips, nonce freshness, tampering, wrong-key and wrong-setting AAD failures,
redacted formatting/JSON, encrypted Telegram persistence, status/test metadata,
fake Telegram API success/error classification, VAPID pair validation/restart
stability/tamper failure, subscription AAD isolation, encrypted-at-rest storage,
and wrong-key failure. No real Telegram/Push credentials or network calls are
used.
