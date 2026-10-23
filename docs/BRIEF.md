# Neda — Product and Engineering Brief

**Status:** Approved baseline for planning; implementation must not begin until the owner explicitly approves the plan produced after environment inspection.

## 1. Mission and priorities

Build **Neda**, a lightweight, self-hosted, single-user train-ticket availability monitor. The owner monitors multiple Iranian train routes and travel dates simultaneously, detects ticket offerings that become reservable, and receives actionable notifications through Telegram and real Web Push. The browser may be closed while monitoring continues.

Priority order:
1. Correct availability detection.
2. Reliable background monitoring and notifications.
3. Clean, extensible multi-provider architecture.
4. Security and credential protection.
5. Low CPU, memory, disk, and network usage.
6. Maintainable code and meaningful automated tests.
7. A polished but minimal dashboard.

Avoid unnecessary infrastructure, dependencies, abstractions, and distributed-system complexity. This is a single-user application, not a SaaS or multi-tenant system.

## 2. Workflow and approval gates

### Phase 1 — Environment discovery

Inspect the current local working directory and development environment. Determine:
- Current directory, repository state, branch, working-tree changes, and whether this is an existing project.
- Existing project instructions (including `AGENTS.md` and OpenCode instructions).
- Git/GitHub CLI availability and safe authentication status; never print credentials.
- Whether `neda` already exists in the authenticated GitHub account, if read-only inspection is possible.
- Go and Node.js versions, package managers, build tools, Docker availability, and CodeGraph integration capabilities.
- CodeGraph CLI version and availability, current project index status, and whether the current OpenCode session exposes CodeGraph MCP tools. Check actual capabilities; do not infer them from a configured server alone.
- Relevant OS/architecture constraints.

Do not overwrite, delete, or reinitialize existing work without a clear reason and owner approval. Do not SSH to or inspect a production server during discovery. Do not display secrets or environment variable values that may contain them.

### Phase 2 — Specification and plan

Prepare:
- `docs/PRODUCT_SPEC.md`
- `docs/ARCHITECTURE.md`
- `docs/IMPLEMENTATION_PLAN.md`
- Proposed repository tree and milestones.
- SQLite schema proposal and migration plan.
- Provider interface and registry proposal.
- Availability classification, identity, event-transition, and deduplication strategy.
- Security checklist.
- Testing and acceptance plan.
- Resource budget for the existing server, based only on information available. Do not access the server to discover its specifications; mark unknowns and request the minimum needed details.

Classify important facts as **verified**, **provisional**, or **requires owner input**. Identify real ambiguities, choose sensible defaults when safe, and document any proposed deviations from this brief.

**Stop and wait for explicit owner approval after presenting the plan.** No substantial implementation, GitHub repository creation, commits/pushes, or server changes before approval. Do not ask the owner to answer things already specified here.

### Phase 3 — Private repository

After plan approval, check the authenticated account and existing repository state again. The requested repository name is `neda` and it must be private.
- Use authenticated GitHub CLI when available.
- Never create a public repository as fallback.
- If authentication is missing, stop this step and explain the exact next action. Do not ask for a personal access token to be pasted into chat or source code.
- If a repository named `neda` already exists, inspect its visibility and contents safely. Do not overwrite history or unrelated content; ask for approval if reusing it is not clearly safe.
- Preserve an appropriate current project directory; otherwise propose a path based on inspection.
- Commit only reviewed documentation after checking staged files for secrets; push only after verifying the remote is private.
- Report repository creation and push outcomes accurately.

### Phase 4 — Incremental implementation

After approval, work in small logical increments, add tests alongside code, format/lint/build/test, review diffs, and use descriptive commits. Use feature branches for substantial changes. Push only to the approved private repository. Keep this brief and `AGENTS.md` aligned with reality. Do not introduce infrastructure without a demonstrated requirement.

## 3. Product behavior and scope

The owner must be able to:
- Create, view, edit, enable/pause, resume, and delete multiple independent monitors.
- Monitor different routes, departure dates, passenger configurations, and one or multiple providers per monitor.
- Inspect monitor status, last successful check, next check, current inventory observations, provider health, diagnostics, and notification history.
- Configure and test Telegram and browser Push from the dashboard.
- Follow a booking link when a validated link is available.

Jobs and important state survive restarts. Monitoring runs in the backend without the browser. The default polling interval is five minutes, configurable globally, with hard bounds of 60 seconds minimum and one hour maximum. Never overlap executions of the same monitor. Respect provider-specific rate limits, use bounded concurrency and add scheduling jitter when appropriate.

