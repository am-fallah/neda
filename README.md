# Neda

**Status: implementation in progress — M1, the approved M2 initial slice, M3 SCH-001, SCH-002, SCH-003, SCH-004, and M4 NTF-000 are complete.** The repository contains the planning documents, service/store/auth foundation, normalized provider boundary with a test-only fake provider, durable monitor CRUD/validation/config-version rules, the due-time scheduler foundation, provider spacing/target-backoff policy with a transaction-scoped persistence helper, an unwired provider-check stage that starts one running check row and performs a single bounded provider call, and the transactional notification-outbox schema and enqueue boundary. The scheduler runs idle in production until SCH-010 composes the check stage into a runner and owns check finalization; target-backoff persistence is also applied by the future outcome transaction (SCH-010). Production provider-check execution, notification delivery workers/senders, the MrBilit adapter, HTTP authentication endpoints, and the dashboard are not implemented yet.

Neda is a planned single-user, self-hosted train-ticket availability monitor for Iranian train routes. It will monitor configured routes, departure dates, passenger counts, and selected providers in the background, then send actionable Telegram and browser Web Push notifications when availability is confirmed. It is not a booking engine: the owner completes any purchase on the provider's site.

## Implemented so far

**M1.1 service foundation**

- Go module pinned to Go `1.27.3`.
- Validated environment configuration with fail-closed production checks.
- Structured logging with request IDs and credential/URL/body redaction.
- SQLite with WAL, foreign keys, `synchronous=FULL`, a single-writer connection, embedded forward-only migrations `0001`–`0003`, checksum verification, and rollback-safe application.
- HTTP server with `GET /healthz`, `GET /readyz`, JSON errors, Host-header validation, body limits, security headers, panic recovery, explicit timeouts, and graceful shutdown.
- `neda healthcheck` for local/container health probes.
- Backend CI for formatting, module tidiness, vet, tests, race tests, and build.

**M1.2 repository and authentication foundation (complete)**

- `internal/store`: transaction helper and repositories for settings, the single owner user, and sessions, with real-SQLite transaction, constraint, and foreign-key tests.
- Argon2id password hashing with encoded parameters; minimum 12 characters and a bounded maximum.
- `neda set-password`: interactive TTY-only setup, reads the password twice without echo, rejects piped input and password arguments, and revokes existing sessions.
- Session and CSRF primitives: 32-byte random tokens stored only as SHA-256 hashes, HttpOnly session and `neda_csrf` cookies, idle/absolute expiry, logout and password-change revocation.
- Origin/Referer validation, double-submit CSRF middleware, and in-memory per-client/global login rate limiting. HTTP auth endpoints and route protection remain for the API milestone.

**M2 initial provider foundation (approved slice complete)**

- `internal/provider`: normalized provider IDs, availability states, capabilities, metadata, request/result types, `Provider` interface, and an explicit immutable registry that rejects nil, duplicate, empty-ID, and empty-display-name entries.
- Shared error taxonomy and `Classify` contract for scheduler/health: success, infrastructure, configuration, route, request, and abandoned categories; timeout/network/429/5xx/invalid-response and provider-side cancellation are infrastructure failures, while 4xx/route/config are not.
- `internal/provider/fake`: deterministic, test-only provider with `TestOnly` metadata, fake-only route-ID prefixes, injected clock, and deterministic available/sold-out/unknown/empty/error scenarios. It performs no network access and is not registered by default.
- Fake-provider UI labeling and the MrBilit adapter remain for later milestones.

**M3 SCH-001 monitor foundation (approved slice complete)**

- `internal/domain`: durable monitor model, validation (label/route/date/passenger/provider rules), Tehran end-of-day expiry, active/paused/expired status, config-version update planning, and a `MonitorService` for create/get/list/edit/pause/resume/delete.
- `internal/store`: real-SQLite monitor and provider-target repository with active/inactive target rows retained for history, atomic material-edit transactions, old-episode closure with `config_changed`, and near-term scheduling after commit.
- Material edits bump affected `config_version`s; label-only and booking-slug-only edits do not reset scheduling. Provider removal deactivates without deleting history; re-adding reactivates and bumps again.
- Expired monitors cannot resume until a future departure-date edit recomputes expiry and clears `expired_at`; paused monitors remain paused.
- A scheduler wake callback is invoked after committed monitor changes; `SCH-002` supplies the wake channel, jitter, and scheduler loop, and production remains idle until `SCH-010` composes the SCH-004 check stage into a runner.

**M3 SCH-002 scheduler foundation (approved slice complete)**

