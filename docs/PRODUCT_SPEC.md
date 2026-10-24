# Neda — Product Specification

**Status:** Planning baseline approved under `GATE-001` (2026-10-23). Implementation proceeds under `GATE-002` only within the scope recorded in `IMPLEMENTATION_PLAN.md`.
**Date:** 2026-10-23
**Authoritative source:** `docs/BRIEF.md` (read completely). If this document and the brief conflict, the brief wins; the conflict must be reported, not silently resolved.
**Repository path:** `docs/PRODUCT_SPEC.md`

**Classification legend**

- **VERIFIED** — directly confirmed by inspection, command output, or the approved brief.
- **PROVISIONAL** — a safe planning default that must be confirmed during implementation or by the owner.
- **REQUIRES OWNER INPUT** — cannot be safely decided from available evidence.

---

## 1. Product summary

Neda is a single-user, self-hosted train-ticket availability monitor for Iranian train routes. The owner defines independent monitors (route, departure date, passenger counts, one or more providers). A backend scheduler polls selected providers without needing a browser, classifies each inventory offering conservatively, records durable availability episodes and events, and sends actionable notifications through Telegram and real browser Web Push.

The product is not a booking engine. It detects and reports availability and, when a booking URL has been validated, links the owner to the provider's booking page. The owner completes any purchase on the provider's site.

### 1.1 V1 goals, in priority order

1. Correct availability detection. A provider error, timeout, malformed response, or unknown state must never be presented as "sold out"; a feature flag alone must never be presented as "available".
2. Reliable background monitoring and notifications, including restarts, browser closure, and independent channel failures.
3. Clean, extensible multi-provider architecture from day one.
4. Security and credential protection.
5. Low CPU, memory, disk, and network usage.
6. Maintainable code and meaningful deterministic tests.
7. A polished but minimal dashboard.

### 1.2 V1 non-goals

- Multi-user accounts, registration, billing, roles, or SaaS operation.
- A curated city catalogue or city search.
- Custom filters by train number, departure-time window, wagon/class, or price.
- Automatic ticket purchase, seat selection, payment, or credential harvesting.
- Cross-provider inventory aggregation, canonical journey merging, or suppressed duplicate notifications across providers.
- Price history, seat maps, analytics dashboards, or mobile native apps.
- Kubernetes, Redis, a separate worker container, an external database, or a message broker.
- Next.js or server-side rendering.
- A full browser-automation E2E suite for v1; a manual acceptance checklist is required.
- Production deployment without a separately reviewed and approved deployment plan.

---

## 2. Users and core workflows

### 2.1 User

A single owner/operator. The owner is technical enough to self-host a Docker Compose service, configure a Telegram bot, and manage backups. There is no public signup or shared tenancy.

### 2.2 Primary user stories

| ID | Story | Acceptance summary |
|---|---|---|
| US-01 | As the owner, I can create a monitor for an origin, destination, Gregorian departure date, passenger counts, and one or more providers. | Monitor persists; validation rejects identical origin/destination, missing provider IDs, invalid date/counts, and zero providers. |
| US-02 | As the owner, I can edit a monitor's label, route/date, passenger counts, provider mappings, and provider selection. | Material changes reset only affected provider observation state without deleting historical events; next check uses the new configuration. |
| US-03 | As the owner, I can pause and resume a monitor. | Pause stops future polling and is reversible. Provider failures never pause or disable a monitor. There is no terminal `stopped` state. |
| US-04 | As the owner, I can delete a monitor after confirmation. | Monitor and its dependent runtime state/history are removed according to the documented deletion policy. |
| US-05 | As the owner, I can see per-monitor status: last check, next check, last success, current observations, provider health, and errors. | UI clearly distinguishes "no reservable tickets found", "check failed/unknown", "paused", and "expired". |
| US-06 | As the owner, I receive a Telegram message and/or Web Push when a validated reservable offering is detected. | Event is durable; channels are independent; delivery failures are recorded and retried within bounds. |
| US-07 | As the owner, I can follow a validated booking URL from a notification or event detail. | URL appears only when a validated provider-specific pattern/slug exists; otherwise it is absent with an explanation. |
| US-08 | As the owner, I can configure, test, update, and remove Telegram settings without exposing the token. | API never returns the full token; token is encrypted at rest; test result is visible; logs are redacted. |
| US-09 | As the owner, I can enable/disable browser Push, register a device, send a test, and click a notification to open the relevant page. | Real server-side Web Push via service worker; works after the browser is closed; invalid subscriptions are cleaned up. |
| US-10 | As the owner, I can set a strong password and log in securely. | Password set only through an offline/interactive CLI; no default credentials; sessions, CSRF, and rate limits are enforced. |
| US-11 | As the owner, I can see notification history and distinguish detection from delivery. | Event time, provider, journey/offering, per-channel status, and booking link are visible. |
| US-12 | As the owner, I can monitor multiple providers in one monitor without one failure hiding another provider's success. | Per-provider checks, observations, events, and deliveries are independent. |

---

## 3. Functional requirements

### 3.1 Monitors

The owner can create, view, edit, pause, resume, and delete multiple independent monitors.

Each monitor stores:

- Stable ID and user label.
- Canonical origin and destination display names.
- Gregorian departure date, stored as a calendar date (`YYYY-MM-DD`), never an ambiguous timestamp.
- Adult, child, and infant counts.
- One or more selected providers.
- Per-provider origin/destination IDs, optional validated booking slug, and optional provider-specific gender code where genuinely required.
- Enabled/paused state, expiry state, timestamps, last attempt, last success, next due time, and diagnostics.

Validation rules:

- Label: required, trimmed, bounded length (proposal: 1–100 characters).
- Origin and destination names: required; not identical after trimming/case-folding.
- Departure date: valid Gregorian date, not before "today" in `Asia/Tehran` at creation/edit time; stored canonically.
- Passenger counts: adults at least 1; children and infants at least 0; bounded per category (proposal: 0–9 each, total at most 9) pending provider validation. The owner may change these bounds only with explicit approval.
- At least one provider selected; no duplicate provider selections.
- Every selected provider must receive the route inputs its adapter requires. Numeric MrBilit city IDs must be positive integers. The fake/test provider deliberately requires a different route-ID format.
- Identical origin/destination rejected by both display name and provider-specific IDs.
- Optional booking slug is accepted only if it passes the validated provider pattern; otherwise the monitor can still be saved without a booking link, with a visible limitation.