A manual stop is the same as a reversible pause/disable in v1. There is no separate terminal `stopped` state unless planning identifies a compelling need and the owner approves it. Jobs expire automatically after the end of their departure day under the timezone policy in Section 12. Provider failures never permanently stop a job.

**V1 filter scope:** no custom filters by train number, departure-time window, wagon/class, or price unless the owner explicitly adds them during planning. Notify for every confirmed reservable inventory offering that matches the monitor's route, departure date, provider, and passenger request.

## 4. Baseline technology and implementation choices

Use this baseline unless environment inspection or current compatibility testing reveals a concrete issue. Document proposed deviations before implementation.

### Backend
- Go; one backend service with a persistent background monitoring worker.
- Standard `net/http` where practical; versioned REST API with explicit request/response models.
- SQLite and explicit schema migrations.
- Structured logging with redaction, context-aware requests, graceful shutdown, explicit timeouts.
- One executable and a small number of runtime processes where practical.

### Frontend
- React, TypeScript, Vite, Tailwind CSS, and shadcn/ui.
- Modern, minimal, accessible, responsive English-only dashboard; light and dark themes.
- Do not use Next.js without a concrete requirement.

### Deployment
- Docker Compose and a multi-stage image.
- Non-root runtime user, persistent SQLite volume, persistent encryption-key material supplied outside the DB, health endpoint, and suitable resource limits.
- Document reverse proxy/HTTPS for the existing Ubuntu host; do not access or change the host before explicit deployment approval.
- Do not use Kubernetes, Redis, a separate worker container, or an external database without demonstrated need.

### Dependency selection
Evaluate SQLite driver choices for CGO requirements, ARM64/AMD64, footprint, correctness, reproducible builds, and maintenance. Prefer a simple, supportable choice; a pure-Go driver such as `modernc.org/sqlite` is a candidate, not an unexamined mandate. Evaluate migration tooling and data-access style; prefer a small explicit repository layer and avoid a heavy ORM. Select maintained, tested Persian-calendar and Web Push libraries only after confirming current compatibility. Use a modest frontend stack; Vitest plus Testing Library is the default for component/workflow tests, with a manual end-to-end acceptance checklist. Add a full browser automation framework only if its benefit justifies its cost.

## 5. Multi-provider architecture (first-class from day one)

Implement a small idiomatic Go provider interface and registry. The monitoring engine must not depend on MrBilit-specific JSON or business rules.

Provider responsibilities should include:
- Stable provider identity and display metadata.
- Capability reporting and route/location input requirements.
- Fetching inventory for a normalized monitoring request.
- Parsing/mapping native responses into normalized journey and inventory observations.
- Provider-specific reservability evaluation, including passenger-count semantics when confirmed.
- Provider-specific booking-link generation when reliably possible.
- Error classification, timeout/cancellation handling, and rate-limit behavior.

Use typed Go models and explicit optional fields. Do not build a generic plugin framework, use reflection-based registration, dynamic code loading, or add layers without a demonstrated need.

A provider should be independently implementable and testable. Register MrBilit first. A second, deterministic **fake/test provider** with different route-ID requirements and a different availability rule must test the provider-agnostic engine. It must never be presented to the owner as a real ticket source. Adding a real future provider should require implementing/registering the provider, not rewriting scheduling, state-transition, or notification logic.

Each monitor can select one or multiple providers. Persist provider selection and provider-specific route mapping/configuration. A failure in one provider must not block a successful check from another. If provider-specific location IDs differ, store them per provider; never assume an ID is globally meaningful.

### Normalized observation model

Represent, where supplied by a provider:
- Provider identity and native identifiers.
- Monitor/job and normalized route.
- Journey/train identity, train number, operator, departure/arrival date-times.
- Wagon/class, inventory offering, price, passenger/capacity information when available.
- Normalized state: `Reservable`, `NotReservable`, or `Unknown`.
- Booking URL if validated.
- Observation time and freshness/staleness.
- Provider diagnostic reason and error category.

Fields absent from a provider remain optional. Preserve relevant native IDs/metadata for diagnostics. Do not archive full raw responses indefinitely; any temporary fixture/diagnostic data must be bounded, scrubbed, and free of credentials.

### Journey identity and cross-provider matching

