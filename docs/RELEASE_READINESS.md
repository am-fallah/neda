# Neda — Release Readiness and Definition-of-Done Audit

**Date:** 2026-10-25 (updated 2026-10-26)
**Tasks:** `OPS-007` (verification suite), `OPS-008` (DoD audit), plus 2026-10-26 production hardening
**Verdict:** **Local release candidate; NOT production-deployed.** Production
MrBilit registration is now implemented behind the required explicit
`NEDA_MRBILIT_ENABLED=true` setting, with fail-closed startup and missing-provider
coverage checks. The remaining blockers are external validation/authorization
items, not an empty production registry. No live MrBilit request, real Telegram
message/credential, real browser-to-Push-service delivery, production server
access, or deployment was performed.

**2026-10-26 update:** This audit’s older rows are historical. Current code
requires `NEDA_MRBILIT_ENABLED=true` in production, rejects the fake provider in
production, fails startup if the registry is empty or active monitors reference
an unregistered provider, refuses to run as UID 0 in production, and uses
`/readyz` for container health. The image does not enable the real provider by
default; `compose.yaml` requires the setting explicitly. The Docker workflow
builds through Compose and includes an actual Compose start check plus effective
UID/GID, read-only root, dropped capabilities, restart-policy, container
recreation with stable key/database, wrong-valid-key, UID-0/fake-provider
fail-closed, and cleanup checks. That workflow passed CI run
[40365784450](https://github.com/am-fallah/neda/actions/runs/40365784450);
Docker is still unavailable on the local development machine.
`scripts/setup.sh` provides the first-time host layout and key without
overwriting existing files.

Local production-mode verification on 2026-10-26 used a temporary database and
key, no live monitors, and no provider calls. It observed
`provider_registry=["mrbilit"]`, `mrbilit_provider=true`, `fake_provider=false`,
HTTP 200 from `/healthz` and `/readyz`, `neda healthcheck` success, clean
SIGTERM shutdown, five applied migrations, and `PRAGMA integrity_check=ok`.
A temporary database+key restore preserved the VAPID public key; a wrong key
failed closed with exit 1 before migrations/settings seeding. A separate local
database with an active fake provider target confirmed production startup exits
1 on coverage failure before seeding settings. This is binary/filesystem
validation, not Docker or target-host validation.

Also on 2026-10-26: base images were pinned to verified multi-arch manifest
digests; the three tag-to-digest resolutions were re-checked through the
registry APIs and still expose `linux/amd64` and `linux/arm64` entries.
`govulncheck ./...` reported no called vulnerabilities (one module-level
advisory remains for an unimported `openpgp` package), and
`npm audit --audit-level=low` reported 0 vulnerabilities. No image scanner or
local Docker runtime was available, so image scanning remains pending. The
Docker image build and Compose runtime checks passed in CI run
[40365784450](https://github.com/am-fallah/neda/actions/runs/40365784450).
A production-only UID-0 guard is implemented and unit-tested.

## 1. Verified facts

| Fact | Evidence |
|---|---|
| Repository `am-fallah/neda` is private; default branch `main` | `gh repo view`; see tracker |
| Baseline `origin/main` at task start | `f361c309709f9f743d0c5ee04b861d360e7ee755` |
| Final merged `origin/main` for this batch | `ac7a38c` (PR #35 merge); merged-main Go/frontend/Docker CI passed (runs 40137201767/40137201756/40137201807) |
| M1–M5 backend/frontend, private API, and Web Push implemented | merged PRs #33/#34; tracker |
| Retention worker implemented with rollback/FK tests | `internal/retention`; `go test ./internal/retention/...` |
| Go checks pass locally (including race) | `make check`; `make race`; command log in tracker |
| Frontend checks pass locally | `npm ci`, `lint`, `typecheck`, `test` (161 tests), `build` |
| OpenAPI valid | Redocly CLI 1.34.5 |
| Local backup/restore drill passes | `docs/BACKUP_RESTORE_DRILL.md` |
| Real Chrome service worker displays a synthetic Push and permission denial is handled | `docs/BROWSER_PUSH_VALIDATION.md` |
| Dependency scans pass with documented exception | `docs/DEPENDENCIES.md`; 2026-10-26 `govulncheck` reported no called vulnerabilities (one unimported module advisory) and `npm audit` reported 0. No image scanner available. |
| 1-hour synthetic resource measurement completed | `docs/RESOURCE_MEASUREMENT.md`: RSS avg 22.6 MiB, CPU avg 0.12%, WAL max 3.94 MiB, 482 checks |
| Bounded MrBilit live validation completed | `docs/MRBILIT_LIVE_VALIDATION.md`: 2 unauthenticated GETs, HTTP 200, 1 journey/6 classes, 2 Reservable, 4 NotReservable, 0 Unknown, 6 booking URLs |
| Docker image/Compose validation | CI run [40365784450](https://github.com/am-fallah/neda/actions/runs/40365784450) passed the Compose build, actual Compose start, effective UID/GID, read-only root, dropped capabilities, restart-policy, container recreation, key/DB persistence, VAPID stability, wrong-valid-key, UID-0/fake-provider fail-closed, and cleanup checks. Local Docker remains unavailable. |

## 2. Definition-of-done audit

| # | Requirement | Status | Notes |
|---|---|---|---|
| 1 | Code in private GitHub repository `neda` | PASS | Private repository verified; no secrets tracked |
| 2 | Backend Go; frontend React/TS/Vite/Tailwind/shadcn | PASS | Implemented and CI-checked |
| 3 | Persistent SQLite and migrations work safely | PASS | Migrations, FK checks, backup/restore tests |
| 4 | Multiple monitors survive restarts | PASS | Scheduler/restart tests; SQLite authoritative |
| 5 | One job can select multiple providers | PASS (engine) | API/scheduler support; production provider coverage is validated at startup |
| 6 | MrBilit first real adapter; fake proves engine independence | PASS (registration) / NOT VERIFIED (live production) | Adapter live-validated against the observed envelope; URL-first selected-option creation works locally; production registration is explicit and fail-closed via `NEDA_MRBILIT_ENABLED=true`. No live production provider check was run. |
| 7 | Provider-specific reservability; unknown/error never sold out | PASS | Table-driven tests plus live positive/known-negative classes; shortfall remains Unknown |
| 8 | One provider failure does not invalidate another | PASS | SCH-008 multi-provider tests |
| 9 | Durable events/dedupe across restart/retry | PASS | SCH-005/SCH-009 tests |
| 10 | Later availability episode creates a new event | PASS | Availability/state tests |
| 11 | Telegram setup/test/update/remove without leaks | PASS (fake/local) | Real send not authorized/run |
| 12 | Real Web Push via service worker; browser closure | PARTIAL | Server sender/API/SW/UI implemented and locally/fake-tested; **real external Push + closed-browser acceptance not run** |
| 13 | Validated booking links; unverified omitted | PASS | Owner reference verifies `yazd-mashhad` + `1405-08-14`; emitted only for configured validated slugs |
| 14 | Persian/Gregorian conversion and Tehran expiry tested | PASS | `internal/jalali`, frontend `jalaali-js`, expiry tests |
| 15 | Auth/session/CSRF/rate/secret/private-route protection | PASS | Security matrix and reviews |
| 16 | Critical business tests/builds pass | PASS | Local and CI evidence |
| 17 | Deployment and backup workflow documented | PASS (plan) | `docs/DEPLOYMENT.md`; owner review/`GATE-004` pending |
| 18 | No deployment without explicit approval | PASS | No server access/deployment performed |
| 19 | Documentation/limitations accurate | PASS | Tracker, README, SECURITY, DEPENDENCIES, validation docs |

## 3. Release blockers and approvals

| Blocker | Owner/action |
|---|---|
| Real browser-to-Push-service delivery and closed-browser acceptance | Owner authorization + HTTPS deployment + live Push service |
| Real Telegram delivery | Owner credentials + explicit authorization |
| Production provider registration (`PRV-009`) | Implemented locally; live production provider behavior still not exercised |
| Actual Compose runtime and target-volume drill | Compose runtime passed CI run [40365784450](https://github.com/am-fallah/neda/actions/runs/40365784450); the target-host volume drill remains owner-approved |
| Target-host resource/soak measurement | Owner-approved deployment/soak window (local synthetic result is complete) |
| Deployment plan review (`OPS-005`/`GATE-004`) | Explicit owner approval |

## 4. Known limitations

- Production provider registration is implemented but no live production
  provider check has been run; provider availability, rate-limit behavior, and
  booking URLs remain production-observation items.
- The MrBilit adapter is live-validated; production registration is explicit via
  `NEDA_MRBILIT_ENABLED=true`. The booking URL is emitted only when a validated
  slug is configured.
- Push/Telegram delivery is at-least-once; exactly-once is not claimed.
- Login rate limiting is process-local and resets on restart.
- The pre-migration key check authenticates encrypted `settings` rows
  (including VAPID/Telegram); encrypted Push subscription rows are not
  separately checked at startup. A wrong key is still caught while the VAPID
  private key is present.
- Real Push/Telegram/provider/HTTPS behavior is not proven by unit tests or CI.
- Docker/Compose runtime validation passed in CI run
  [40365784450](https://github.com/am-fallah/neda/actions/runs/40365784450),
  including the actual `docker compose up` smoke and the additional UID/GID,
  read-only, capability, restart/recreation, wrong-key, and fail-closed checks.
  The target Ubuntu host, proxy, TLS, firewall, key ownership, and backup volume
  remain unvalidated.
- The local backup drill used a directory, not a Docker named volume; the Docker
  CI job exercises a fresh bind-mounted volume but not the owner's real host.
- Retention defaults are design choices; the local 1-hour DB/WAL result passes,
  but 14-day/180-day growth and target-host behavior remain unproven.

## 5. Next milestone

After the remaining external approvals, the next milestone is **controlled
provider validation and deployment readiness**:

1. Run the reviewed Docker/Compose deployment on an isolated staging/target host
   with `NEDA_MRBILIT_ENABLED=true`; verify real provider registration and
   readiness without bulk provider traffic.
2. Perform authorized real Push/Telegram acceptance and the target-volume
   backup/restore drill.
3. Complete the target-host soak/resource and restore drill.
4. Re-run the complete suite and update this audit with the actual evidence.
