# Neda

**Status: M1–M4 backend foundations and the M5 private API + dashboard are implemented and tested. Web Push delivery, production provider registration/live validation, retention cleanup, Docker packaging, and deployment remain pending.** The repository contains the planning documents, service/store/auth foundation, normalized provider boundary with a test-only fake provider, durable monitor CRUD/validation, the scheduler/state engine, the transactional outbox and Telegram delivery worker, the private JSON API, and a React/TypeScript dashboard embedded in the Go binary. The production provider registry remains intentionally empty, so production checks do not dispatch and no new availability events are created. Nothing here authorizes live provider traffic, real Telegram credentials, or deployment.


Neda is a single-user, self-hosted train-ticket availability monitor for Iranian train routes. It monitors configured routes, departure dates, passenger counts, and selected providers in the background, then sends actionable Telegram and browser Web Push notifications when availability is confirmed. It is not a booking engine: the owner completes any purchase on the provider's site.

## Implemented so far

**M1.1 service foundation**

- Go module pinned to Go `1.27.3`.
- Validated environment configuration with fail-closed production checks.
- Structured logging with request IDs and credential/URL/body redaction.
- SQLite with WAL, foreign keys, `synchronous=FULL`, a single-writer connection, embedded forward-only migrations (`0001`–`0003`, with NTF-000 adding `0004`), checksum verification, and rollback-safe application.
- HTTP server with `GET /healthz`, `GET /readyz`, JSON errors, Host-header validation, body limits, security headers, panic recovery, explicit timeouts, and graceful shutdown.
- `neda healthcheck` for local/container health probes.
- Backend CI for formatting, module tidiness, vet, tests, race tests, and build.

**M1.2 repository and authentication foundation (complete)**

- `internal/store`: transaction helper and repositories for settings, the single owner user, and sessions, with real-SQLite transaction, constraint, and foreign-key tests.
- Argon2id password hashing with encoded parameters; minimum 12 characters and a bounded maximum.
- `neda set-password`: interactive TTY-only setup, reads the password twice without echo, rejects piped input and password arguments, and revokes existing sessions.
- Session and CSRF primitives: 32-byte random tokens stored only as SHA-256 hashes, HttpOnly session and `neda_csrf` cookies, idle/absolute expiry, logout and password-change revocation.
- Origin/Referer validation, double-submit CSRF middleware, and in-memory per-client/global login rate limiting. HTTP auth endpoints and private-route protection are implemented by P4/API-001/API-005.

**M2 initial provider foundation (approved slice complete)**

- `internal/provider`: normalized provider IDs, availability states, capabilities, metadata, request/result types, `Provider` interface, and an explicit immutable registry that rejects nil, duplicate, empty-ID, and empty-display-name entries.
- Shared error taxonomy and `Classify` contract for scheduler/health: success, infrastructure, configuration, route, request, and abandoned categories; timeout/network/429/5xx/invalid-response and provider-side cancellation are infrastructure failures, while 4xx/route/config are not.
- `internal/provider/fake`: deterministic, test-only provider with `TestOnly` metadata, fake-only route-ID prefixes, injected clock, and deterministic available/sold-out/unknown/empty/error scenarios. It performs no network access and is not registered by default.
- Fake-provider metadata is surfaced as a test-only source in the dashboard; the MrBilit adapter remains unregistered pending its validation gate.

**M3 SCH-001 monitor foundation (approved slice complete)**

- `internal/domain`: durable monitor model, validation (label/route/date/passenger/provider rules), Tehran end-of-day expiry, active/paused/expired status, config-version update planning, and a `MonitorService` for create/get/list/edit/pause/resume/delete.
- `internal/store`: real-SQLite monitor and provider-target repository with active/inactive target rows retained for history, atomic material-edit transactions, old-episode closure with `config_changed`, and near-term scheduling after commit.
- Material edits bump affected `config_version`s; label-only and booking-slug-only edits do not reset scheduling. Provider removal deactivates without deleting history; re-adding reactivates and bumps again.
- Expired monitors cannot resume until a future departure-date edit recomputes expiry and clears `expired_at`; paused monitors remain paused.
- A scheduler wake callback is invoked after committed monitor changes; `SCH-002` supplies the wake channel, jitter, and scheduler loop, and `SCH-010` composes the check stage and outcome runner. Production dispatch remains inert while the provider registry is empty.

**M3 SCH-002 scheduler foundation (approved slice complete)**

- `internal/scheduler`: due selection for enabled, non-expired monitors with active registered-provider targets; monitor-wide in-flight serialization (A-33); bounded worker pool (`NEDA_WORKERS`, 1–16, default 2); timer-plus-wake loop with no busy polling; ±10% jitter helpers used by `SCH-010` and 0–30s near-term jitter; restart stagger; stale `check_runs` recovery; and the `Runner`/`Result` contract implemented by the SCH-010 outcome runner.
- `internal/store`: schedule projection, startup recovery/stagger, and the SQLite `poll_interval_seconds` setting seeded to 300s and validated against 60–3600s.
- `MonitorService` near-term scheduling is injectable and deterministic; a material edit's post-commit wake is covered by a scheduler integration test.
- `cmd/neda` starts and gracefully stops the scheduler. It now composes the SCH-010 outcome runner; with the intentionally empty production provider registry it performs recovery/staggering but dispatches no provider checks.
- No provider-check execution, availability/health transitions, notifications, API endpoints, or frontend work is included.