- `internal/scheduler`: due selection for enabled, non-expired monitors with active registered-provider targets; monitor-wide in-flight serialization (A-33); bounded worker pool (`NEDA_WORKERS`, 1–16, default 2); timer-plus-wake loop with no busy polling; ±10% jitter helpers for `SCH-010` and 0–30s near-term jitter; restart stagger; stale `check_runs` recovery; and an explicit `Runner`/`Result` contract for the future `SCH-010` runner.
- `internal/store`: schedule projection, startup recovery/stagger, and the SQLite `poll_interval_seconds` setting seeded to 300s and validated against 60–3600s.
- `MonitorService` near-term scheduling is injectable and deterministic; a material edit's post-commit wake is covered by a scheduler integration test.
- `cmd/neda` starts and gracefully stops the scheduler. With no runner it performs recovery only and stays idle, so it cannot dispatch no-op jobs or busy-loop.
- No provider-check execution, availability/health transitions, notifications, API endpoints, or frontend work is included.

**M3 SCH-003 provider spacing and target backoff (approved slice complete)**

- `internal/scheduler`: per-provider concurrency 1 and 5s start-to-start spacing (provisional), enforced alongside A-33 monitor-wide serialization. Spacing state is in-memory and resets on restart; startup staggering is unchanged.
- `internal/scheduler/backoff.go`: pure target scheduling policy consuming `PRV-010` classifications. First infrastructure failure uses 2× interval; the delay is capped at `max(4×interval, 30m)`, gets ±10% post-cap jitter, and honors classified `RetryAfter` as a lower bound. Config/route/ordinary 4xx use the normal interval; success/valid empty reset the counter; abandoned outcomes do not update scheduling.
- `internal/store`: transaction-scoped `ApplyTargetSchedule` updates only `consecutive_failures`, `next_due_at`, and `updated_at`. It never begins/commits a transaction. Production invocation belongs to SCH-010; no migration is required.
- No provider HTTP retries, redirects, timeouts, availability/health transitions, notifications, API, or frontend work is included.

**M3 SCH-004 provider-check stage (approved slice complete, unwired)**

- `internal/scheduler/check.go`: `CheckStage` loads the persisted monitor/target, builds the normalized `provider.CheckRequest`, commits exactly one `check_runs(outcome='running')` row before I/O, validates provider configuration, and calls `Provider.Check` with one overall 20-second context. A `CheckAttempt` retains the check-run ID, target ID, request config version, booking slug, result/error, PRV-010 classification, and timing for SCH-010.
- `internal/store/checks.go`: `StartCheckRun` performs the attempt-start transaction, verifies the target row still exists, and writes only `check_runs`.
- The stage is deliberately not registered as the production scheduler `Runner`. SCH-010 will compose it, own the post-I/O validity gate, and finalize the running row.
- No target scheduling, availability/health/outbox writes, HTTP retries, live provider requests, or scheduler runner wiring are included.

**NTF-000 transactional notification outbox (approved slice complete)**

- `internal/migrations/0004_notifications.sql`: provider-health tables, notification deliveries/attempts, and Push-subscription storage with enum, counter, foreign-key, and unique constraints plus outbox claim/lookup indexes.
- `internal/notify/outbox.go`: `Enqueue` inserts an event and its initial `pending` deliveries inside the caller's `*sql.Tx`. A duplicate `dedupe_key` returns the existing event ID with `Created=false` and inserts nothing; duplicate delivery targets are no-ops. `FindEventIDByDedupeKey` provides only the minimal dedupe read-back.
- Initial deliveries use `next_attempt_at = event.created_at`. The helper never begins, commits, or rolls back the caller's transaction.
- No delivery worker/retry processing, Telegram or Web Push sender, Push subscription API/encryption, provider-health evaluation, or API read model is included.

## Not implemented yet

Production provider-check execution (the SCH-004 stage exists but is not wired into the scheduler), notification delivery workers and retries, Telegram/Web Push senders, Push subscription API/encryption, provider-health evaluation, availability/health transitions, HTTP authentication endpoints and private-route enforcement, monitor HTTP APIs/UI, the MrBilit adapter, availability episodes/events processing, the dashboard, Docker packaging, and deployment. The scheduler is wired but idle until SCH-010 composes the SCH-004 check stage into a runner; target-backoff persistence is ready for the SCH-010 outcome transaction but is not invoked in production yet. Nothing in this repository authorizes live provider traffic or deployment.

## Local development

Prerequisites: Go `1.27.3` and `make`.

```bash
make check          # formatting, vet, tests, build
make race           # race-enabled tests
```

