# Encryption and Key Lifecycle

This document covers the encryption foundation implemented by `GATE-018` P2. It
does not authorize deployment, key generation automation, or any production
server access.

## Key file

`NEDA_ENCRYPTION_KEY_FILE` must point to a regular file:

- owned by the runtime user (checked on Unix);
- mode `0400` or `0600` (group/other access is rejected);
- not a symbolic link;
- containing base64 text that decodes to exactly 32 bytes;
- at most 128 bytes of file content.

The service never generates or rotates a production key automatically. An
operator may create one manually with a trusted local tool, for example:

```sh
umask 077
openssl rand -base64 32 > ./secrets/neda_encryption_key
chmod 0400 ./secrets/neda_encryption_key
```

The key file must stay outside the SQLite database and outside Git. Do not paste
a real key into chat, logs, tickets, or documentation.

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

## Telegram settings

`internal/notify/telegramsettings` stores:

- `telegram.bot_token` (encrypted, secret);
- `telegram.chat_id` (encrypted, secret);
- last-test timestamp/result/error category (plaintext, non-secret metadata).

`Status` returns only `configured`, `readable`, a masked chat label, and test
metadata. There is no API or helper that returns the stored token. The bounded
Telegram client lives in `internal/notify/telegram.go` and does not implement
delivery retries or orchestration; those remain P3/`NTF-001`.

## Backup and restore

A usable backup requires both the SQLite database and the key file:

1. Stop the service so WAL/SHM files are not changing.
2. Copy the database and any `-wal`/`-shm` sidecars.
3. Copy the key file with its owner and `0400`/`0600` mode.
4. Store the backup with access controls equivalent to production.
5. Restore both components together; verify `PRAGMA integrity_check` and that a
   known secret setting can be decrypted.

A database restored without the matching key cannot decrypt Telegram settings
or future Push key material. In that case, remove and re-enter the Telegram
configuration; do not attempt to recover by weakening authentication.

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
go test ./internal/config/...
```

The tests cover key-file permissions/ownership/symlink rejection, AES-GCM round
trips, nonce freshness, tampering, wrong-key and wrong-setting AAD failures,
redacted formatting/JSON, encrypted Telegram persistence, status/test metadata,
and fake Telegram API success/error classification. No real Telegram credentials
or network calls are used.