**M3 SCH-003 provider spacing and target backoff (approved slice complete)**

- `internal/scheduler`: per-provider concurrency 1 and 5s start-to-start spacing (provisional), enforced alongside A-33 monitor-wide serialization. Spacing state is in-memory and resets on restart; startup staggering is unchanged.
- `internal/scheduler/backoff.go`: pure target scheduling policy consuming `PRV-010` classifications. First infrastructure failure uses 2× interval; the delay is capped at `max(4×interval, 30m)`, gets ±10% post-cap jitter, and honors classified `RetryAfter` as a lower bound. Config/route/ordinary 4xx use the normal interval; success/valid empty reset the counter; abandoned outcomes do not update scheduling.
- `internal/store`: transaction-scoped `ApplyTargetSchedule` updates only `consecutive_failures`, `next_due_at`, and `updated_at`. It never begins/commits a transaction. Production invocation belongs to SCH-010; no migration is required.
- No provider HTTP retries, redirects, timeouts, availability/health transitions, notifications, API, or frontend work is included.

**M3 SCH-004 provider-check stage (approved slice complete; consumed by SCH-010)**

- `internal/scheduler/check.go`: `CheckStage` loads the persisted monitor/target, builds the normalized `provider.CheckRequest`, commits exactly one `check_runs(outcome='running')` row before I/O, validates provider configuration, and calls `Provider.Check` with one overall 20-second context. A `CheckAttempt` retains the check-run ID, target ID, request config version, booking slug, result/error, PRV-010 classification, and timing for SCH-010.
- `internal/store/checks.go`: `StartCheckRun` performs the attempt-start transaction, verifies the target row still exists, and writes only `check_runs`.
- The production scheduler now composes this stage through the SCH-010 outcome runner; the stage itself still performs no outcome writes.
- No target scheduling, availability/health/outbox writes, HTTP retries, or live provider requests are included in the stage.

**NTF-000 transactional notification outbox (approved slice complete)**

- `internal/migrations/0004_notifications.sql`: provider-health tables, notification deliveries/attempts, and Push-subscription storage with enum, counter, foreign-key, and unique constraints plus outbox claim/lookup indexes.
- `internal/notify/outbox.go`: `Enqueue` inserts an event and its initial `pending` deliveries inside the caller's `*sql.Tx`. A duplicate `dedupe_key` returns the existing event ID with `Created=false` and inserts nothing; duplicate delivery targets are no-ops. Delivery IDs are outbox-owned deterministic UUIDs derived from the event ID and destination identity, so multiple events in one transaction cannot collide on the global primary key. `FindEventIDByDedupeKey` provides only the minimal dedupe read-back.
- Initial deliveries use `next_attempt_at = event.created_at`. The helper never begins, commits, or rolls back the caller's transaction.
- No delivery worker/retry processing, Telegram or Web Push sender, Push subscription API/encryption, provider-health evaluation, or API read model is included.

**M3 SCH-005 availability state machine (approved slice complete, applier only)**

- `internal/availability/apply.go`: `Apply(ctx, *sql.Tx, input)` applies one successful provider result to `monitor_provider_id × config_version × journey_key × offering_key` states. It covers initial Reservable, NotReservable→Reservable, continuous Reservable, decisive close, Unknown preservation, later re-open, missing/empty observations, and conflicting duplicate keys.
- The applier never begins, commits, or rolls back. It writes availability states/episodes and enqueues availability events plus initial deliveries through NTF-000 inside the caller's transaction.
- Missing and empty successful results set `last_missing_at` without closing episodes. Provider errors/timeouts/malformed results are never mapped to NotReservable; Unknown and conflict results remain Unknown.
- This slice did not include provider health, check-run finalization, target scheduling/backoff, worker/sender, or production runner wiring; SCH-010 now supplies finalization and runner composition.

**M3 SCH-006 provider health (approved slice complete, applier only)**

- `internal/health/apply.go`: `ApplySuccess` and `ApplyFailure` are caller-transaction-scoped and never begin, commit, or roll back. They upsert `provider_health`, manage the single open degraded episode, emit one deduplicated degraded/recovery event through NTF-000, and update provider/target diagnostics.
- A-21 is resolved to **Option A (infrastructure-only)**: only applicable infrastructure failures with the provider classification's outage flag increment the provider counter. Config/route/ordinary 4xx and business/availability outcomes update diagnostics but do not count as provider failures; success/empty resets the counter.
- Three eligible infrastructure failures open one degraded episode and one event; further failures do not repeat it. Recovery closes the episode and emits one recovery event.
- `internal/health/status.go` derives stale status as `max(3×poll interval, 15m)` or an open degraded episode.
- Target `consecutive_failures`/`next_due_at`/`updated_at` remain owned by SCH-003; this slice did not include availability state, check-run finalization, workers/senders, or production runner wiring. SCH-010 supplies check finalization and runner composition.