Prefer reliable provider-native or canonical identifiers when available. Otherwise use a documented conservative match using route, departure date/time, train number, operator, and other reliable attributes. Never merge solely because route and date match. If identity is ambiguous, keep observations separate. Always maintain inventory state per `monitor × provider × journey/observation identity × offering/class` and never add or merge inventory quantities across providers.

A lightweight optional match key is acceptable for display/diagnostics, but cross-provider entity merging is not a v1 prerequisite. Favor correct, independent observations over speculative canonicalization.

## 6. Initial provider: MrBilit

Initial API target:

`GET https://train.mrbilit.com/api/GetAvailable/v2`

Known example query parameter names:
- `from` — origin city ID
- `to` — destination city ID
- `date` — Gregorian `YYYY-MM-DD`
- `genderCode`
- `adultCount`
- `childCount`
- `infantCount`
- `exclusive`
- `availableStatus`

Example route values only (not application defaults): Yazd ID `219`, Mashhad ID `191`, departure date `2026-10-20`.

Potential response fields include train-level `id`, `from`, `to`, `fromName`, `toName`, `trainNumber`, `departureTime`, `arrivalTime`, `providerName`, `corporationName`, `prices`, `meta`; nested class/offering fields may include `id`, `price`, `capacity`, `wagonName`, `shortTitle`, `compartmentType`, `isAvailable`, `reservationAvailable`, `capacityString`, `ownerName`, `wagonID`. These examples are not a complete or guaranteed schema. Validate response shape and semantics; do not hardcode sample IDs as defaults.

### Live validation permissions and limitations

If network access is available and the request is permitted by the provider's terms and applicable policy, the agent may perform **at most five sequential, read-only GET requests** against one representative route/date, with at least **five seconds between requests**. No parallel probing, scanning, authentication bypass, browser-session credential harvesting, or load testing. Record what was actually observed and redact sensitive material. If live requests are inaccessible or would be inappropriate, proceed with fixtures only and clearly label unverified assumptions. Do not make live network access a requirement for ordinary tests or CI.

When safe and available, capture representative, sanitized fixtures for positive inventory, no inventory, zero capacity, missing/null fields, empty lists, and errors. If no valid positive sample can be obtained, do not fabricate one; mark positive classification as unverified and write synthetic unit tests for designed edge cases, explicitly distinguishing synthetic tests from real-world verification.

### MrBilit availability classification — safety contract

One known sample must classify as `NotReservable`:
- `capacity = 0`
- `isAvailable = false`
- `reservationAvailable = true`

Therefore `reservationAvailable = true` alone is **never** sufficient evidence of a sellable ticket.

Build a provider-specific, documented decision table and table-driven tests based on validated native semantics. Requirements:
- `Reservable` only when there is sufficient validated positive evidence that the specific offering can be booked for the monitor's requested passenger configuration, with no unresolved contradictory evidence.
- `NotReservable` only when a validated response provides sufficient decisive negative evidence. Apply the known sample rule above.
- Missing, null, unexpected-type, insufficient, or contradictory evidence returns `Unknown`, unless real validated provider semantics prove the case unambiguously otherwise.
- A provider error must never be mapped to `NotReservable`.
- Do not treat a feature flag, advertised capacity, booking capability, or HTTP 200 response alone as proof of sellable inventory.
- Capacity semantics and whether infants require inventory must be investigated. Do not assume that `capacity > 0` guarantees enough seats for all requested passengers. Do not invent gender or other query enum values.
- A valid empty inventory list can be recorded as a successful query with no returned offerings; a route-not-found/error response, malformed envelope, or incomplete response is a provider/query error or `Unknown`, not proof that all tickets are sold out.

Cover positive and negative capacity, `isAvailable` true/false/missing/null, `reservationAvailable` true/false/missing/null, contradictions, unexpected JSON types, empty lists, route-not-found, invalid JSON, HTTP errors, timeouts, and incomplete responses. If semantics remain uncertain after the bounded validation, document the exact uncertainty rather than guessing.

### HTTP client

Use a reusable HTTP client with explicit timeouts, cancellation, bounded response sizes, connection reuse, appropriate non-secret headers, safe errors, and bounded retries only for appropriate transient failures. Honor provider rate limits and retry guidance. A successful HTTP status does not guarantee a valid response.

### Booking URLs

Known illustrative pattern:

`https://mrbilit.com/trains/yazd-mashhad?departureDate=1405-07-28`

