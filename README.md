# Neda

**Status: planning stage — documentation only. Application implementation has not started.**

Neda is a planned single-user, self-hosted train-ticket availability monitor for Iranian train routes. It will monitor configured routes, departure dates, passenger counts, and selected providers in the background, then send actionable Telegram and browser Web Push notifications when availability is confirmed. It is not a booking engine: the owner completes any purchase on the provider's site.

## Current repository state

- This repository currently contains planning documentation only.
- There is no application source code, database, frontend build, runtime configuration, or deployment asset yet.
- No credentials, tokens, private keys, or subscription secrets are stored here.
- Implementation is not authorized by this README; the approval gates and task status live in the implementation plan.

## Planned architecture (not implemented)

- Go backend with a persistent background monitoring scheduler.
- SQLite for authoritative state, migrations, events, deliveries, and provider health.
- React, TypeScript, Vite, Tailwind CSS, and shadcn/ui dashboard.
- Telegram and real browser Web Push notification channels.
- Docker Compose deployment design for a low-resource host, subject to a separate deployment approval.

These are planning decisions, not implemented functionality.

## Documentation

- [`docs/BRIEF.md`](docs/BRIEF.md) — approved product and engineering brief; the authoritative requirement source.
- [`docs/PRODUCT_SPEC.md`](docs/PRODUCT_SPEC.md) — product behavior, assumptions, and acceptance criteria.
- [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) — architecture, schema, scheduler/state design, security, testing, and deployment design.
- [`docs/IMPLEMENTATION_PLAN.md`](docs/IMPLEMENTATION_PLAN.md) — single source of truth for tasks, gates, dependencies, and verification evidence.
- [`docs/CODEGRAPH_WORKFLOW.md`](docs/CODEGRAPH_WORKFLOW.md) — CodeGraph usage policy for later implementation.
- [`AGENTS.md`](AGENTS.md) — operating rules for agents working in this repository.

## Safety and security

- Do not commit passwords, tokens, cookies, private keys, encryption keys, Push subscription material, production environment files, or private runtime data.
- Runtime secrets must be supplied outside the repository when implementation and deployment are separately approved.
- No production server access, provider traffic, or deployment is authorized by this documentation-only repository.