Provider mappings are stored per provider. A city ID is never assumed to be globally meaningful.

### 3.2 Scheduling and background monitoring

- Default global polling interval: **5 minutes (300 seconds)**.
- Hard bounds: **60 seconds minimum**, **3600 seconds maximum**.
- The interval is configurable globally in Settings. There is no per-monitor interval in v1.
- Monitoring runs in the backend independently of the browser.
- Executions are serialized **per monitor**: at most one provider check for a given monitor may be in flight at any time. Different monitors may execute concurrently, subject to the global worker bound. This is the owner decision recorded in A-33 on 2026-10-24, choosing the stricter reading of the brief’s “Never overlap executions of the same monitor”; it is stricter than, and subsumes, per-provider concurrency 1.
- Global worker concurrency is bounded. Proposal: 2 workers by default, configurable within a small range; per-provider concurrency 1; per-provider minimum spacing (owner-approved provisional: 5 seconds, start-to-start, one scheduler-level value). The spacing state is in-memory and resets on restart; startup staggering is unchanged. The minimum spacing is provisional and has not been validated against live provider terms; it must not be presented as a provider-published limit.
- Scheduling jitter is applied so monitors do not align into thundering-herd bursts.
- Transient failures use bounded exponential backoff; provider failures never permanently disable a monitor.
- Jobs are not scheduled when no eligible jobs exist. The scheduler does not spawn one unbounded goroutine per future job.
- A monitor is not polled while paused, deleted, or expired.
- A job expires after the end of its departure day in `Asia/Tehran`, regardless of the server's local timezone.
- Restart recovery: active jobs, next-due state, last attempts, and open episodes survive a process restart. Each attempt is recorded as `running` before provider I/O; an interrupted or crashed attempt is finalized as `abandoned` at startup (check-runs diagnostics only) or via the abandoned path and is not treated as a provider verdict or as availability evidence.
- One provider's failure must not block or invalidate another provider's successful check.
- Pause/resume is idempotent. Resuming an expired monitor is rejected until the departure date is changed.
- Provider errors never change a monitor to a terminal state.

### 3.3 Availability semantics

Normalized inventory states are exactly:

- **Reservable** — sufficient validated positive evidence that the specific offering can be booked for the requested passenger configuration, with no unresolved contradictory evidence.
- **NotReservable** — sufficient validated decisive negative evidence for that offering.
- **Unknown** — missing, null, unexpected-type, insufficient, contradictory, malformed, errored, timed-out, or otherwise unproven evidence.

Hard rules:

- A provider error/timeout/malformed response is never `NotReservable`.
- `reservationAvailable = true` alone is never sufficient evidence of a sellable ticket. The known negative sample (`capacity = 0`, `isAvailable = false`, `reservationAvailable = true`) must classify as `NotReservable`.
- `isAvailable = true`, a feature flag, advertised capacity, booking capability, or HTTP 200 alone is never sufficient evidence of sellable inventory.
- Missing observations, empty lists, unknown states, failures, and discarded/abandoned checks never close an open availability episode.
- Only a successful, decisive `NotReservable` observation closes an open episode.
- The first successful observation of a new monitor/provider/offering that is `Reservable` creates exactly one initial event and notifies. This avoids silently missing currently bookable tickets.
- Repeated `Reservable` observations in one continuous episode create no additional events.
- After a confirmed close, a later `Reservable` observation opens a new episode and creates a new event.
- Cross-provider states are independent. No inventory quantities are added or merged across providers.
- If identity is ambiguous, observations remain separate. Route + date alone is never sufficient to merge journeys.

The detailed MrBilit decision table is in `ARCHITECTURE.md`, section 9. It is intentionally conservative until live/native semantics are validated.

### 3.4 Passenger capacity semantics

Capacity and passenger-count semantics are **not yet verified**. The request sends adult/child/infant counts, but whether `capacity` means remaining seats, total seats, per-class inventory, or something else is unconfirmed, and whether infants consume inventory is unconfirmed.

Safe default:

- Do not claim `Reservable` unless a numeric capacity is present and is at least the conservative required-seat count.
- Conservative required-seat count initially includes adults + children + infants. This may produce false negatives if infants do not consume seats; false positives are the greater risk.
- A positive capacity below the requested count is `Unknown`, not `NotReservable`, until capacity semantics are validated. It must not close an episode based on an unverified interpretation.
- The only decisive negative rule initially is the documented known sample: `capacity == 0` and `isAvailable == false` (regardless of `reservationAvailable`).
- After bounded validation and/or owner confirmation, refine the rule and document the evidence. Never infer semantics from a single ambiguous sample.

**Classification:** `REQUIRES OWNER INPUT` and live/native validation. The owner should confirm how the provider's UI treats children/infants and whether capacity is remaining bookable seats.

### 3.5 Providers

- **MrBilit** is the first real adapter.
- A deterministic **fake/test provider** proves the engine is provider-agnostic. It requires a different route-ID format and uses a different availability rule. It must never be presented as a real ticket source.
- A monitor can select multiple providers. Each provider has independent configuration, observations, health, events, and deliveries.
- Adding a future real provider should require implementing/registering an adapter, not rewriting scheduler, state-transition, or notification logic.
- No generic plugin framework, reflection registration, dynamic code loading, or speculative abstraction layers.
- Provider-specific request/response parsing, reservability interpretation, and booking-URL logic stay inside the adapter. The core engine consumes normalized outcomes only.

### 3.6 Notifications

Two channels in v1: Telegram and browser Web Push. Each runs independently.

Common requirements:

- Durable delivery queue created in the same transaction as the durable event; delivery state is persisted separately from event creation.
- A delivery failure must never roll back or corrupt inventory state.
- Retries only for transient failures, with bounded attempts and backoff. Permanent failures are recorded and surfaced.
- Stable event IDs, unique delivery records, and idempotency constraints prevent duplicate events. External delivery is at-least-once; Neda must not claim exactly-once delivery.
- Include, where available: origin → destination, departure date/time, arrival time, train number/operator, wagon/class, price, provider, detection timestamp, and a validated booking URL.
- Do not include secrets, raw provider payloads, subscription endpoints, or credential-bearing URLs.
- Provider-health alerts are distinct from ticket-availability notifications and must be visibly labeled.
- A separate event and delivery are created per provider-specific availability event. One provider's event is never suppressed because another provider's state differs.
- If a monitor expires before an availability delivery is sent, the stale ticket notification is skipped (the historical event remains visible). Provider-health deliveries are not tied to monitor expiry.

#### Telegram

- Owner supplies an existing bot token and chat ID through the dashboard after implementation.
- Save/update/remove, test connection, send test message, and view connection status.
- The API never returns the full stored token.
- Credentials are encrypted at rest with a persistent key supplied outside the database; they are never committed, bundled, logged, or returned.
- Bounded retries for transient failures; clear errors for invalid token/chat.

#### Browser Web Push

- Real server-side Web Push (VAPID or a maintained Go implementation), not only in-page notifications.
- HTTPS in production; localhost exception for development.
- Browser permission flow, service worker, subscription registration/persistence, unsubscribe, invalid-subscription cleanup, enable/disable control, and a test action.
- Clicking an availability notification opens its validated booking URL when available; otherwise opens the event detail page.
- VAPID key material persists across restarts and is never logged.
- Monitoring continues with the browser closed.
- Subscription endpoints are treated as sensitive; validation prevents SSRF and arbitrary endpoint abuse.

**Implementation status (2026-10-25).** The VAPID keypair (public plaintext,
private encrypted), encrypted subscription storage, subscription-time and
send-time SSRF validation, service worker, authenticated config/subscription/
test routes, dashboard enable/pause/device/test/remove flows, and independent
Push delivery are implemented. Automated tests cover the validation, encryption,
dedupe, 410 disable, retry, DNS/IP, redirect, API, and UI paths. Real
browser-to-Push-service delivery, closed-browser acceptance, and iOS/PWA
behavior remain manual items and were not run; a live deployment also needs the
owner's HTTPS origin and `NEDA_VAPID_SUBJECT`.

### 3.7 Dashboard and API

Dashboard requirements:

- English-only, responsive, accessible, light and dark themes, using shadcn/ui.
- Overview: active monitors, recent availability events, delivery status, last successful checks, provider health, actionable errors.
- Monitor list/detail: label, route/date, providers, passenger counts, enabled/paused/expired state, last status/check, next check, errors, current observations.
- Clear distinction between "no reservable tickets found" and "provider check failed/unknown".
- Create/edit: monitor label, origin/destination names, manually entered per-provider city IDs, Jalali date picker backed by canonical Gregorian storage, passenger counts, gender-related input only where genuinely required, one-or-more providers. No invented city catalogue.
- Notifications view: event details, provider, journey/ticket, event time, booking link, per-channel delivery status.
- Settings: Telegram setup/test/status, browser Push setup/test/status, read-only provider metadata/health, global polling interval, security options. Per-monitor provider IDs/slugs are edited in monitor forms; global provider base URLs and provider metadata are code-owned, not user-editable. Never redisplay full secrets.
- A monitor may be paused/resumed and deleted with confirmation.

API requirements:

- Versioned REST API (`/api/v1`) with explicit request/response models, consistent JSON errors, appropriate status codes, and input validation.
- Frontend-independent API models.
- All private routes require authentication. Only intentionally public health endpoints are exempt.
- Routes for auth/session, monitor management (including monitor detail, current observations, and check diagnostics), provider status/health, availability events, notification history/delivery, settings, and Push subscriptions.
- OpenAPI documentation if practical without excessive complexity; not a v1 blocker.

### 3.8 Authentication and security

- Private, password-protected dashboard. No registration, billing, or multi-user roles.
- Secure initial password setup through an offline/interactive CLI such as `neda set-password`; read without echo; never accept the password as a command-line argument. No unauthenticated web bootstrap endpoint on a public interface.
- Argon2id password hashing, preferably; document any deviation.
- Secure server-managed sessions, `HttpOnly` cookies, appropriate `SameSite`, `Secure` in production, login rate limiting, and safe logout.
- Login does not require an authenticated-session CSRF token. Login requires same-origin `Origin`/`Referer` validation and rate limiting. Every authenticated state-changing request requires the session CSRF token and origin validation.
- Protect all private APIs. Public health endpoints only.
- Never expose stored secrets again in settings responses.
- Never place secrets in frontend bundles, logs, Git history, example files, or errors.
- Encrypt Telegram credentials and other sensitive settings at rest with a persistent external key. Document backup, restore, rotation, and key-loss consequences.
- Validate Push subscription input; do not trust client-supplied URLs blindly; apply the SSRF controls in `ARCHITECTURE.md` §13.4/§14.4.
- Constant-time comparisons where relevant; validate bodies; avoid leaking internals.
- Review reverse proxy, HTTPS, cookie, host-header, and firewall assumptions.

### 3.9 Calendar, dates, and timezone

- Departure date is persisted as a Gregorian calendar date, not a timestamp and not a server-local date.
- The UI selects dates through a Persian-calendar date picker; the API accepts and returns the canonical Gregorian `YYYY-MM-DD`.
- MrBilit API requests use Gregorian `YYYY-MM-DD`.
- Booking URLs, when validated, use the verified Persian-calendar date format.
- Use a maintained Persian-calendar conversion library; do not invent conversion algorithms.
- Test round trips, leap years, month/year boundaries, and known conversion fixtures.
- Keep departure date distinct from departure/arrival timestamps.
- For v1 Iran-focused routes, use `Asia/Tehran` for departure-day expiry and display policies. Stop polling only after that local calendar day ends; never use midnight in the server's timezone. Do not infer an exact departure timestamp when unavailable.
- Test timezone behavior independently of the host timezone (the local development host is currently `Asia/Tehran`, which could mask bugs).

