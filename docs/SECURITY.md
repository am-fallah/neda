# Neda — Security Model and Review

**Status:** Implemented controls reviewed on 2026-10-25 for the current private
single-user release candidate. Local automated verification passed. Real
Telegram delivery, real browser-to-Push-service delivery, HTTPS reverse-proxy
behavior, and deployment were **not** exercised and are not claimed.
**Related documents:** `docs/BRIEF.md` §10, `docs/ARCHITECTURE.md` §14,
`docs/ENCRYPTION.md`, `docs/IMPLEMENTATION_PLAN.md` (`SEC-*`), and
`docs/DEPENDENCIES.md`.

## 1. Scope and assets

Neda is a single-owner, self-hosted service. The security boundary is one Go
process, one SQLite database, one external encryption-key file, a private
dashboard/API, and outbound HTTPS to MrBilit, Telegram, and browser Push
services. There is no registration, multi-user role model, billing, arbitrary
webhook, or public admin API.

| Asset | Why it matters |
|---|---|
| Owner password and sessions | Full control of monitors, settings, and secrets |
| Telegram bot token and chat ID | Can send messages and disclose monitoring activity |
| VAPID private key and Push subscriptions | Can deliver notifications to the owner's browsers; endpoint/key material is credential-bearing |
| Encryption key file | Decrypts Telegram credentials, VAPID private key, and Push subscription material |
| SQLite database | Monitoring history, settings, password hash, hashed sessions, ciphertext |
| Availability history and events | Integrity of "reservable" vs "unknown" claims |
| Provider credentials/configuration | Outbound traffic identity and rate-limit budget |

## 2. Trust boundaries

1. **Browser ↔ Go HTTP server**: authenticated session cookie, separate CSRF
   cookie/header, same-origin checks, Host allowlist, no CORS.
2. **Go server ↔ SQLite**: local file, foreign keys on, single-writer
   transactions for state transitions, secret values encrypted before storage.
3. **Go server ↔ external HTTPS**: fixed provider/Telegram hosts; Push endpoint
   allowlist plus send-time DNS/IP validation.
4. **Operator ↔ encryption key**: key is supplied separately from the database,
   owner-only file, never generated or rotated silently.
5. **Service worker ↔ Push service**: the worker receives an opaque payload and
   displays it; it stores no credentials.

## 3. Threat model and controls