The route slug and Persian-calendar date require separate validation. Never assume an arbitrary slug is derived by lowercasing city names, and never invent identifiers or URLs. For v1:
- Use an optional provider-specific, owner-supplied/validated booking route slug or a URL pattern directly verified from trustworthy evidence.
- Generate the Persian-calendar departure date with a maintained, tested library only after confirming the query format.
- Test URL generation with verified examples and date-conversion cases.
- If route slug, identifier, or pattern cannot be validated, leave `bookingURL` absent and surface the limitation; do not generate a misleading link.
- Include a validated booking URL in notifications when available.

## 7. Jobs and scheduler

Each job should persist:
- Stable ID and user label.
- Origin/destination display names and provider-specific IDs/mappings.
- Gregorian departure date.
- Adult/child/infant counts and any provider-required gender parameter.
- Selected providers/configuration.
- Enabled/paused and expired status, timestamps, last attempt, last success, next due time, and relevant diagnostics.

Use a simple scheduler suited to a low-resource, single-user service. It must:
- Run on the configured interval (default five minutes, hard minimum 60 seconds, hard maximum one hour).
- Avoid overlapping executions for a job/provider pair and avoid global concurrency spikes.
- Use bounded worker concurrency, request timeouts, per-provider throttling, small scheduling jitter, and bounded backoff for transient failures.
- Persist enough state to recover active jobs after restart.
- Avoid scheduling when no eligible jobs exist and avoid creating an unbounded goroutine for every future job.
- Continue checking other selected providers if one fails.
- Stop polling disabled jobs and expire jobs after the departure day in `Asia/Tehran`.

Distinguish successful reservable observation, successful non-reservable observation, unknown/incomplete response, timeout, HTTP failure, invalid response, and configuration/route errors. Temporary failures must not permanently disable a job.

## 8. Availability episodes, durable events, and deduplication

Notifications derive from durable state transitions, not from each polling result. Track inventory state per monitor, provider, stable journey identity, and offering/class.

Rules:
1. A transition from known `NotReservable` to `Reservable` creates an availability event.
2. If the first successful observation for a new monitor/provider/offering is already `Reservable`, create one initial availability event and notify. This is the v1 default so currently bookable tickets are not silently missed.
3. Repeated `Reservable` observations during the same continuous episode do not generate more events.
4. Only a **successful, decisive `NotReservable` observation** closes an open reservability episode.
5. `Unknown`, timeout, provider errors, malformed data, and missing observations never close an episode and never count as evidence that availability disappeared.
6. After a confirmed close, a later `Reservable` observation creates a new event.
7. Persist event creation and state transition atomically in a DB transaction. Use unique constraints/idempotency keys so duplicate processing, retries, and restarts do not create duplicate availability events.
8. Persist notification delivery separately from event creation. A delivery failure must not roll back or corrupt inventory state.
9. Do not claim exactly-once delivery to an external provider unless that provider guarantees it. Use stable event IDs, unique delivery records, and bounded retries to minimize duplicates.
10. Cross-provider inventory states remain independent. In v1, create a separate availability event and delivery per provider-specific event, even if matching indicates the underlying journey may be the same. Do not suppress one provider's event because of another provider's state. Cross-provider notification aggregation can be added later if needed.

Persist event details and notification delivery attempts, including channel, attempt timestamps, status (`Pending`, `Sent`, `Failed`, `RetryScheduled`), sanitized error category, and retry schedule. Define retention policies for observations and notification history.

### Provider-health alerts

Provider health is separate from ticket availability. Track last attempt, last success, last error category/time, request duration, consecutive failures, and stale status. When a provider has three consecutive failed checks, create one deduplicated provider-degraded alert; do not repeat it for every failed poll. Clear the alert episode after a successful check and record recovery. Provider-health notifications must not be confused with ticket-availability notifications. Avoid logging auth headers, cookies, session IDs, subscription endpoints, or credential-bearing URLs.

## 9. Notification channels

Each channel runs independently. Failure of Telegram must not prevent a Push attempt, and vice versa. Queue deliveries durably after committing the event. Retry only transient failures with bounded backoff. Include concise actionable details where available:
- Origin → destination.
- Departure date and time; arrival time if known.
- Train number/operator if known.
- Wagon/class, price, provider.
- Validated booking URL if available.
- Detection timestamp.

### Telegram