### 3.10 Deletion, retention, and history

- Manual stop is a reversible pause/disable. There is no separate terminal `stopped` state.
- Jobs expire automatically after the departure day in `Asia/Tehran`.
- Deleting a monitor is a hard delete with a confirmation. Proposal: deleting a monitor removes its provider mappings, current states, episodes, availability events, and delivery records. This is **PROVISIONAL**; the owner may prefer retaining historical events after deletion.
- Observation/diagnostic retention is bounded. Proposal: check runs 14 days; notification attempts 30 days; events/deliveries 180 days; closed provider-health episodes 180 days; expired sessions cleaned up. Raw provider responses are never archived. Retention deletes dependent deliveries/events first, then prunes only closed episodes/states with no remaining events; open episodes are never pruned.
- Retention values may be adjusted after measuring disk usage; any change with data-integrity impact requires owner approval.

### 3.11 Documentation and README

- The project README and normal repository documentation are written entirely in English unless a specific product requirement explicitly requires another language.
- The initial documentation commit prepared by `REPO-003` after GATE-001 approval contains an English planning-state README. It describes the repository as a planning-stage project, explicitly stating that application implementation is not yet present.
- The planning-state README is reviewed against the actual files in the repository before the initial documentation commit. It must not invent commands, environment-variable names or values, unimplemented features, or misleading setup instructions. Configuration/environment-variable placeholders must not be presented as supported settings; the real configuration section is deferred until the implementation and configuration exist.
- The eventual README, completed after implementation, covers as applicable: purpose and scope; core features; high-level architecture; prerequisites; local development setup and execution; configuration and environment variables; security and secret handling; initial password setup; Telegram notification setup; browser Push setup; Docker deployment; reverse proxy and HTTPS; backups and restore; testing and validation; troubleshooting; and deployment/operational boundaries.
- Implementation-specific setup, commands, configuration, and verified behavior are added only after the implementation and relevant tests exist. The README must clearly distinguish implemented functionality from planned functionality.
- The README and repository documentation must never contain credentials, tokens, keys, subscription endpoints, production environment files, or other secrets.

---

## 4. Detailed behavior decisions

### 4.1 Initial availability

When the first successful check for a new monitor/provider/offering returns `Reservable`, Neda creates exactly one initial availability event and enqueues notifications. This is required so currently bookable tickets are not silently missed.

- First observation `NotReservable`: create state; no event.
- First observation `Unknown`: create state as Unknown; no event.
- First check is an error/empty: no state; no event; open episodes are unaffected.
- Later `Reservable` after any non-open-episode history opens a new episode and notifies.

### 4.2 Empty inventory lists

A valid, well-formed, successfully parsed empty inventory list is a successful query with zero returned offerings.

- Record the check as successful/empty.
- Do not create offerings or availability events.
- Do not close any open episode. Absence of an offering in a response is not decisive evidence that the specific offering is not reservable.
- Update a `last_missing_at`/diagnostic marker for previously known offerings so the UI can say "not returned by provider; availability unknown".
- Do not claim "sold out" for the route.

This follows the brief's rule that an empty list is not proof all tickets are sold out, combined with the rule that only decisive `NotReservable` evidence closes an episode. A consequence is documented: if an offering disappears and later reappears as `Reservable`, it may remain in the same open episode and produce no second notification. Correctness is favored over speculative re-notification. Changing this policy requires owner approval and evidence.

### 4.3 Pause/disable

- `enabled = false` means paused; scheduler excludes the monitor/providers.
- Pause is reversible at any time before expiry.
- Resume sets the monitor enabled and schedules a near-term check with jitter.
- An expired monitor cannot be resumed; the owner must edit the departure date (a material change that resets affected observation state).
- Provider failures, repeated errors, and degraded health never disable a monitor.
- Every attempt starts with one short, `SCH-004`-owned attempt-start transaction that inserts a single `check_runs` row with `outcome='running'`, `started_at`, and the request’s `config_version`. That transaction commits before provider I/O and writes only `check_runs`; it never updates target timing/scheduling, availability, episodes, provider health, events, or outbox. If the target row is already missing or the start transaction cannot commit, the attempt is not started and no provider request is made. `SCH-010` owns the later outcome transaction that transitions the same row to exactly one final outcome and sets `finished_at`.
- A check already in flight when the owner pauses, removes, or materially edits the target, or when the target expires or is deleted, may finish at the network level, but the result must not be applied unless the target is still current. After provider I/O returns and before selecting success or provider failure, the engine re-loads the target and verifies that it exists, is active and enabled, is not expired, has the same `config_version` used for the request, and has the same booking slug/URL-generation configuration retained with the request. This gate applies regardless of whether the provider returned a result, an error, or a cancellation.
- If the target is disabled, paused, expired, removed, deleted, has a different `config_version`, or has a changed booking configuration, the result follows the discarded path: the existing `running` row is transitioned to `outcome='discarded'` with the request’s version and sanitized diagnostics; no second row is inserted. Do not update `next_due_at`, `last_attempt_at`, `last_outcome`, failure counters, availability state, episodes, or provider health; do not create events or outbox rows. If deletion cascaded the target and running row away, no diagnostic row can legally exist. Target invalidity takes precedence over cancellation. A booking-configuration mismatch is a URL-safety discard and does not reset inventory state.
- Cancellation or shutdown follows the abandoned path, not the provider-failure path. The existing `running` row is transitioned to `outcome='abandoned'` when the target row and database remain available; otherwise it is left `running` for startup recovery, which marks it abandoned. Cancellation/shutdown never updates target scheduling, availability, episodes, or provider health and never creates a health event. A deadline expiry is a timeout failure, not abandonment.
- A provider failure may update provider health only when the post-I/O validity gate confirms the result is applicable to the current target. `SCH-010` calls the SCH-006 failure transition for every applicable provider error; A-21 determines whether the error increments the outage counter or only updates diagnostics. The start transaction and the outcome transaction are separate; only the outcome transaction may change target state, availability, health, events, or outbox.

### 4.4 Provider outage alerts