| Threat | Control | Residual risk |
|---|---|---|
| Unauthenticated dashboard/API access | Argon2id password hash; opaque 256-bit sessions stored hashed; `HttpOnly`, `SameSite=Lax`, `Secure` in production; idle and absolute expiry; all private routes wrapped by session+CSRF middleware | Single factor; no MFA or account recovery. A compromised owner password grants full control. |
| Session theft / fixation | Random session ID and token; token hashed at rest; session created fresh on login; logout revokes; password change revokes other sessions | A stolen live cookie can be replayed until expiry/revocation. |
| CSRF | Double-submit CSRF cookie plus `X-CSRF-Token` header on every unsafe method; same-origin `Origin`/`Referer` check; no CORS; `SameSite=Lax` | Browser extensions or a compromised same-origin page can act with the owner's authority. |
| Login brute force | Per-IP and global in-memory soft/hard limits with `Retry-After`; password hashing concurrency bound | Limiter resets on restart and is process-local; distributed attacks are not fully represented. |
| Host-header attacks / DNS rebinding | Exact `Host` match against `NEDA_PUBLIC_URL` in production; localhost-only in development; mismatch returns 421 | Reverse proxy must preserve the public Host and terminate TLS correctly. |
| Clickjacking / MIME sniffing / referrer leakage | `X-Frame-Options: DENY`, `frame-ancestors 'none'`, `nosniff`, `no-referrer`, restricted `Permissions-Policy`, `Cross-Origin-Opener-Policy`, `Cross-Origin-Resource-Policy`, no CORS | Browser-specific header behavior is not a substitute for a correct TLS/proxy setup. |
| XSS | React escaping; no `dangerouslySetInnerHTML`; strict SPA CSP (`script-src 'self'`); Telegram HTML escaping; Push payload is JSON, not HTML | Third-party frontend dependencies remain in the trust base. |
| SQL injection | Parameterized queries throughout; no string-built SQL from request values | Repository code must continue to avoid dynamic SQL. |
| Secret leakage through API | Settings responses expose masked chat label and status only; Push config returns the public VAPID key and device metadata, never endpoint/p256dh/auth/private key; write-only credential fields | A future endpoint could accidentally serialize a store row; tests scan responses for secret fields. |
| Secret leakage through logs | `internal/logging` redacts credential-bearing keys, bearer tokens, Telegram bot-token paths, URL userinfo/query/path, CSRF/session values, and Push key names; provider logs record fixed host/path only; delivery logs record sanitized categories; errors from Telegram are category-only | Over-redaction can hide diagnostics; panics are recovered and logged without stacks. |
| Encryption-key loss/theft | Key loaded from a regular non-symlink owner-only file, runtime-UID ownership, 32 bytes, checked at startup; AES-256-GCM with versioned nonce+ciphertext and setting-key AAD; wrong key/tamper fail closed; no silent regeneration | Database + key together are sufficient to decrypt secrets. Key loss makes Telegram/VAPID/subscriptions unrecoverable; see `docs/ENCRYPTION.md`. |
| Push SSRF / DNS rebinding | Subscription-time HTTPS-only allowlist (FCM/Google, Mozilla, Apple, WNS), no userinfo/fragment, default port, bounded length, literal/private/loopback/link-local/multicast/metadata rejection; send-time revalidation, one DNS resolution, fail closed if any answer is unsafe, dial the validated IP directly, recheck the connected IP, no proxy, no redirects, bounded body/time | Push-service hostnames and IP ranges can change; the allowlist and blocked-range table need periodic review. |
| Push endpoint/key leakage | Endpoint, p256dh, and auth encrypted at rest with per-subscription AAD; SHA-256 endpoint hash used only for dedupe; target key is `push:<uuid>`; API/worker never log endpoint or keys | The browser and its Push service inherently hold the endpoint/keys. |
| Push subscription abuse / 410 loops | Per-device rows, dedupe by endpoint hash, bounded list/test, 404/410 disables the subscription, 429/5xx retry with capped `Retry-After`, 400/401/403 permanent failure | A malicious endpoint cannot be reached because of allowlist + IP validation; a compromised Push service is out of scope. |
| Notification spoofing/abuse | Fixed Telegram/Push destinations; authenticated settings; no user-supplied webhook; message labels distinguish availability from provider health; service worker accepts only same-origin or validated mrbilit.com click URLs | The Push service itself is trusted to deliver the payload. |
| False "sold out" / false availability | Conservative availability state machine; errors/timeouts/malformed/missing/stale data never close an episode; provider errors never overwrite confirmed state; transactional events/outbox | Provider semantics remain provisional until `PRV-006` live validation. |
| Delivery loss/duplication | Durable outbox, deterministic delivery IDs, unique targets, leases, bounded retries, at-least-once semantics, independent channels | Exactly-once external delivery is not claimed. |
| Container/host compromise | Multi-stage distroless image/Compose run non-root, read-only root FS, dropped capabilities, no-new-privileges, bounded PIDs/CPU/memory, loopback port, and a `0400` key secret; CI runs it on a fresh volume and after restore | Target-host Docker/host hardening and deployment are not yet verified. |
| Dependency compromise | `go.sum` + `package-lock.json`; `govulncheck` and `npm audit`; maintained libraries; no floating installs in CI | Third-party supply chain remains a residual risk; the openpgp module is present but unused. |
| Internal error disclosure | Sanitized JSON error model; provider/Telegram/Push errors mapped to stable categories; panic recovery returns generic 500; readiness errors are not echoed | New handlers must keep using the shared error helpers. |

## 4. Authentication and session review

- Password setup is offline via `neda set-password`; the web has no
  unauthenticated bootstrap endpoint. Login requires same-origin
  `Origin`/`Referer` and the rate limiter before password verification.
- Passwords are Argon2id with bounded concurrent hashing. Login bounds reject
  oversized password input before hashing.
- Session tokens are random, 32-byte values; only SHA-256 hashes are stored.
  Idle expiry is throttled; absolute expiry is enforced.
- Session and CSRF cookies are `HttpOnly`, `SameSite=Lax`, `Path=/`, and
  `Secure` when production is enabled. Cookies are cleared on logout and invalid
  sessions.
- All private route families (monitors, settings, providers, events,
  deliveries, overview, Push) pass through `RequireSession` and `RequireCSRF`.
  Safe methods still require a session but skip the CSRF token.
- `GET /api/v1/auth/session` is intentionally public for the login page and
  returns only authentication/setup state, CSRF token when authenticated, and
  expiry.
- Health endpoints (`/healthz`, `/readyz`) are intentionally public and expose
  no database path or internal error.

**Review finding fixed in this cycle:** assigning a typed-nil `*push.Service`
to the optional HTTP interface made the server think Push was configured and
returned 503 for `GET /push/config` instead of the intended `available:false`
view. `internal/httpapi` now normalizes typed-nil optional interfaces.

## 5. Push SSRF and subscription review

The sender and subscription validator were reviewed against
`docs/ARCHITECTURE.md` §13.4. Tests cover:

- HTTP, userinfo, fragment, unknown host, suffix-confusion, custom port, IP
  literal, trailing dot, and oversized endpoints.
- Invalid/off-curve P-256 keys, wrong auth-secret length, oversized keys, and
  control characters in labels.
- Private/loopback/link-local/ULA/metadata/documentation IPv4 and IPv6 ranges.
- One DNS resolution with a validated IP dial; mixed public/private answers
  fail closed; the connected remote address is rechecked.
- Redirect rejection, 410 disable, 429/5xx retry classification, 400/403
  permanent failure, and enabled/disabled channel behavior.
- Encrypted-at-rest storage, dedupe, stable IDs across re-registration, wrong-key
  failure, restart persistence, and VAPID tamper failure.