The owner already has a Telegram bot. Provide a dashboard settings flow to save/update/remove bot token and chat ID, test the connection, send a test message, and see connection status. Never return a full stored token from the API. Do not commit Telegram credentials. Prefer authenticated encryption at rest for sensitive settings, using a persistent key supplied separately from the SQLite DB; document safe key/database backup and restore. Never regenerate that key at each boot. Redact tokens from logs and error messages. Use bounded retries for transient failures.

### Browser Web Push

Implement actual server-side Web Push, not just in-page notifications:
- HTTPS in production, browser permission flow, service worker, subscription registration/persistence, server-side delivery, unsubscribe, invalid-subscription cleanup, and a dashboard enable/disable control.
- A test-notification action and correct click handling; clicking an availability notification opens its validated booking URL when available.
- Use VAPID or a maintained Go implementation and persist its key material across restarts.
- Do not log subscription endpoints or cryptographic secrets.
- Monitoring must continue with the browser closed.

## 10. Authentication and security

The dashboard is private and password-protected. No registration, billing, or multi-user roles.

Requirements:
- Secure initial password setup; no hardcoded or default credentials.
- Prefer an offline CLI command such as `neda set-password` for initial setup, reading the password interactively without echo and never accepting it as a command-line argument. Do not expose an unauthenticated web bootstrap endpoint on a public interface. Confirm the design against the actual container/deployment workflow.
- Use a modern password hash (prefer Argon2id when supportable; document any deviation), secure server-managed sessions, `HttpOnly` cookies, appropriate `SameSite`, `Secure` in production, CSRF protection where applicable, login rate limiting, and safe logout.
- Protect all private APIs. Only intentionally public health endpoints may be exempt.
- Never expose stored secrets again in settings responses. Never place secrets in frontend bundles, logs, Git history, example files, or errors.
- Encrypt Telegram credentials and any other sensitive settings at rest with a persistent external key. Explain backup, restore, rotation/recovery limitations, and key-loss consequences.
- Validate Push subscription input and do not trust client-supplied URLs blindly.
- Use constant-time secret comparisons where relevant, validate request bodies, and avoid leaking internals in errors.
- Review reverse proxy, HTTPS, cookie, host-header, and firewall assumptions.

## 11. Dashboard and REST API

### Dashboard

Use accessible, responsive shadcn/ui components; English-only interface; light/dark themes.

Overview should show active jobs, recent availability events, notification deliveries, last successful checks, provider health, and actionable errors.

Monitor list/detail should show label, route/date, selected providers, passenger counts, enabled/paused/expired state, last status/check, next check, and errors. Make a clear UI distinction between “no reservable tickets found” and “provider check failed/unknown.”

Create/edit should include monitor label, origin and destination names, manually entered provider city IDs for MrBilit, Gregorian departure date stored canonically but selected via a Persian-calendar date picker, passenger counts, gender-related input where genuinely required, and one-or-more provider selection. Validate input and reject identical origins/destinations. No invented city catalogue; a reliable city search can be future work. Expose optional validated route slug/configuration only if necessary for a real booking link.

Notifications view should show event details, provider, journey/ticket, event time, booking link, and per-channel delivery status so detection and successful delivery are distinguishable.

Settings should include Telegram setup/test/status, browser Push setup/test/status, provider configuration and health, global polling interval, and appropriate security options. Never redisplay full secrets. Show clear success/error feedback.

### API

Implement a versioned REST API with consistent JSON errors and appropriate status codes. Validate all inputs and protect private routes. Keep frontend-independent API models. Organize routes for authentication/session, monitor management, provider status/health, availability events, notification history/delivery status, settings, Push subscriptions, and intentionally public health checks. Generate OpenAPI documentation if practical without excessive complexity.

## 12. Calendar, dates, and timezone

- Persist departure date as a Gregorian calendar date, not an ambiguous timestamp and not a server-local date.
- Use a maintained Persian-calendar date picker and calendar-conversion library; do not invent the conversion algorithm.
- Use Gregorian `YYYY-MM-DD` for the MrBilit API and a verified Persian-calendar date for the booking URL.
- Test round trips, leap years, month/year boundaries, and known conversion fixtures.
- Keep departure date distinct from departure/arrival timestamps.
- For v1 Iran-focused routes, use `Asia/Tehran` for departure-day expiry and display policies. Stop polling only after that local calendar day ends; do not stop at midnight in the server's timezone or infer an exact departure timestamp when unavailable.
- Document the policy and test it independently of the server timezone.