- Provider health is separate from ticket availability.
- Track last attempt, last success, last error category/time, duration, consecutive failures, and stale status per provider. `provider_health.last_duration_ms` records the duration of the last completed applicable check; discarded and abandoned checks do not update it.
- Provider-level `last_attempt_at` and `last_duration_ms` update for every applicable completed outcome; `last_success_at` updates on success/empty; `last_failure_at` and `last_error_kind` update on applicable errors. These are diagnostics and do not by themselves imply an outage. Provider-level `consecutive_failures` and degraded/recovery episodes are governed by the table below.
- Target-level `last_attempt_at`, `last_outcome`, and `last_error_*` update for applicable success/empty/error outcomes; discarded and abandoned checks update only their `check_runs` row. Target-level `consecutive_failures` increments only on applicable infrastructure failures, resets on applicable success/empty, and is unchanged on config/route/ordinary 4xx, discarded, and abandoned outcomes. Target backoff is applied only for failures classified as transient/infrastructure; config/route/ordinary 4xx use the normal interval. The approved formula is `interval × 2^failures` using the incremented failure count (first failure = 2× interval), capped at `max(4× interval, 30 minutes)`, with ±10% jitter applied after the cap and no normal-interval clamp. A classified `RetryAfter` value is a lower bound on the final retry delay; material edits and resume do not reset the counter.
- A successful, applicable check — including a valid empty result — counts as health success and may recover an open degraded episode.
- After **three applicable provider-infrastructure failures** under the approved Option A below, create **one** deduplicated provider-degraded alert and deliver it through configured channels. Infrastructure failures are network failures, timeouts, HTTP 429/5xx, and invalid/malformed provider responses according to the existing classification.
- Do not repeat the alert for every failed poll while the episode remains open.
- Clear the alert episode on the first subsequent successful, applicable check and record a recovery notification/event. Provider-health counters, episode changes, events, and corresponding outbox rows commit in one transaction.
- Cancellation, shutdown-abandoned checks, and discarded checks are not provider failures and do not update provider-health counters or create health events.
- Provider-health messages are clearly labeled as provider issues, not ticket availability, and must not imply tickets are sold out.

**Transition table (A-21 Option A — approved 2026-10-24):**

| Result class | Provider `consecutive_failures` / episode | Provider diagnostics | Target `consecutive_failures` / scheduling | Availability / episodes | Outbox / notification |
|---|---|---|---|---|---|
| Applicable success, including valid empty | reset to 0; close open episode if any; one recovery event/outbox if an episode was open | update `last_attempt_at`, `last_success_at`, `last_duration_ms`; retain last error as historical diagnostic | reset target counter; update `last_attempt_at`, `last_success_at`, `last_outcome=success/empty`; set normal `next_due_at` | apply observations; valid empty applies zero and does not close episodes | availability events as applicable; health recovery if an episode was open |
| Applicable infrastructure failure (timeout, network, 429, 5xx, invalid/malformed response) | increment by 1; at 3 open one episode and create one degraded event/outbox; further failures create no event | update `last_attempt_at`, `last_failure_at`, `last_error_kind`, `last_duration_ms` | increment target counter; update `last_attempt_at`, `last_outcome=error`, `last_error_*`; apply target backoff | no availability or episode change | health degraded event at threshold only |
| Config/route/ordinary 4xx error | unchanged under Option A; no episode/event; no recovery | update `last_attempt_at`, `last_failure_at`, `last_error_kind`, `last_duration_ms` | unchanged target counter; update `last_attempt_at`, `last_outcome=error`, `last_error_*`; normal interval | no availability or episode change | none |
| Discarded (target invalid/paused/expired/config changed/booking config changed) | unchanged | unchanged | unchanged; only the running `check_runs` row becomes `discarded` | unchanged | none; no worker wake |
| Abandoned (caller cancellation/shutdown) | unchanged | unchanged | unchanged; only the running `check_runs` row becomes `abandoned` | unchanged | none; no worker wake |

Under approved Option A, “consecutive” means consecutive applicable infrastructure failures since the last applicable success; config/route/ordinary 4xx, discarded, abandoned, and business/availability outcomes do not increment and do not reset the count. This interpretation was approved by the owner on 2026-10-24 under `GATE-014`.

**A-21 — resolved 2026-10-24 (Option A, infrastructure-only):**

- **Option A (approved):** only applicable infrastructure failures count; non-counting outcomes leave the provider counter unchanged; only success/empty resets it. Business outcomes such as NotReservable and ordinary availability results are not provider failures. Consequence: route-specific config errors do not trigger a provider-wide alert, but three infrastructure failures separated by config errors still can.
- **Option B (not selected):** every failed check counts, including config/route/ordinary 4xx; success/empty resets. Consequence: one bad route could falsely degrade a healthy provider and produce a provider-wide alert.
- **Option C (not selected):** any non-success outcome breaks or resets the sequence; only three consecutive failed checks count. Consequence: intermittent failures may never reach the threshold, reducing false alerts but also delaying real outage detection.

Rationale for Option A: provider health should reflect provider distress, not route/configuration mistakes or normal availability outcomes. The approved rule avoids a single bad route degrading a healthy provider while still detecting repeated infrastructure failures.

### 4.5 Cross-provider notifications

- One event and delivery set per provider-specific availability event.
- Do not suppress one provider's event because another provider's inventory differs.
- Do not add/merge inventory quantities across providers.
- Do not aggregate notifications across providers in v1.
- Cross-provider journey matching is optional/display-only; no entity merging is required.
- If matching is ambiguous, keep observations separate.

### 4.6 Booking URLs

- A booking URL is included only when the provider adapter can construct it from a validated pattern and a validated route slug/identifier.
- The known illustrative pattern `https://mrbilit.com/trains/{slug}?departureDate={jalali}` is **PROVISIONAL**; neither the slug derivation nor the date format is verified.
- Never derive an arbitrary slug by lowercasing city names.
- Never invent identifiers, URL paths, or date formats.
- V1 accepts an optional owner-supplied/validated provider-specific booking slug. If no validated slug exists, `bookingURL` is absent and the UI explains the limitation.
- Jalali conversion for the booking URL uses a maintained, tested library only after the query format is confirmed.
- Test URL generation with owner-verified examples and calendar fixtures.
- A booking link must be `http`/`https` only and is escaped/validated before display.