Create or update the owner password for the local database in an interactive terminal:

```bash
NEDA_ENV=development NEDA_DB_PATH=./data/neda.db go run ./cmd/neda set-password
```

The command rejects non-TTY stdin and does not accept a password argument. The password is read twice without echo.

Run the service locally:

```bash
make run
```

In another terminal:

```bash
curl -i http://127.0.0.1:8080/healthz
curl -i http://127.0.0.1:8080/readyz
NEDA_ENV=development go run ./cmd/neda healthcheck
```

`make run` sets `NEDA_ENV=development` and `NEDA_DB_PATH=./data/neda.db`, then starts the server on `:8080`. The `data/` directory is ignored by Git.

## Configuration

The current service reads environment variables. The validated settings that affect the running skeleton and auth foundation are:

| Variable | Default | Purpose |
|---|---|---|
| `NEDA_ENV` | `production` | `development` or `production`. Use `development` locally. |
| `NEDA_HTTP_ADDR` | `:8080` | HTTP listen address. |
| `NEDA_DB_PATH` | `/data/neda.db` | SQLite database path. |
| `NEDA_ENCRYPTION_KEY_FILE` | — | Required in production. Owner-only (`0400`/`0600`) base64 file containing exactly 32 bytes. |
| `NEDA_PUBLIC_URL` | — | Required in production. HTTPS origin used for Host-header and origin validation. |
| `NEDA_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, or `error`. |
| `NEDA_LOG_FORMAT` | `json` in production, `text` in development | `json` or `text`. |
| `NEDA_SESSION_TTL_HOURS` | `720` | Absolute session lifetime used by the auth foundation. |
| `NEDA_SESSION_IDLE_HOURS` | `168` | Idle timeout used by the auth foundation; must not exceed the absolute TTL. |
| `NEDA_WORKERS` | `2` | Bounded scheduler worker pool; integer from 1 to 16. |
| `NEDA_FAKE_PROVIDER_ENABLED` | `false` | Test-only fake provider; never a real ticket source. |

The global polling interval is not an environment variable. It is seeded into SQLite settings as `poll_interval_seconds` with a default of 300 seconds and enforced bounds of 60–3600 seconds. `.env.example` lists placeholders for settings validated by the loader but reserved for later milestones. The HTTP endpoints that consume the session settings are pending `API-001`/`API-005`. Do not commit real secrets.

## Repository layout

```text
cmd/neda/             CLI: serve, set-password, healthcheck, version
internal/config/      environment loading and validation
internal/logging/     structured logging and redaction
internal/db/          SQLite open/PRAGMAs, migration runner, readiness
internal/migrations/  embedded forward-only SQL migrations
internal/store/       transaction helper and core repositories
internal/domain/      monitor model, validation, expiry, and CRUD service
internal/scheduler/   due-time loop, monitor-wide guard, jitter, recovery, runner boundary
internal/auth/        password, session, CSRF, origin, and rate-limit primitives
internal/provider/    normalized provider boundary, registry, and classification
internal/provider/fake/ deterministic test-only provider
internal/httpapi/     HTTP server, middleware, health/readiness
docs/                 approved planning documents
.github/workflows/    backend CI
```

## Documentation

- [`docs/BRIEF.md`](docs/BRIEF.md) — approved product and engineering brief; the authoritative requirement source.
- [`docs/PRODUCT_SPEC.md`](docs/PRODUCT_SPEC.md) — product behavior, assumptions, and acceptance criteria.
- [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) — architecture, schema, scheduler/state design, security, testing, and deployment design.
- [`docs/IMPLEMENTATION_PLAN.md`](docs/IMPLEMENTATION_PLAN.md) — single source of truth for tasks, gates, dependencies, and verification evidence.
- [`docs/CODEGRAPH_WORKFLOW.md`](docs/CODEGRAPH_WORKFLOW.md) — CodeGraph usage policy.
- [`AGENTS.md`](AGENTS.md) — operating rules for agents working in this repository.

## Safety and security

- No live MrBilit, Telegram, or Web Push calls are made by ordinary tests or CI.
- No production server access or deployment is authorized by this repository state.
- Runtime secrets must be supplied outside the repository.
- The auth foundation stores only session/CSRF hashes; raw tokens live in HttpOnly cookies. HTTP auth endpoints are not implemented yet.
- The fake provider is test-only, disabled by default, and must never be presented as a real ticket source.
- Do not commit passwords, tokens, cookies, private keys, encryption keys, Push subscription material, production environment files, or private runtime data.