## 13. City and route configuration

The first version may accept city names and numeric MrBilit city IDs manually. Do not invent a comprehensive city catalogue. Keep a canonical route representation separate from provider-specific location IDs and optional booking slugs. Validate that every selected provider has the route inputs it needs. A future city-search feature requires a reliable source and is not a v1 requirement.

## 14. SQLite, persistence, and retention

Design a normalized but practical SQLite schema with explicit migrations, foreign keys enabled, useful indexes, transactions around state transitions/event creation/enqueueing, safe concurrent access, busy timeout, and a clear single-writer strategy where needed.

Expected logical data includes:
- Monitoring jobs and provider selections/mappings.
- Provider inventory observations and health state.
- Availability state/episodes and durable events.
- Notification deliveries/attempts.
- Encrypted settings and Push subscriptions.
- Authentication/session data as appropriate.

Avoid tables that do not provide a concrete benefit, but keep ownership and foreign keys clear. Persist all state required after restart. Do not make in-memory state authoritative. Bound diagnostic/raw data retention and redact secrets before storage. Never put Telegram tokens, private keys, encryption keys, or Push secrets in ordinary logs or raw API archives.

## 15. Resource constraints and deployment

Target an existing low-resource Ubuntu server. Prefer one Go process, static frontend assets served by Go where practical, SQLite, bounded concurrency/queues, reusable clients, small models, indexed queries, minimal logs, and controlled retention. Do not poll when no eligible jobs exist; do not spawn an unbounded goroutine per future job.

During planning, provide a resource budget with assumptions clearly marked. Do not invent server CPU/RAM/disk data or access the production host. Ask the owner to supply only the necessary specs, such as OS/version, CPU/RAM, free disk, reverse proxy, domain/HTTPS, and existing port constraints. Estimate actual idle/active consumption only after measuring a real build on an appropriate environment; do not promise unrealistic performance.

Deployment design should cover:
- Multi-stage Docker build and non-root runtime.
- Compose configuration and restart policy.
- Persistent DB and encryption/VAPID key material outside ephemeral container storage.
- Secret provisioning, `.env.example` placeholders only, and secure permissions.
- HTTPS/reverse proxy, ports, firewall exposure, and health checks.
- Log rotation, disk retention, backup/restore, and rollback.
- Avoiding conflicts with existing services.

Do not access or change a production server or deploy until the owner approves a concrete deployment plan. Never expose SQLite or private/admin APIs directly to the public internet without appropriate protections. Never overwrite server data.

## 16. Testing and verification

Use deterministic automated tests for critical business behavior. No normal unit test may require live MrBilit access or real credentials.

### Provider tests
Use sanitized fixtures and mocked HTTP servers. Cover the known sample, positive/negative/missing/null/unexpected fields, contradictions, empty lists, route-not-found, malformed JSON, HTTP error, timeout, size limit, and request cancellation. Mark synthetic fixture tests separately from observed real samples. Test booking URL generation only against validated patterns and calendar fixtures.

### Multi-provider tests
Cover one provider, multiple providers, different route-ID requirements and availability rules, one failing while another succeeds, independent inventory states, duplicate processing, and ambiguous journey matches. Verify fake provider tests exercise the core without MrBilit-specific coupling.

### Scheduler tests
Cover interval bounds/default, no overlapping execution, concurrency limits, jitter/rate limiting, disabled and expired jobs, restart recovery, transient failures, backoff, no jobs due, and recovery after errors.

### Event/notification tests
Cover initial availability, `NotReservable → Reservable`, continuous availability without duplicate events, successful `Reservable → NotReservable`, later new episode, unknown/error not closing an episode, durable dedupe across restarts/retries, multi-provider independent events, delivery failures, retries, and independent Telegram/Push outcomes. Test provider-degraded alert dedupe and recovery.

### Security/API tests
Cover unauthenticated private routes, login failures/rate limits, session/cookie/CSRF behavior, initial password setup, settings encryption and persistence, secret redaction, Push-subscription validation, and invalid inputs.

### Frontend tests
Use Vitest and Testing Library for the highest-value interactions. Provide a concise manual acceptance checklist for login, create/edit/pause monitor, provider selection, Telegram test, Push registration/test, notification detail, and booking links. E2E automation is optional for v1 unless justified and approved.

### Required checks where supported
- `gofmt` and `go test ./...`
- `go vet ./...`
- `go test -race ./...` where practical
- Frontend lint and component tests
- Production frontend build and backend build
- Docker image build if Docker is available
- A modest local benchmark or load test if useful; never load-test the real provider