### 4.7 Gender and other provider parameters

- `genderCode` semantics and allowed values are unknown. Do not invent enums or defaults.
- Safe default: omit `genderCode` unless the owner supplies a validated value or live validation proves it is required.
- The API/schema may reserve an optional per-provider gender code field, but the UI should expose it only if validation shows a genuine requirement.
- `availableStatus` and `exclusive` are known parameter names but their semantics/allowed values are unverified. Omit them until validated; do not send arbitrary booleans/enums.
- If the provider requires any of these parameters, the adapter and UI must be updated from evidence, not guesswork.

### 4.8 Editing and state reset

A monitor edit can invalidate journey identity or passenger semantics. Use a per-provider `config_version`:

- Label-only changes do not reset state.
- Material changes to route, departure date, passenger counts, provider route IDs, gender code, or provider selection bump the affected provider's `config_version`.
- Removing a provider deactivates its target row rather than deleting it, so historical events remain; re-adding reactivates it and bumps the version again.
- Open episodes for the old version are closed with reason `config_changed` (no availability event). New checks use the new version.
- A departure-date edit is material: within the same material-edit transaction, recompute `expires_at` from the new departure date at the end of that day in `Asia/Tehran`, and clear `expired_at` when the recomputed expiry is in the future. If the monitor was paused, it remains paused and is not scheduled until resumed; resume is allowed only when the recomputed expiry is in the future. Status, resume, scheduling, and the post-I/O gate must use the reconciled `expires_at`/`expired_at` values, not a stale expired cache.
- After the material-edit transaction commits, affected targets that remain active, whose monitor is enabled, and that are not expired are scheduled for a near-term check using the existing `now + small jitter` convention; the scheduler is woken after commit. Removed, inactive, expired, or deleted targets are not scheduled. A date edit uses the recomputed expiry for this decision.
- A discarded or abandoned result from a worker using an older `config_version` or an older booking slug never updates target scheduling, failure counters, or provider health; the post-I/O `config_version` and booking-slug checks prevent an old worker from overwriting new scheduling state or emitting an obsolete URL.
- Historical events are preserved. A currently reservable offering may produce a new initial event after a material edit; this is intentional and must be explained in the UI/docs.
- Booking-slug-only changes do not bump `config_version` and do not reset inventory state. The post-I/O gate compares the request’s booking slug with the current target’s `booking_slug`; a mismatch makes the attempt discarded (diagnostic `check_runs` only) so no event with an obsolete URL is created. The next check uses the new slug; subsequent events use the new URL.

### 4.9 Multiple reservable offerings

V1 notifies for every confirmed reservable inventory offering that matches the monitor's route, date, provider, and passenger request. This can produce several notifications for one train (for example, multiple classes). The brief requires this; do not silently aggregate. Cross-provider aggregation is out of scope.

### 4.10 Provider availability vs check outcome

These are separate:

- **Check attempt lifecycle:** every attempt begins as `running` in its `check_runs` row, then reaches exactly one final outcome: success, successful-empty, error (timeout/HTTP/parse/config), discarded (target invalid/paused/expired/removed/config changed/booking config changed), or abandoned (caller cancellation/shutdown). `running` is internal and is never a provider verdict. Only applicable success/error outcomes can update target diagnostics; discarded/abandoned outcomes are diagnostic-only.
- **Offering state:** Reservable, NotReservable, Unknown.

A failed check does not rewrite offering states; the UI shows the last known state with a clear "last checked/failed" timestamp. An `Unknown`/error is never displayed as "sold out". A successful check with some unknown offerings shows counts and reasons. A failed check that is no longer applicable (discarded) or was abandoned is not a provider verdict and must be shown as such, not as a provider failure or availability change.

### 4.11 Notification duplication

External delivery is at-least-once. Neda uses stable event IDs, unique delivery rows, and bounded retries to minimize duplicates, but a crash between sending and marking sent can produce a duplicate. The product/docs must state this limitation; do not promise exactly-once.

### 4.12 Manual "check now"

Not required in v1. If added as polish (P2), it must respect the same in-flight guard, rate limits, timeouts, and provider health accounting. It must not bypass the scheduler or create overlapping checks.

---

## 5. Assumptions register

