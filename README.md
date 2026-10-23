# Neda

**Status: implementation in progress — M1.2 repository and authentication foundation.** The repository contains the approved planning documents, a bootable Go service foundation, and the repository/auth primitives. HTTP authentication endpoints, route protection, monitoring, providers, scheduling, notifications, and the dashboard are not implemented yet.

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

**M1.2 repository and authentication foundation**

- `internal/store`: transaction helper and repositories for settings, the single owner user, and sessions, with real-SQLite transaction, constraint, and foreign-key tests.
- Argon2id password hashing with encoded parameters; minimum 12 characters and a bounded maximum.
- `neda set-password`: interactive TTY-only setup, reads the password twice without echo, rejects piped input and password arguments, and revokes existing sessions.
- Session and CSRF primitives: 32-byte random tokens stored only as SHA-256 hashes, HttpOnly session and `neda_csrf` cookies, idle/absolute expiry, logout and password-change revocation.
- Origin/Referer validation, double-submit CSRF middleware, and in-memory per-client/global login rate limiting. HTTP auth endpoints and route protection remain for the API milestone.

## Not implemented yet

HTTP authentication endpoints and private-route enforcement, monitor CRUD, provider adapters, the fake provider, the scheduler, availability episodes/events, Telegram, Web Push, the dashboard, Docker packaging, and deployment. Nothing in this repository authorizes live provider traffic or deployment.

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

`.env.example` lists placeholders for settings validated by the loader but reserved for later milestones (workers, VAPID, and the fake provider). The HTTP endpoints that consume the session settings are pending `API-001`/`API-005`. Do not commit real secrets.

## Repository layout

```text
cmd/neda/             CLI: serve, set-password, healthcheck, version
internal/config/      environment loading and validation
internal/logging/     structured logging and redaction
internal/db/          SQLite open/PRAGMAs, migration runner, readiness
internal/migrations/  embedded forward-only SQL migrations
internal/store/       transaction helper and core repositories
internal/auth/        password, session, CSRF, origin, and rate-limit primitives
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
- Do not commit passwords, tokens, cookies, private keys, encryption keys, Push subscription material, production environment files, or private runtime data.