Report the exact commands and actual results. Never fabricate verification.

## 17. Repository hygiene and required files

Create and maintain:
- `README.md`
- `AGENTS.md`
- `docs/BRIEF.md` (this brief; preserve approved requirements)
- `docs/PRODUCT_SPEC.md`
- `docs/ARCHITECTURE.md`
- `docs/IMPLEMENTATION_PLAN.md`
- `.env.example` with placeholders only
- `.gitignore`
- migrations, Docker configuration, test fixtures, and CI workflow when practical

The README must cover purpose, architecture, prerequisites, local development, configuration, initial password setup, Telegram, browser Push, backups of DB and encryption keys, Docker deployment, reverse proxy/HTTPS, tests, troubleshooting, and security.

Never commit tokens, passwords, cookies, bearer headers, private keys, encryption keys, production environment files, private DB data, or browser Push subscription secrets. Inspect the staged diff before every commit. Use descriptive commit messages. Push only to the verified private repository. Do not change visibility to public.

## 18. Milestones and acceptance gates

Plan implementation as small milestones with explicit acceptance criteria, and keep the owner informed at each boundary. `docs/IMPLEMENTATION_PLAN.md` is the single source of truth for the task registry and live progress; do not use the conversation as the only record. Before implementation, break the milestones below into prioritized, stable-ID tasks. Each task must specify priority (`P0`, `P1`, or `P2`), status (`TODO`, `IN_PROGRESS`, `BLOCKED`, `DONE`, or `DEFERRED`), dependencies, acceptance criteria, and verification evidence/commands. Update the tracker when each task starts, becomes blocked, or completes, and reconcile it at the end of every session and milestone. Recommended order:

1. **Foundation:** repository/docs, selected dependency decisions, SQLite migrations/repositories, config/logging, authentication/session skeleton, CI.
2. **Provider engine:** normalized types and interface/registry, fake provider, MrBilit adapter, bounded HTTP client, validated availability rules and fixtures.
3. **Scheduler and state engine:** persistent jobs, bounded scheduler, restart recovery, per-provider observations, availability episodes, durable event dedupe.
4. **Notification backend:** durable delivery records/retries, Telegram configuration/test/delivery, provider-health alerts.
5. **Dashboard and API workflows:** monitor CRUD/pause/resume, settings, history, provider health, Persian-calendar input, responsive UI.
6. **Web Push and security hardening:** service worker, subscription lifecycle, VAPID persistence, browser tests, CSRF/rate limits/secret handling.
7. **Production readiness:** full tests, Docker build, backup/restore documentation, measured resource notes, reviewed deployment plan.

These are sequencing suggestions, not permission to omit requirements. The finished production-ready version must satisfy every applicable acceptance criterion below, including Web Push. `P0` means a correctness, security, data-integrity, or approval-gate priority; `P1` means required v1 capability; `P2` means polish/optional improvement. This ranking does not authorize skipping any mandatory definition-of-done item. A task can be marked `DONE` only when its acceptance criteria are met and required checks have actually passed; record exact command outcomes and leave checks marked not-run/failed when applicable. Any scope change, deferred mandatory requirement, or change with security/data/cost implications needs explicit owner approval.

### Definition of done
1. Code lives in the verified private GitHub repository `neda`.
2. Backend is Go; frontend is React, TypeScript, Vite, Tailwind, and shadcn/ui.
3. Persistent SQLite and migrations work safely.
4. Multiple independent monitors survive restarts.
5. One job can select multiple providers.
6. MrBilit is the first real adapter; the fake provider proves engine independence.
7. Reservability rules are provider-specific and evidence-backed; unknown/error never becomes sold-out.
8. One provider failure does not invalidate another provider's successful result.
9. Durable availability events and dedupe cover restarts, retries, and continuous episodes.
10. A genuine later availability episode can create a new event.
11. Telegram setup, testing, updates, removal, and delivery work without leaking credentials.
12. Web Push uses a service worker and real server-side Web Push; browser closure does not stop monitoring.
13. Validated booking links are included when available; unverified links are omitted.
14. Persian/Gregorian conversion and Tehran departure-day expiry are tested.
15. Authentication, sessions, CSRF/rate controls, secret storage, and private routes are protected.
16. Critical business logic has automated tests; builds and static checks actually pass.
17. Deployment and backup workflow are documented for a resource-constrained host.
18. No production deployment occurs without explicit owner approval.
19. Documentation and known limitations accurately reflect actual behavior.

