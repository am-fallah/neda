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
| M1–M5 backend/frontend, private API, and Web Push implemented | merged PRs #33/#34; tracker |
| Retention worker implemented with rollback/FK tests | `internal/retention`; `go test ./internal/retention/...` |
| Go checks pass locally (including race) | `make check`; `make race`; command log in tracker |
| Frontend checks pass locally | `npm ci`, `lint`, `typecheck`, `test` (90 tests), `build` |
| OpenAPI valid | Redocly CLI 1.34.5 |
| Local backup/restore drill passes | `docs/BACKUP_RESTORE_DRILL.md` |
| Real Chrome service worker displays a synthetic Push and permission denial is handled | `docs/BROWSER_PUSH_VALIDATION.md` |
| Dependency scans pass with documented exception | `docs/DEPENDENCIES.md` |
| Docker image/Compose runtime tested | Docker CI run (record final run ID/result here after completion) |

## 2. Definition-of-done audit

| # | Requirement | Status | Notes |
|---|---|---|---|
| 1 | Code in private GitHub repository `neda` | PASS | Private repository verified; no secrets tracked |
| 2 | Backend Go; frontend React/TS/Vite/Tailwind/shadcn | PASS | Implemented and CI-checked |
| 3 | Persistent SQLite and migrations work safely | PASS | Migrations, FK checks, backup/restore tests |
| 4 | Multiple monitors survive restarts | PASS | Scheduler/restart tests; SQLite authoritative |
| 5 | One job can select multiple providers | PASS (engine) | API/scheduler support; production registry still empty |
| 6 | MrBilit first real adapter; fake proves engine independence | PARTIAL | Adapter implemented and unit-tested; **not registered**; live semantics unverified (`PRV-006`) |
| 7 | Provider-specific reservability; unknown/error never sold out | PASS (code/tests) | Table-driven tests; live positive/negative evidence still needed |
| 8 | One provider failure does not invalidate another | PASS | SCH-008 multi-provider tests |
| 9 | Durable events/dedupe across restart/retry | PASS | SCH-005/SCH-009 tests |
| 10 | Later availability episode creates a new event | PASS | Availability/state tests |
| 11 | Telegram setup/test/update/remove without leaks | PASS (fake/local) | Real send not authorized/run |
| 12 | Real Web Push via service worker; browser closure | PARTIAL | Server sender/API/SW/UI implemented and locally/fake-tested; **real external Push + closed-browser acceptance not run** |
| 13 | Validated booking links; unverified omitted | PASS (conservative) | Adapter currently emits none; owner example blocked (`PRV-007`) |
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
| Live MrBilit validation (`PRV-006`/`GATE-003`) | Owner-approved target route/date and bounded protocol |
| Booking URL format (`PRV-007`) | Owner supplies a real example or approves live validation |
| Production provider registration (`PRV-009`) | Scope expansion after validation |
| Docker/Compose CI result and target-volume drill | CI result; owner-approved host for the drill |
| Resource measurement final result | 1-hour synthetic run completion (in progress at this audit) |
| Deployment plan review (`OPS-005`/`GATE-004`) | Explicit owner approval |

## 4. Known limitations

- The production provider registry is intentionally empty; production checks do
  not dispatch and no real availability events are created.
- The MrBilit adapter's booking URL support is disabled until the URL/date shape
  is verified; no misleading link is emitted.
- Push/Telegram delivery is at-least-once; exactly-once is not claimed.
- Login rate limiting is process-local and resets on restart.
- Real Push/Telegram/provider/HTTPS behavior is not proven by unit tests or CI.
- Docker/Compose is a design plus CI smoke; the target Ubuntu host, proxy, TLS,
  firewall, key ownership, and backup volume are not validated.
- The local backup drill used a directory, not a Docker named volume; the Docker
  CI job exercises a fresh bind-mounted volume but not the owner's real host.
- Retention defaults are design choices; the DB size target is provisional until
  measured on the target host.

## 5. Next milestone

After the remaining external approvals, the next milestone is **controlled
provider validation and deployment readiness**:

1. Owner approves `GATE-003` with one future route/date and the bounded protocol;
   record evidence and either register MrBilit (`PRV-009`) or keep it disabled.
2. Owner supplies a verified booking URL example; finish `PRV-007`.
3. Run the reviewed Docker/Compose deployment on the staging/target host behind
   HTTPS; perform real Push/Telegram acceptance and the backup/restore drill.
4. Re-run the complete suite and update this audit with the actual evidence.