| ID | Statement | Classification | Impact / action |
|---|---|---|---|
| A-01 | Working directory is `/Users/fallah/Desktop/Neda`; it is a Git repository on `main` at `9dd0d8a` at the start of the dashboard session, with approved planning docs, an ignored local `docs/.codegraph/` index, an ignored root `.codegraph/` index, and a React/TypeScript dashboard under `web/`. | VERIFIED (updated 2026-10-24) | Preserve existing docs; do not commit generated graph data. Dashboard work is under `GATE-020`; live provider, Push, and deployment work remain gated. |
| A-02 | GitHub CLI is authenticated to account `am-fallah` with `repo`/`workflow` scopes; no token was printed. | VERIFIED | Repo creation is possible after approval. |
| A-03 | The private repository `am-fallah/neda` exists under `am-fallah`; default branch `main`; no open PRs at the start of the dashboard session. | VERIFIED (updated 2026-10-24) | Inspect before each create/push; never force-push or change visibility. |
| A-04 | Git 2.54.0, Node 26.10.0, npm 11.19.1, pnpm 12.11.2, make 3.81, clang 21, sqlite3 3.54.0, curl 8.7.1 are available. | VERIFIED | Local frontend work is possible. |
| A-05 | Go 1.27.3 is installed and pinned in `go.mod`; the local toolchain built and tested the backend. | VERIFIED (updated 2026-10-24) | Keep CI pinned to `go.mod`; verify dependency compatibility before upgrades. |
| A-06 | Docker/Compose is not installed; no Colima/Podman/Lima. | VERIFIED | Docker build verification must use CI or wait for installation. |
| A-07 | CodeGraph 1.6.2 is installed at `~/.local/bin/codegraph`; MCP `codegraph` is connected; the root project index is initialized and used for backend navigation; an empty initialized index remains at `docs/.codegraph`; root graph data is ignored. | VERIFIED (updated 2026-10-24) | Do not commit graph data. Removing or moving the nested index requires a separate owner decision. |
| A-08 | OpenCode 2.1.4 is running; `opencode mcp list` reports `codegraph` and `shadcn` connected. | VERIFIED | MCP loaded in this session. |
| A-09 | Local host timezone is `Asia/Tehran` (+0330). | VERIFIED | Tests must force UTC/other zones to avoid masking timezone bugs. |
| A-10 | The brief is the authoritative product/engineering source. | VERIFIED | Conflicts must be reported. |
| A-11 | MrBilit endpoint and parameter names in the brief are accurate enough to plan against. | PROVISIONAL | Independent validation is required; do not treat examples as defaults. |
| A-12 | MrBilit response envelope, field types, time formats, price units, and positive-inventory shape are unconfirmed. | **PARTIALLY VERIFIED 2026-10-25** | Live validation observed `trains[]`, `prices[].classes[]`, numeric IDs, local Tehran timestamps, and positive/zero-capacity classes. Price units (Rial vs Toman), child/infant inventory semantics, and empty/route-not-found live cases remain unverified; parser stays conservative. |
| A-13 | `capacity` means remaining bookable seats for the offering, and passenger-count semantics are as the UI implies. | REQUIRES OWNER INPUT | Conservative classification until validated. |
| A-14 | Infants consume inventory. | PROVISIONAL (conservative) | Count infants toward required capacity until told otherwise. |
| A-15 | `genderCode` is required and its allowed values are known. | REQUIRES OWNER INPUT | Omit by default; do not invent enums. |
| A-16 | `availableStatus`/`exclusive` are optional and their values are known. | REQUIRES OWNER INPUT | Omit by default; validate later. |
| A-17 | The illustrative booking URL pattern/slug/date format is valid. | REQUIRES OWNER INPUT / validation | Do not generate a link without a validated slug and date format. |
| A-18 | A real positive MrBilit sample exists and can be captured safely. | **VERIFIED 2026-10-25** | One live response contained two positive classes (capacity 2/3, both flags true) and four known-negative zero-capacity classes. Sanitized fixture and evidence recorded; no raw response or auth data committed. |
| A-19 | Initial `Reservable` observations should notify. | VERIFIED (brief) | Implement one initial event. |
| A-20 | Empty lists must not close open episodes. | PROVISIONAL (derived from brief safety rule) | Document the re-notification limitation. |
| A-21 | Provider-health counting rule. Option A (approved 2026-10-24): only infrastructure failures count; non-counting outcomes neither increment nor reset the provider counter; only success/empty resets. Business/availability outcomes are not provider failures. Option B (brief-literal): every failed check counts, including config/route/ordinary 4xx. Option C (strict consecutive): any non-success breaks/resets the sequence. | **RESOLVED — Option A** (`GATE-014`, 2026-10-24) | Provider health counts only applicable infrastructure failures; route/config/ordinary 4xx and business outcomes do not count. Options B/C are not implemented. |
| A-22 | Hard-deleting a monitor may remove its history. | PROVISIONAL | Owner may choose retention/soft delete before implementation. |
| A-23 | Retention defaults (14/30/180 days) are appropriate. | PROVISIONAL | Measure and adjust; bounded by approval. |
| A-24 | Proposed stack/dependencies (modernc SQLite, webpush library, Jalali library, RHF/Zod) are compatible and maintained. | PROVISIONAL | Evaluate at implementation; document deviations. |
| A-25 | Production server OS/CPU/RAM/disk/proxy/domain/ports are known. | REQUIRES OWNER INPUT | No server access in planning; provide minimum facts later. |
| A-26 | VAPID subject, public domain, and HTTPS setup are available. | REQUIRES OWNER INPUT | Push cannot be production-tested without them. The implementation is complete and locally tested; no real Push send was authorized. |
| A-27 | Telegram bot token/chat ID will be supplied later via the dashboard, not chat. | REQUIRES OWNER INPUT | Never ask for the token in chat or commit it. |
| A-28 | Live MrBilit validation target route/date and permission will be supplied. | **VERIFIED 2026-10-25** | Owner supplied Yazd→Mashhad, 2026-11-05, and authorized bounded reads; two unauthenticated GETs executed within budget. See `docs/MRBILIT_LIVE_VALIDATION.md`. |
| A-29 | V1 has no custom train/time/class/price filters. | VERIFIED (brief) | Do not add filter UI/API. |
| A-30 | Single-user, password-protected dashboard; no registration. | VERIFIED (brief) | No multi-tenant design. |
| A-31 | Polling default/bounds (300s; 60–3600s). | VERIFIED (brief) | Enforce server-side. |
| A-32 | Deployment must not occur without a separate approved plan. | VERIFIED (brief) | `GATE-004`. |
| A-33 | Monitor concurrency: at most one provider check per monitor may be in flight; different monitors may run concurrently within the global worker bound. | VERIFIED (owner decision 2026-10-24) | The stricter reading of the brief’s “Never overlap executions of the same monitor”; it also satisfies per-provider concurrency 1. `SCH-008` must test multi-provider independence sequentially within one monitor, and no scheduler path may dispatch a second target for a monitor whose check is in flight. |

---

## 6. Product acceptance criteria

These are product-level gates; the implementation plan maps each to tasks and evidence.