## 19. Reporting at every phase

At every task update and at the end of each working session/milestone, report the relevant task IDs and statuses. Report what was inspected, decided, implemented, and changed; test commands/results; current branch and commit IDs; repository/push state; known limitations; remaining work; unverified assumptions; resource measurements when actually obtained; and approvals still required. Summarize `DONE`, `IN_PROGRESS`, `BLOCKED`, and next `TODO` tasks. Be explicit about what has not been verified. Never fabricate API behavior, test results, repository state, pushes, or deployment success.


## 20. CodeGraph MCP and code-intelligence workflow

The owner uses CodeGraph CLI/MCP with OpenCode. The CodeGraph repository is `https://github.com/colbymchenry/codegraph`; the owner reported CodeGraph CLI v1.6.2 and ran its installer to configure OpenCode globally. This is a reported environment fact, not proof that the current OpenCode process has loaded MCP or that this project's index exists. Verify the live session and current CLI state before relying on them.

### 20.1 Discovery phase: verify; do not initialize yet

- Inspect the actual CodeGraph CLI version and supported commands using `codegraph --version` and `codegraph --help` where available.
- Inspect the status of the MCP connection from the current OpenCode session and/or `opencode mcp list`. Restart OpenCode if its process predates the MCP configuration change.
- Ask/inspect the actual tools exposed by the connected CodeGraph MCP; do not assume a tool name or capability that is not present.
- The current starter directory may contain only Markdown documents. In that case, CodeGraph has little meaningful source-code graph to index. Record `index: not yet initialized — deferred until initial source code exists` as expected, not as a project failure.
- Do not run `codegraph install`, change global agent configuration, or restart/modify other tools from inside this project workflow unless the integration is demonstrably missing and the owner explicitly approves such a configuration change. The owner already ran the installer.

### 20.2 Initialization timing

- After the owner approves the implementation plan and the first meaningful application source files (for example Go packages and frontend TypeScript modules) have been created as part of an approved task, prepare `.gitignore` first and include `.codegraph/` so local generated graph data is not committed.
- Check `codegraph status`. If this project has no index, run `codegraph init` from the repository root, then check `codegraph status` again. Follow the installed CLI's actual help if its version's syntax differs.
- Do not initialize/rebuild the graph during the documentation-only planning phase.
- Use the local index for this repository only. Never copy an index from a different project or assume a global index maps to the correct repository.

### 20.3 When to use CodeGraph

Use the actual available CodeGraph MCP tools (usually its natural-language exploration tool in the default integration) for non-trivial code tasks where relationships matter, especially:
- tracing a monitoring request through scheduler, provider adapter, normalized outcome, database transition, event creation, and notification delivery;
- locating all implementations/callers/tests before changing a Go interface or normalized model;
- impact analysis for database schema or migration changes;
- tracing frontend API use and affected UI workflows;
- finding test files likely affected by source changes.

Do not pay the context/tool-call cost for trivial isolated edits when ordinary direct inspection is sufficient. CodeGraph is a navigation and dependency aid, not a correctness oracle. Check key findings against source files, migrations, tests, and actual command/test results. Do not blindly trust a graph statement if source evidence disagrees.

### 20.4 Keeping the index healthy

- Prefer CodeGraph's documented automatic synchronization when available.
- If results appear stale, inspect `codegraph status`; use the installed version's `codegraph sync` command if needed.
- Reserve a full `codegraph index` rebuild for a real indexing problem or an explicitly justified case; explain the reason in the report.
- Never run `codegraph uninit` or delete `.codegraph/` just to troubleshoot without a clear reason and owner approval.
- Record whether CodeGraph was available/used in a task's verification notes when it materially informed a non-trivial change. Do not claim a graph query ran unless it actually did.
- Keep `.codegraph/` out of Git. Do not rely on it as the only durable project documentation or as a substitute for `docs/IMPLEMENTATION_PLAN.md`.

### 20.5 If CodeGraph is unavailable

Continue safely with source reading, language tooling, tests, and normal navigation. Report the exact limitation (MCP not loaded, index missing, CLI command failed, or tool not exposed) and the minimum next action. Do not block planning on CodeGraph, do not invent MCP tools, and do not claim the integration is working based only on the existence of configuration.