**M3 SCH-007 expiry reconciliation and next-expiry wake (approved slice complete)**

- `internal/availability/expiry.go`: `CloseExpired` closes every open availability episode for a monitor with `close_reason='expired'`, inside the caller's transaction, without creating an event or delivery.
- `internal/store/expiry.go`: `ReconcileExpirations` applies one transaction-scoped sweep over all monitors whose parsed `expires_at` is at or before `now`, including paused and disabled monitors. It sets `expired_at`, preserves `expires_at`, delegates episode closure to SCH-005, and returns the earliest remaining future expiry. It does not poll or write target scheduling, check-run, provider-health, event, or delivery state.
- `internal/scheduler/scheduler.go`: the scheduler reconciles expiry during startup and every loop iteration and includes the next expiry in its timer deadline. Expiry is reconciled even while every worker is busy. Date-edit or pause/resume wakes recalculate the deadline, and old timers are stopped by the existing wait loop.
- Expiry remains a non-event diagnostic transition: historical availability events remain, open episodes close, and paused monitors stay paused. An expired monitor cannot be resumed until a future departure-date edit recomputes `expires_at` and clears `expired_at`.
- No schema migration, live provider request, notification sender, API, or deployment work is included.

**M3 SCH-010 per-check outcome orchestrator (approved slice complete)**

- `internal/scheduler/outcome.go`: `OutcomeRunner` composes the SCH-004 check stage with one caller-owned outcome transaction. It applies the mandatory pre-I/O-return and in-transaction target validity gates (target existence/active state, monitor enabled, expiry, config version, booking slug) before any outcome write.
- Success/valid-empty outcomes finalize the check row, apply availability transitions, apply provider-health success, persist target scheduling through SCH-003, and enqueue events/deliveries through NTF-000 atomically. Provider-error outcomes finalize the row, update SCH-006 diagnostics/counters, and persist target backoff without touching availability. Discarded and abandoned outcomes finalize only the running `check_runs` row.
- Structural result validation runs before the outcome transaction. Invalid results are classified as `invalid_response` and take the provider-error branch. Caller cancellation is abandoned; deadline/timeout is a provider failure; target invalidity takes precedence over cancellation.
- `internal/store`: `WithSQLTx` provides the raw transaction plus a transaction-scoped repository view; `FinalizeCheckRun` uses a conditional `WHERE id=? AND outcome='running'` update and reports `ErrNoRunningCheck` for duplicate/lost finalization.
- An injected delivery source supplies destinations; production defaults to none until notification-channel tasks define resolution. The outbox wake is a no-op until a delivery worker exists. Outcome-transaction failure leaves the running row for startup recovery and returns `Settled:true` so the scheduler does not start a duplicate attempt immediately; the target is not rechecked until the process restarts or persisted scheduling changes, which is a documented limitation of this slice.
- No live provider requests, notification senders, channel configuration, multi-provider end-to-end matrix, API, or deployment work is included.

**M3 SCH-008 multi-provider isolation (test-only complete)**

- `internal/scheduler/multi_provider_test.go` adds a nine-test `TestMultiProvider*` matrix over real check-stage/outcome-runner and scheduler components. It covers one provider failing while another succeeds, independent availability states/episodes/events even when providers report identical journey/offering keys, no cross-provider merge or suppression, independent event/outbox counts, and a discarded stale result that does not block the other provider.
- Provider health is verified as global per provider ID, not per monitor: a beta success does not recover degraded alpha, while a later alpha success on another monitor does recover alpha. Per-target backoff is isolated, and the A-33 monitor-wide guard is verified with real sequential provider execution (maximum concurrency one).
- Expiry tests use the merged SCH-007 reconciliation path to close every open episode for every provider of a monitor with `close_reason='expired'`, without creating events or outbox rows and without dispatching a post-expiry poll.
- Evidence: `go test ./internal/scheduler/... -run TestMultiProvider -count=1` executed all nine tests and passed; ten repeated iterations also passed. No production behavior was changed.

**M3 SCH-009 scheduler/state end-to-end integration (test-only complete)**