| ID | Acceptance criterion |
|---|---|
| AC-01 | Multiple independent monitors persist across restarts and can be created, viewed, edited, paused, resumed, and deleted. |
| AC-02 | A monitor can select multiple providers; per-provider configuration is persisted; one provider's failure does not invalidate another's success. |
| AC-03 | MrBilit is the first real adapter; the fake provider exercises the core with different route-ID requirements and availability rules and is never presented as real. |
| AC-04 | Availability rules are provider-specific and evidence-backed; errors/timeouts/malformed responses/unknowns/discarded/abandoned checks never become `NotReservable` or close episodes. |
| AC-05 | The documented negative sample classifies as `NotReservable`; positive classification is either validated or explicitly marked unverified. |
| AC-06 | Initial `Reservable` creates one event; continuous `Reservable` does not duplicate; a decisive close followed by `Reservable` creates a new event. |
| AC-07 | Event creation and outbox rows are atomic; unique constraints/idempotency survive restarts, retries, and duplicate processing. |
| AC-08 | Telegram settings can be saved/updated/removed/tested; delivery works; the token is never returned or logged; credentials are encrypted at rest. |
| AC-09 | Real server-side Web Push works through a service worker, persists subscriptions, survives browser closure, handles clicks, and cleans up invalid subscriptions. |
| AC-10 | Validated booking links appear when available; unverified links are omitted with a visible limitation. |
| AC-11 | Jalali/Gregorian conversion and Tehran departure-day expiry are tested independently of the server timezone. |
| AC-12 | Authentication, sessions, CSRF/rate controls, secret storage, and private routes are protected; no default credentials; login uses same-origin checks plus rate limiting, and every authenticated state-changing route requires the session CSRF token. |
| AC-13 | Provider-degraded alerts follow the approved A-21 rule. Under Option A, they appear once after three applicable infrastructure failures, do not repeat per poll, and clear with recovery; config/route/ordinary 4xx, cancellation/abandoned, discarded, and business/availability outcomes do not increment outage counters and do not reset the provider counter. |
| AC-14 | The dashboard clearly distinguishes no reservable tickets, unknown/failed checks, paused, and expired states. |
| AC-15 | Critical business logic has deterministic automated tests; builds/static checks actually pass; no ordinary test requires live provider access. |
| AC-16 | Docker Compose deployment design covers non-root runtime, persistent DB/key volumes, health checks, resource limits, backups, and reverse proxy/HTTPS. |
| AC-17 | Documentation and known limitations accurately reflect behavior; no fabricated API behavior, test results, or deployment claims. |
| AC-18 | No production deployment occurs without explicit owner approval. |

---

## 7. Required owner decisions and inputs

The owner does not need to answer anything already fixed by the brief. The following are genuinely unresolved:

1. **Server facts** (minimum): Ubuntu version/architecture, CPU cores, RAM, free disk, Docker/Compose availability, ports already in use, reverse proxy (Caddy/Nginx), domain/DNS/TLS, outbound HTTPS access, backup location. Do not access the server during planning.
2. **MrBilit validation permission and target**: approval for a dedicated validation run that makes at most five read-only HTTP GET attempts in total, one process, one owner-approved route/date, no parallel requests, no automatic retries, redirects disabled, and the next request starting at least five seconds after the previous response completes. Any redirect response stops the run; all attempts count. Confirm the target route/date and whether network access is permitted by provider terms. Do not include a booking-page fetch unless it is explicitly part of the approved five-request budget.
3. **MrBilit semantics**: capacity meaning, child/infant inventory behavior, required query parameters, gender enum/requirement, `availableStatus`/`exclusive` semantics, response envelope/time/price formats, and rate-limit guidance.
4. **Booking URL**: a real, owner-verified booking URL/slug example and confirmation of the Persian date format. If unavailable, v1 will omit booking links.
5. **Telegram**: bot token and chat ID to be entered through the dashboard after implementation; do not paste them into chat.
6. **Web Push**: public HTTPS domain, VAPID contact subject, and confirmation that the deployment supports service workers/HTTPS.
7. **Deletion/history**: confirm whether deleting a monitor should erase its events/delivery history (proposed) or retain history.
8. **Provider-health channels**: confirm alerts should go to both configured channels by default (proposed).
9. **Retention defaults**: confirm 14/30/180-day proposals or specify alternatives.
10. **Local toolchain**: approval to install Go (and optionally Docker/Colima) on the development Mac before implementation. Go is currently absent; Homebrew metadata is only an observation. Verify the current stable version and compatibility with the selected SQLite/Web Push/Jalali libraries before pinning it.
11. **Monitor concurrency**: resolved by the owner on 2026-10-24 in favor of monitor-wide serialization (at most one in-flight check per monitor); see A-33. Different monitors may run concurrently.
12. **Provider-health counting (A-21)**: resolved by the owner on 2026-10-24 to Option A (infrastructure-only). Config/route/ordinary 4xx and business/availability outcomes do not count as provider failures; success/empty resets the counter.

---

## 8. Success metrics and verification philosophy

Neda is correct when:

- No `NotReservable` or "sold out" state is derived from an error, timeout, malformed response, empty list, unknown field, or discarded/abandoned check.
- No duplicate availability event is created for one continuous episode across retries/restarts.
- A genuine later episode creates a new event.
- A single provider failure does not hide another provider's success.
- Telegram and Push failures are independent, visible, and retried only when appropriate.
- Restarting the process does not lose active monitors, open episodes, pending deliveries, or provider health.
- Tehran expiry and Jalali conversion are correct on a host in any timezone.
- Secrets never appear in API responses, logs, Git, images, or example files.
- Resource use is measured, not assumed.

**Verification rule:** never fabricate provider behavior, test results, repository state, delivery, or deployment. Mark unverified behavior explicitly. A test that uses synthetic fixtures is labeled synthetic and is not evidence of real-world provider semantics.

---

## 9. Out-of-scope and future work (not authorized in v1)

- City search/catalogue; reliable source required.
- Custom filters and per-monitor intervals.
- Cross-provider aggregation, canonical journey merging, or notification suppression.
- Price history and analytics.
- Full E2E browser automation; optional only if justified and approved.
- Multiple users, sharing, roles, API tokens for third parties.
- Automatic purchase, payment, or seat selection.
- Mobile native apps.
- Kubernetes/Redis/external queue.
- Production deployment or server changes before a separately reviewed plan.

---

## 10. Document change control

- Any change to availability semantics, authentication, secret handling, data deletion/retention, provider behavior, polling bounds, or deployment scope requires explicit owner approval and an update to this specification, `ARCHITECTURE.md`, and `IMPLEMENTATION_PLAN.md`.
- `docs/IMPLEMENTATION_PLAN.md` is the single source of truth for task status and live progress. This product spec is the behavior authority after the brief.
