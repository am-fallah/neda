# Neda — Release Readiness and Definition-of-Done Audit

**Date:** 2026-10-25
**Tasks:** `OPS-007` (verification suite), `OPS-008` (DoD audit)
**Verdict:** **Local release candidate; NOT production-deployed.** The remaining
blockers are external validation/authorization items, not unverified local code
paths. No live MrBilit request, real Telegram message/credential, real
browser-to-Push-service delivery, production server access, or deployment was
performed.

## 1. Verified facts

| Fact | Evidence |
|---|---|
| Repository `am-fallah/neda` is private; default branch `main` | `gh repo view`; see tracker |
| Baseline `origin/main` at task start | `f361c309709f9f743d0c5ee04b861d360e7ee755` |
| Final merged `origin/main` for this batch | `ac7a38c` (PR #35 merge); merged-main Go/frontend/Docker CI passed (runs 40137201767/40137201756/40137201807) |
| M1–M5 backend/frontend, private API, and Web Push implemented | merged PRs #33/#34; tracker |
| Retention worker implemented with rollback/FK tests | `internal/retention`; `go test ./internal/retention/...` |
| Go checks pass locally (including race) | `make check`; `make race`; command log in tracker |
| Frontend checks pass locally | `npm ci`, `lint`, `typecheck`, `test` (91 tests), `build` |
| OpenAPI valid | Redocly CLI 1.34.5 |
| Local backup/restore drill passes | `docs/BACKUP_RESTORE_DRILL.md` |
| Real Chrome service worker displays a synthetic Push and permission denial is handled | `docs/BROWSER_PUSH_VALIDATION.md` |
| Dependency scans pass with documented exception | `docs/DEPENDENCIES.md` |
| 1-hour synthetic resource measurement completed | `docs/RESOURCE_MEASUREMENT.md`: RSS avg 22.6 MiB, CPU avg 0.12%, WAL max 3.94 MiB, 482 checks |
| Bounded MrBilit live validation completed | `docs/MRBILIT_LIVE_VALIDATION.md`: 2 unauthenticated GETs, HTTP 200, 1 journey/6 classes, 2 Reservable, 4 NotReservable, 0 Unknown, 6 booking URLs |
| Docker image/Compose runtime tested | Docker CI run `40133170885` passed build, Compose config, non-root/read-only runtime, healthcheck, restart, fresh-volume restore, wrong-key rejection; image 16,331,188 bytes uncompressed / 7,250,978 compressed |

## 2. Definition-of-done audit

| # | Requirement | Status | Notes |
|---|---|---|---|
| 1 | Code in private GitHub repository `neda` | PASS | Private repository verified; no secrets tracked |
| 2 | Backend Go; frontend React/TS/Vite/Tailwind/shadcn | PASS | Implemented and CI-checked |
| 3 | Persistent SQLite and migrations work safely | PASS | Migrations, FK checks, backup/restore tests |
| 4 | Multiple monitors survive restarts | PASS | Scheduler/restart tests; SQLite authoritative |
| 5 | One job can select multiple providers | PASS (engine) | API/scheduler support; production registry still empty |
| 6 | MrBilit first real adapter; fake proves engine independence | PARTIAL | Adapter live-validated against the observed envelope; URL-first selected-option creation works in non-production; **not registered** in production (`PRV-009` remains a separate decision) |
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
| Production provider registration (`PRV-009`) | Scope expansion after validation; keeping the registry empty is current default |
| Docker/Compose CI result and target-volume drill | CI result; owner-approved host for the drill |
| Target-host resource/soak measurement | Owner-approved deployment/soak window (local synthetic result is complete) |
| Deployment plan review (`OPS-005`/`GATE-004`) | Explicit owner approval |

## 4. Known limitations

- The production provider registry is intentionally empty; production checks do
  not dispatch and no real availability events are created.
- The MrBilit adapter is live-validated but deliberately unregistered; production
  checks still do not dispatch. The booking URL is emitted only when a validated
  slug is configured.
- Push/Telegram delivery is at-least-once; exactly-once is not claimed.
- Login rate limiting is process-local and resets on restart.
- Real Push/Telegram/provider/HTTPS behavior is not proven by unit tests or CI.
- Docker/Compose is a design plus CI smoke; the target Ubuntu host, proxy, TLS,
  firewall, key ownership, and backup volume are not validated.
- The local backup drill used a directory, not a Docker named volume; the Docker
  CI job exercises a fresh bind-mounted volume but not the owner's real host.
- Retention defaults are design choices; the local 1-hour DB/WAL result passes,
  but 14-day/180-day growth and target-host behavior remain unproven.

## 5. Next milestone

After the remaining external approvals, the next milestone is **controlled
provider validation and deployment readiness**:

1. Decide whether to register MrBilit in production (`PRV-009`) after reviewing
   the live validation evidence; keep the registry empty until then.
2. Run the reviewed Docker/Compose deployment on the staging/target host behind
   HTTPS; perform real Push/Telegram acceptance and the backup/restore drill.
3. Complete the target-host soak/resource and restore drill.
4. Re-run the complete suite and update this audit with the actual evidence.