- `internal/scheduler/integration_*_test.go` adds a reusable temp-SQLite harness that composes the real scheduler, check stage, outcome runner, availability/health appliers, NTF-000 outbox, and a reopenable database. It covers all 15 SCH-009 acceptance criteria: success/valid-empty commit and health recovery, rollback both before and at the final schedule write, infrastructure and non-counting errors, threshold health commit/rollback, discarded/abandoned/deleted-target write sets, cancellation/timeout semantics, delivery/outbox ordering, stale in-flight material edits, attempt-start and outcome crash recovery, slow-retry deadline, booking-slug changes, expiry/date-edit/resume, and the interleaved health transition table.
- Restart requirements close and reopen SQLite rather than relying on in-memory state. The stale in-flight test proves the SCH-010 validity gate discards the old-config result while the material edit's near-term schedule and provider health remain intact. The valid-empty test asserts both check finalization and the persisted provider-health success/recovery record.
- AC 8 limitation: NTF-001 is not implemented, so there is no production delivery worker. The test-only after-commit observer proves that the delivery source runs while the outcome is uncommitted and that the outbox is visible only at the post-commit wake. It does not claim that an external sender was exercised.
- Evidence: `go test ./internal/... -count=1`, `go test -count=1 ./...`, `go test -race -count=1 ./...`, `go vet ./...`, `go build ./...`, `gofmt -l .`, `git diff --check`, `make check`, and `make race` passed; selected integration tests passed repeated and race-enabled runs, and targeted runs under `TZ=UTC` and `TZ=America/New_York` passed. No production sender, schema change, live provider, server, or deployment work was added.
- PR: [#13](https://github.com/am-fallah/neda/pull/13); code-head CI [40103924704](https://github.com/am-fallah/neda/actions/runs/40103924704) and evidence-head CI [40104074601](https://github.com/am-fallah/neda/actions/runs/40104074601) passed. Merged to `main` as `25d46b9` on 2026-10-24.

**P1a MrBilit adapter foundation (unregistered)**

- `internal/provider/mrbilit` implements the documented/provisional `GET /api/GetAvailable/v2` request with `from`, `to`, `date`, `adultCount`, `childCount`, and `infantCount`. It never sends unverified `genderCode`, `exclusive`, or `availableStatus` values and never sends cookies or credentials.
- The transport owns only the 6s per-attempt timeout, two bounded retries (timeouts, connection errors, 429, 502/503/504), 500ms/1s backoff with jitter, `Retry-After` bounded by the caller's deadline, redirect rejection, cancellation, and a 2 MiB response cap. It does not create an overall check deadline; `SCH-004` remains the sole owner of the 20s context.
- The parser accepts only an explicitly labeled synthetic response envelope (`testdata/mrbilit/synthetic`) because the real envelope, offering-container key, time/price formats, and capacity semantics are unverified. Other shapes fail closed as invalid responses. Caps are 100 journeys, 50 offerings per journey, and 500 offerings total.
- Journey/offering keys follow Architecture §9.3: native IDs use `id:<native>`; fallback keys hash a versioned canonical string and exclude price, capacity, availability flags, and array index. The known-negative sample (`capacity=0`, `isAvailable=false`, `reservationAvailable=true`) is `NotReservable`; positive/shortfall/contradiction cases use synthetic fixtures only.
- The adapter is deliberately not registered in `cmd/neda/serve.go`; production checks remain inert. Real-contract validation requires the owner-approved `PRV-006`/`GATE-003` protocol.
- PR: [#14](https://github.com/am-fallah/neda/pull/14); merged to `main` as `33f8a7c`, reverted by [#16](https://github.com/am-fallah/neda/pull/16) (`1fdba66`), and re-applied by [#17](https://github.com/am-fallah/neda/pull/17) (`bb514c3`). The code is present in `main`; code-head CI [40110863710](https://github.com/am-fallah/neda/actions/runs/40110863710) (`a477150`) and final code-head CI [40111365080](https://github.com/am-fallah/neda/actions/runs/40111365080) (`10ba711`) passed.

**P2 encryption and Telegram foundation**

- `internal/crypto` loads and validates the external key file, encrypts setting values with versioned AES-256-GCM and setting-key-bound associated data, and fails closed on wrong keys/tampering. `internal/config` now uses the same key-file validation at startup.
- `internal/notify` provides a bounded, redirect-free Telegram Bot API client with HTML escaping, safe permanent/transient/rate-limit/canceled classification, `Retry-After`, and redacted errors. `internal/notify/telegramsettings` stores the token and chat ID encrypted and returns only a masked status/test view.
- Delivery orchestration is implemented by `NTF-001`; the authenticated Telegram settings/test API is implemented by `API-004`. The manual backup/restore drill and `/data` permission proof remain `SEC-003`/`GATE-004`. No real credentials, live Telegram calls, or production key generation are included.
- PR: [#15](https://github.com/am-fallah/neda/pull/15); merged to `main` as `759cf77`. Rebased-head CI [40115109700](https://github.com/am-fallah/neda/actions/runs/40115109700) and post-merge `main` CI [40115242788](https://github.com/am-fallah/neda/actions/runs/40115242788) passed.

**P3 notification delivery worker (Telegram)**

- `internal/notify/delivery` claims committed outbox rows with a lease, sends outside any database transaction, records every attempt, applies bounded retry/backoff policy, honors safe `Retry-After`, supports crash reclaim of expired `sending` leases, and enforces a bounded worker concurrency.
- The worker skips availability deliveries whose monitor is already expired and skips an unconfigured or unreadable channel instead of retrying forever. Event creation/outbox atomicity remains owned by NTF-000/SCH-005/SCH-006; the worker never creates events.
- `internal/notify/format.go` renders escaped Telegram HTML with validated booking links and distinct provider-health/recovery labels. `internal/notify/delivery.TelegramSender` decrypts configured credentials and uses the bounded P2 client. Tests use fake Telegram servers only; no real credentials or messages are used.
- Production composition starts the worker only when the encryption key file is configured. With the provider registry still empty, no production checks dispatch, so the worker has no new rows to send.
- Remaining: NTF-002 full presentation (notably Jalali display); the authenticated Telegram settings/test endpoint is implemented by `API-004`.
- PR: [#19](https://github.com/am-fallah/neda/pull/19); merged to `main` as `bddcf24`. Code CI [40115991305](https://github.com/am-fallah/neda/actions/runs/40115991305), docs CI [40116125663](https://github.com/am-fallah/neda/actions/runs/40116125663), and post-merge main CI [40116243015](https://github.com/am-fallah/neda/actions/runs/40116243015) passed.

**P4 private authentication API**

- `POST /api/v1/auth/login` uses same-origin `Origin`/`Referer` validation and the in-memory login rate limiter, then issues a fresh session and CSRF cookie. Login does not use session CSRF.
- `GET /api/v1/auth/session` returns authenticated state, CSRF token, setup-required signal, and expiry without secrets. `POST /api/v1/auth/logout` and `POST /api/v1/auth/password` require a session plus CSRF header/cookie and same-origin validation; password change revokes other sessions.
- The API preserves the existing Host validation, body limits, security headers, no-CORS policy, request IDs, sanitized errors, and no-store caching. `cmd/neda/serve.go` composes the auth manager and login guard.
- Tests cover login success/failure/setup-required, Origin rejection, rate limiting with `Retry-After`, cookie flags, session probes, private-route 401/403 behavior, logout, and password change. No real credentials are used.
- PR: [#20](https://github.com/am-fallah/neda/pull/20); merged to `main` as `7cd0261`. Code CI [40116569664](https://github.com/am-fallah/neda/actions/runs/40116569664), docs CI [40116746410](https://github.com/am-fallah/neda/actions/runs/40116746410), and post-merge main CI [40116899292](https://github.com/am-fallah/neda/actions/runs/40116899292) passed.

**API-002 monitor CRUD API**

- `internal/httpapi/monitors.go` adds private `GET/POST /api/v1/monitors`, `GET/PATCH/DELETE /api/v1/monitors/{id}`, and idempotent `POST /api/v1/monitors/{id}/pause|resume` endpoints.
- Requests use camelCase monitor/provider DTOs. `PATCH` is partial: omitted fields/providers are preserved; supplying a provider list replaces the active set and inherits omitted optional fields from matching providers. Responses expose monitor status, expiry, scheduling/diagnostics, and provider rows without internal secrets. Validation failures return a `validation_error` with path-keyed fields; expired resume returns 409; delete returns 204.
- `cmd/neda/serve.go` composes the monitor service with the scheduler waker and current-interval near-term scheduling. Automated tests use the deterministic fake provider and temp SQLite; no live provider is involved. Development may explicitly register the fake provider with `NEDA_FAKE_PROVIDER_ENABLED=true`; production keeps the provider registry empty until P1b/`PRV-009`, so production monitor create/edit rejects provider selections and no checks dispatch.
- Remaining: none for monitor CRUD/detail; overview/providers/events/deliveries are implemented by `API-003`.
- PR: [#22](https://github.com/am-fallah/neda/pull/22); merged to `main` as `f33dd8a`. CI run [40119615680](https://github.com/am-fallah/neda/actions/runs/40119615680) passed on the code head; merged-main CI [40119753601](https://github.com/am-fallah/neda/actions/runs/40119753601) passed.

**NTF-006 notification/event/delivery read model**

- `internal/store/notification_read.go` adds read-only `ListEvents`, `GetEvent`, and `ListDeliveries`. Events include durable snapshots, nested per-channel delivery state, and derived pending/sending/sent/retry/failed/skipped and attempted/terminal counts. Delivery history includes event kind, occurrence time, provider, and monitor context.
- Pagination is opaque cursor-based with deterministic chronological ordering and stable ID tie-breaks; page sizes default to 50 and are capped at 100. Filters cover event kind/monitor/provider and delivery channel/status/event/kind. Delivery target keys, leases, credentials, raw error details, and HTTP endpoints are excluded.
- This task is repository-only; `API-003` owns the HTTP events/deliveries/overview surface. No schema, outbox, worker, or delivery-semantics changes were made.
- Tests cover empty pages, subsecond and tie ordering, page walking, filters, all six delivery statuses, attempted/terminal counts, malformed cursors, cancellation, and monitor-deletion cascade.
- PR: [#24](https://github.com/am-fallah/neda/pull/24); CI run [40121564627](https://github.com/am-fallah/neda/actions/runs/40121564627).

**API-008 monitor detail observations and diagnostics**

- `GET /api/v1/monitors/{id}` now keeps the existing monitor/provider fields and adds bounded `observations` and `recentChecks` for each active provider target. List/create/update/pause/resume payloads are unchanged.
- Observations are read only for the target's current `configVersion`, ordered deterministically by state and stable keys, capped at 100 with `observationsTruncated`. They expose the persisted `reservable`/`not_reservable`/`unknown` state and timestamps. `missing=true` means the latest successful check did not return that offering; current availability is then unknown, not confirmed unavailable or available.
- Recent checks are newest-first, capped at 10 with `recentChecksTruncated`, and expose sanitized outcome, error category, HTTP status, duration, and counts. Provider errors, timeouts, malformed responses, and discarded/abandoned checks never overwrite the last confirmed availability state.
- Inactive historical targets remain in the provider list but expose no stale diagnostics. The detail DTO excludes target keys, leases, credentials, endpoints, and raw provider errors.
- Tests cover normal, empty, missing, unauthenticated, paused, expired, stale-timestamp, unknown/missing, empty-success-vs-provider-error, provider-error, config-version isolation, and unchanged list responses.
- PR: [#25](https://github.com/am-fallah/neda/pull/25); CI run [40122307378](https://github.com/am-fallah/neda/actions/runs/40122307378).

**API-004 settings and Telegram configuration**

- `GET /api/v1/settings` returns non-secret settings: `pollIntervalSeconds`, `pushEnabled`, and nested Telegram `available`/`configured`/`readable`, masked `chatLabel`, settings error category, and last-test metadata.
- `PUT /api/v1/settings` atomically updates the poll interval (60–3600 seconds) and/or Push-enabled flag. The new interval applies to subsequent scheduling.
- `PUT /api/v1/settings/telegram` saves the token and chat ID through the existing AES-256-GCM settings layer, or clears them with `clear:true`. Responses never include the token, raw chat ID, or ciphertext.
- `POST /api/v1/settings/telegram/test` validates the bot with `getMe`, sends one test message, and records only a sanitized result category/timestamp. Telegram failures are shown through `lastTestOk=false`/`lastErrorKind`; missing encryption settings return 503 and missing/unreadable stored credentials return 409.
- Uses the private session+CSRF wrapper and existing Host validation, body limits, and sanitized errors. Tests cover auth/CSRF, defaults, partial updates, interval bounds, encrypted storage, set/clear, success/permanent-failure tests, unreadable/missing/unavailable configuration, response secret scans, and log scans. No real Telegram credentials or messages are used.
- PR: [#26](https://github.com/am-fallah/neda/pull/26); CI run [40123110108](https://github.com/am-fallah/neda/actions/runs/40123110108).

**API-003 providers, events, deliveries, and overview**

- `GET /api/v1/providers` returns registered provider metadata/capabilities, the test-only flag, and global per-provider health with optional open degraded episode and derived stale state. Missing health is `null`/unknown, never fabricated as healthy.
- `GET /api/v1/events` and `/events/{id}` expose durable events with nested per-channel delivery state and summary counts. Filters: `kind`, `monitorId`, `providerId`, `limit`, opaque `cursor`. Availability events carry `state: "reservable"`; provider-health events carry only sanitized error/failure diagnostics.
- `GET /api/v1/deliveries` exposes delivery history with `channel`, `status`, `eventId`, `kind`, `limit`, and opaque `cursor` filters. Target keys and leases are never returned.
- `GET /api/v1/overview` returns bounded aggregates: derived monitor counts, registered-provider degraded/stale counts, delivery status counts, five recent availability events, five recent deliveries, and `generatedAt`.
- All routes are private GETs; invalid filters/cursors return sanitized 400s and missing events 404. No write, retry, or send endpoint is added, so notification ownership remains with the delivery worker/tasks.
- Tests cover auth, provider metadata/health/stale/degraded, event/delivery ordering and pagination, valid/invalid filters, detail/missing, empty results, overview counts/recent items, and internal-field absence.
- PR: [#27](https://github.com/am-fallah/neda/pull/27); CI run [40123917780](https://github.com/am-fallah/neda/actions/runs/40123917780).

**API-007 end-to-end private API coverage**

- `TestAPIEndToEndLifecycle` composes the real in-process HTTP server, temp SQLite, auth/session/CSRF manager, settings service, fake provider registry, fake Telegram tester, and NTF-006 read model.
- The lifecycle covers unauthenticated 401s, login, settings read/update, Telegram configure/test, monitor create/list/detail, observation/check diagnostics, event/delivery reads, overview aggregates, pause/resume, delete cascade, settings persistence, logout, post-logout 401, and log secret scans.
- No live provider, real Telegram credentials, or Push endpoint tests are used.
- PR: [#28](https://github.com/am-fallah/neda/pull/28); CI run [40124471803](https://github.com/am-fallah/neda/actions/runs/40124471803).

**API-006 OpenAPI documentation**

- `docs/openapi.yaml` is a hand-written OpenAPI 3.0.3 description of the implemented health, auth/session, monitor, provider/health, event/delivery, overview, and settings/Telegram routes.
- It documents session/CSRF cookie/header security, cursor/filter parameters, common error/status responses, and response schemas; write-only Telegram secrets are never described as returned.
- Validated with `npx --yes @redocly/cli@1.34.5 lint docs/openapi.yaml` (valid, no warnings). No code generation or runtime changes.
- PR: [#29](https://github.com/am-fallah/neda/pull/29); CI run [40125174668](https://github.com/am-fallah/neda/actions/runs/40125174668); merged as `4ed7a52`; merged-main CI [40125322348](https://github.com/am-fallah/neda/actions/runs/40125322348).

**M5 dashboard and frontend (WEB-001–WEB-009, FND-013)**

- `web/` is a React 19 + TypeScript + Vite + Tailwind CSS 4 + shadcn/ui single-page app. Routes: `/login`, `/` (overview), `/monitors`, `/monitors/new`, `/monitors/:id`, `/monitors/:id/edit`, `/events`, `/events/:id`, and `/settings`. A protected layout redirects unauthenticated visitors to `/login`; any private API 401 with `unauthenticated` also clears the client session and redirects.
- The typed API client uses same-origin credentials and an in-memory CSRF token from the login/session response. The token is never written to localStorage or sessionStorage. Server field errors are mapped into forms, while server validation remains authoritative.
- The dashboard shows monitor counts, provider health (degraded/stale), recent events, and per-channel delivery status. The UI never treats a provider error, timeout, malformed response, missing offering, or stale observation as confirmed unavailability. "Not reservable" is never labeled "sold out"; `Unknown` and missing observations are shown as unknown with a reason category. Historical events and observations are explicitly labeled as historical, not current availability.
- Monitor create/edit uses a Jalali date picker backed by the maintained `jalaali-js` library and stores/sends Gregorian `YYYY-MM-DD`. Validation is mirrored on the client for usability; the server remains authoritative. Create/edit supports per-provider route IDs, optional booking slugs/gender codes only when the provider advertises support, and the test-only provider is clearly labeled and never presented as a real ticket source.
- Detail pages show last observed inventory, recent checks, per-provider diagnostics, pause/resume, edit, and confirmed delete. Observation rows expose validated booking links only; missing/unknown offerings never imply "sold out". History supports event/delivery filters and cursor pagination.
- Settings read/update the polling interval, configure/clear encrypted Telegram credentials without prefilling secrets, run an explicit Telegram test, and change the password. Telegram status exposes only a masked chat label and sanitized test result. Browser Push setup is intentionally not exposed because the server-side Push implementation (`PSH-*`) is still pending.
- The frontend is embedded with `web/embed.go` and served by `internal/webui`: one Go binary serves the SPA and API, deep links fall back to `index.html`, missing hashed assets return 404, API/health paths never fall back to HTML, HTML is `no-store`, hashed assets are immutable, and the SPA CSP allows same-origin scripts plus inline style attributes required by Radix while forbidding inline scripts.
- `web/dist/.gitkeep` keeps `go build ./...` valid before a frontend build. The checked-in `.gitkeep` is restored after builds; generated assets are ignored.
- `npm run lint`, `npm run typecheck`, `npm test`, and `npm run build` pass. Vitest/Testing Library covers date conversion/leap/boundaries, API/CSRF/401 behavior, login and setup guidance, monitor status semantics, pause/resume/delete confirmation, settings secret handling, polling validation, password change, history filters/pagination, and delivery labels. The manual browser checklist is in [`docs/FRONTEND_ACCEPTANCE.md`](docs/FRONTEND_ACCEPTANCE.md); items requiring a real browser/Telegram/Push are recorded as not run where applicable.

## Not implemented yet

Web Push (VAPID keys, subscription API/encryption, service worker, sender), NTF-002 full notification presentation (including Jalali display), provider-health notification channel resolution, production MrBilit registration and live validation, retention cleanup, Docker packaging, and deployment. The MrBilit adapter foundation remains unregistered; the P2/P3 Telegram delivery foundation is wired but has no production credentials and no provider checks dispatch with the empty registry. Nothing in this repository authorizes live provider traffic, real Telegram credentials, or deployment.

## Local development

Prerequisites: Go `1.27.3`, Node.js `22.12+` with npm, and `make`.

Backend checks:

```bash
make check          # formatting, vet, tests, build
make race           # race-enabled tests
```

Create or update the owner password for the local database in an interactive terminal:

```bash
NEDA_ENV=development NEDA_DB_PATH=./data/neda.db go run ./cmd/neda set-password
```

The command rejects non-TTY stdin and does not accept a password argument. The password is read twice without echo.

Run the backend:

```bash
NEDA_FAKE_PROVIDER_ENABLED=true make run
```

`make run` sets `NEDA_ENV=development` and `NEDA_DB_PATH=./data/neda.db`. The fake provider is test-only and disabled by default; enable it only for local workflows. In another terminal:

```bash
curl -i http://127.0.0.1:8080/healthz
curl -i http://127.0.0.1:8080/readyz
NEDA_ENV=development go run ./cmd/neda healthcheck
```

Run the dashboard development server in a separate terminal:

```bash
cd web
npm ci
npm run dev
```

Vite serves `http://localhost:5173` and proxies `/api`, `/healthz`, and `/readyz` to `http://127.0.0.1:8080`. Open the Vite URL, sign in with the password set above, and enable the fake provider on the backend only if you want local test data. The browser never sees the test provider as a real ticket source; it is labeled in the UI.

Production-style single-binary build:

```bash
cd web
npm ci
npm run build        # writes web/dist and restores web/dist/.gitkeep
cd ..
go build -o neda ./cmd/neda
NEDA_ENV=development NEDA_DB_PATH=./data/neda.db ./neda serve
```

The frontend must be built before the Go binary because assets are compiled in with `go:embed`. `go build ./...` remains valid without a frontend build (the SPA returns a clear 503 while API/health routes still work).

Frontend checks:

```bash
cd web
npm run lint
npm run typecheck
npm test
npm run build
```

The dashboard test suite uses Vitest and Testing Library with mocked `fetch`; it does not require a running backend, live provider, Telegram, or Push credentials. See [`docs/FRONTEND_ACCEPTANCE.md`](docs/FRONTEND_ACCEPTANCE.md) for the manual browser checklist.

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

The global polling interval is not an environment variable. It is seeded into SQLite settings as `poll_interval_seconds` with a default of 300 seconds and enforced bounds of 60–3600 seconds. `.env.example` lists placeholders for settings validated by the loader but reserved for later milestones. Private auth, monitor, provider/health, event/delivery, overview, and settings/Telegram routes are implemented. Browser Push endpoints are not implemented. Do not commit real secrets.

## Repository layout

```text
cmd/neda/             CLI: serve, set-password, healthcheck, version
internal/config/      environment loading and validation
internal/logging/     structured logging and redaction
internal/db/          SQLite open/PRAGMAs, migration runner, readiness
internal/migrations/  embedded forward-only SQL migrations
internal/store/       transaction helper and core repositories
internal/domain/      monitor model, validation, expiry, and CRUD service
internal/scheduler/   due-time loop, monitor-wide guard, jitter, recovery, expiry wake, runner boundary
internal/auth/        password, session, CSRF, origin, and rate-limit primitives
internal/provider/    normalized provider boundary, registry, and classification
internal/provider/fake/ deterministic test-only provider
internal/httpapi/     HTTP server, middleware, health/readiness, private API
internal/webui/       embedded SPA handler, SPA fallback, static caching
web/                  React/TypeScript dashboard and `go:embed` assets
web/src/              routes, pages, components, typed API client, tests
web/embed.go          embeds web/dist into the Go binary
docs/                 approved planning documents and frontend acceptance checklist
.github/workflows/    backend and frontend CI
```

## Documentation

- [`docs/BRIEF.md`](docs/BRIEF.md) — approved product and engineering brief; the authoritative requirement source.
- [`docs/PRODUCT_SPEC.md`](docs/PRODUCT_SPEC.md) — product behavior, assumptions, and acceptance criteria.
- [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) — architecture, schema, scheduler/state design, security, testing, and deployment design.
- [`docs/IMPLEMENTATION_PLAN.md`](docs/IMPLEMENTATION_PLAN.md) — single source of truth for tasks, gates, dependencies, and verification evidence.
- [`docs/FRONTEND_ACCEPTANCE.md`](docs/FRONTEND_ACCEPTANCE.md) — manual browser/accessibility acceptance checklist for the dashboard.
- [`docs/CODEGRAPH_WORKFLOW.md`](docs/CODEGRAPH_WORKFLOW.md) — CodeGraph usage policy.
- [`AGENTS.md`](AGENTS.md) — operating rules for agents working in this repository.

## Safety and security

- No live MrBilit, Telegram, or Web Push calls are made by ordinary tests or CI.
- No production server access or deployment is authorized by this repository state.
- Runtime secrets must be supplied outside the repository.
- The auth layer stores only session/CSRF hashes; raw tokens live in HttpOnly cookies. The dashboard keeps its CSRF token in memory only and does not store credentials, tokens, or Telegram secrets in browser storage.
- The dashboard never displays Telegram tokens, raw chat IDs, ciphertext, Push subscription material, or raw provider errors.
- The fake provider is test-only, disabled by default, and is labeled as a test source in the UI.
- Do not commit passwords, tokens, cookies, private keys, encryption keys, Push subscription material, production environment files, or private runtime data.