The sender does not log endpoints, keys, payloads, or raw Push-service
responses. The API test suite scans responses and logs for endpoint/key
material.

## 6. Log-redaction audit

Production log call sites were reviewed:

- HTTP request logging records method, path, status, duration, and request ID;
  query strings and headers are omitted.
- Provider logging records the code-owned host and path, status, duration, and
  outcome; query parameters are never logged.
- Telegram errors are mapped to stable categories; the URL containing the bot
  token is never rendered. Tests scan formatted errors and logs.
- Delivery logging records delivery/event IDs, channel, attempt, status, and
  sanitized error category only.
- Push sender logs nothing per send; worker logs use sanitized categories.
- `internal/logging` is defense-in-depth: it removes bearer tokens, `token=`,
  `password=`, `secret=`, `csrf=`, `session=`, `p256dh=`, `auth=`, `chat_id=`,
  `endpoint=` values, Telegram bot-token path segments, URL userinfo, URL path,
  query, and fragment.

**Residual:** log redaction is pattern-based. New log fields must be reviewed
and should prefer IDs/categories over free-form URLs, request bodies, or
provider descriptions.

## 7. Dependency and vulnerability audit

See `docs/DEPENDENCIES.md` for the exact inventory and commands.

- `govulncheck ./...`: **0 called vulnerabilities**. One module-level advisory
  remains for `golang.org/x/crypto/openpgp`, which Neda does not import or call.
- `npm audit --audit-level=low` in `web/`: **0 vulnerabilities**.
- `github.com/golang-jwt/jwt/v5` was upgraded from v5.2.1 to v5.2.2 to remove
  GO-2025-3553 (header-parsing memory allocation) even though Neda only passes
  library-generated claims through `webpush-go`.
- No dependency uses `replace` directives, floating Git refs, or unverified
  install scripts. `package-lock.json` and `go.sum` pin resolved versions.

## 8. Security checklist

Status meanings: **PASS** = automated check or code review completed in this
repository; **PENDING** = requires deployment/manual environment; **N/A** = not
applicable to the current scope.

- [x] Private repository; no secrets, databases, keys, or generated graph data tracked.
- [x] Owner-only password hash; no default credentials; offline setup only.
- [x] Session cookies HttpOnly/SameSite/Secure-in-production; idle/absolute expiry; logout revocation.
- [x] Login same-origin policy and rate limit with `Retry-After`.
- [x] Authenticated CSRF double-submit on every unsafe private route; no CORS.
- [x] Host-header validation returns 421 on mismatch.
- [x] Security headers on API, errors, Host rejection, and SPA.
- [x] Body-size limits and sanitized 413 responses.
- [x] No internal errors or stack traces in API responses.
- [x] Telegram token/chat ID encrypted; never returned; redacted errors.
- [x] VAPID private key encrypted; public key only via authenticated config.
- [x] Push endpoint/keys encrypted; never returned; never logged.
- [x] Push subscription-time URL/key validation and send-time DNS/IP revalidation.
- [x] Push redirects disabled; 404/410 disables; 429/5xx retry bounds.
- [x] Availability errors/unknown never become confirmed unavailability.
- [x] Transactional event/outbox writes and at-least-once delivery semantics.
- [x] Dependency vulnerability scan and lockfile review.
- [x] English-only UI; no secret prefilling.
- [ ] HTTPS/reverse-proxy/HSTS behavior on the real host (`OPS-005`/`GATE-004`).
- [x] Backup/restore drill with key and database on a fresh directory and fresh Docker volume (`OPS-009`/`SEC-003`); target-host drill remains deployment-gated.
- [x] Docker image/Compose build, non-root/read-only runtime, healthcheck, restart, key-mode failure, and fresh-volume restore in CI (run 40133170885).
- [ ] Real Telegram delivery with owner credentials (not authorized/run).
- [ ] Real browser-to-Push-service delivery with a live Push service (not authorized/run).
- [ ] Live MrBilit validation (`PRV-006`/`GATE-003`) (not authorized/run).
- [ ] Deployment and post-deployment security verification (`GATE-004`).

## 9. Unresolved risks

1. **No MFA or recovery flow.** A lost/compromised password is a full compromise
   of the single-owner account; recovery is an offline password reset on the
   host.
2. **Process-local login rate limiting.** A restart clears counters. The
   limiter is not a substitute for a network-level rate limit.
3. **Push allowlist drift.** Browser Push service hostnames and special IP
   ranges can change. Review `internal/push/validate.go` when delivery failures
   are widespread or before a new browser platform is supported.
4. **Provider semantics unverified.** Booking URL format, availability
   semantics, and live rate-limit behavior remain provisional until the
   owner-approved bounded live validation.
5. **Manual delivery acceptance pending.** Automated tests use fake Push and
   Telegram endpoints. Real external delivery, closed-browser behavior, and
   device-specific Push constraints remain manual acceptance items.
6. **Target-host hardening pending.** The image/Compose pass CI runtime and
   restore checks, but the production host's proxy/TLS/firewall, key ownership
   on the real volume, backup target, and deployment remain approval-gated.
