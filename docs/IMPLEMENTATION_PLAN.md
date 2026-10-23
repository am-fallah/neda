# Neda — Implementation Plan and Task Registry

**Status:** Planning baseline approved under `GATE-001`; `GATE-002` authorized M1.1 and `GATE-006` authorizes M1.2 (`FND-008`/`FND-009`) on `feat/m1.2-store-auth`. PR #1 is merged on `main`. `GATE-003` and `GATE-004` remain unauthorized.
**Date:** 2026-10-24
**Authoritative product source:** `docs/BRIEF.md`; behavior: `PRODUCT_SPEC.md`; design: `ARCHITECTURE.md`.
**Repository path:** `docs/IMPLEMENTATION_PLAN.md`

> **Single tracker contract:** This file is the only active task tracker. Do not create a second TODO/status file. Update this file when a task starts, becomes blocked, or completes, and reconcile it at every session/milestone end.

---

## 1. Status and priority legend

| Field | Values | Meaning |
|---|---|---|
| Priority | `P0` | Correctness, security, data integrity, or approval gate. |
| | `P1` | Required v1 capability. |
| | `P2` | Polish/optional improvement. |
| Status | `TODO` | Not started; dependencies may still be pending. |
| | `IN_PROGRESS` | Actively being worked. |
| | `BLOCKED` | Cannot proceed; blocker and unblock action must be stated. |
| | `DONE` | Acceptance criteria met and required checks actually passed. |
| | `DEFERRED` | Intentionally postponed; reason and approval if scope changes. |

**Rules**

- `DONE` requires evidence. A task is not done because code was written; the listed verification must have been run and passed (or explicitly marked not run with a reason, in which case it is not `DONE` for a required check).
- `BLOCKED` must name the blocker and who/what can unblock it.
- `DEFERRED` requires a reason and owner approval if it changes required scope.
- P0/P1 tasks cannot be silently dropped. Any scope change, security/data/cost change, or deferred mandatory requirement needs explicit owner approval.
- Test evidence must record exact commands and outcomes. Distinguish passed, failed, and not run.
- Do not claim a CodeGraph query, live provider request, repository push, or deployment unless it actually happened.

---

## 2. Current state (2026-10-23)

### 2.1 Workspace

| Item | Finding | Evidence |
|---|---|---|
| Working directory | `/Users/fallah/Desktop/Neda` | `pwd` |
| Existing files | `AGENTS.md`, `README.md`, `START_PROMPT.md`, `.gitignore`, `docs/*.md`, M1.1 Go source/tests under `cmd/` and `internal/`, CI workflow, `docs/.codegraph/` (generated empty index, ignored), `.codegraph/` (generated root index, ignored), `.DS_Store`; M1.2 adds `internal/store`, `internal/auth`, and `neda set-password` | `find . -type f`; `git ls-files`; see §2.4 |
| Git | Git repository; PR #1 merged into `main` at `f2252e5`; `feat/m1.2-store-auth` created from `origin/main`; working tree clean at M1.2 start; root `.gitignore` present | `git status -sb`; `git log --oneline -3`; `git ls-files` |
| Existing work | Approved planning docs plus merged M1.1 Go skeleton/tests; M1.2 repository/auth foundation in progress; PR #1 merged with green CI; no frontend or feature-level monitoring code | PR #1; `.github/workflows/ci.yml`; see §2.3 |
| Planning documents | Reviewed copies are committed in `docs/` at `6c4470f`; the superseded working copies remain outside the repository in `/Users/fallah/.opencode/plan/` and are not authoritative. | `git show --stat 6c4470f`; `git ls-files docs/` |
| OpenCode | 2.1.4 | `opencode --version` |
| Local timezone | `Asia/Tehran` (+0330) | `date`, `/etc/localtime` |

### 2.2 Toolchain

| Tool | Status | Version / note |
|---|---|---|
| Git | Available | 2.54.0 (Apple Git-157) |
| GitHub CLI | Available/authenticated | `gh` 2.102.0; account `am-fallah`; scopes include `repo`, `workflow`, `gist`, `read:org`; token was not recorded. |
| Go | Available | `go1.27.3` (Homebrew 1.27.3, arm64); installed 2026-10-23 after GATE-002; pinned in `go.mod` as `go 1.27.3`. |
| Node.js | Available | v26.10.0 |
| npm | Available | 11.19.1 |
| pnpm | Available | 12.11.2 |
| Yarn | Not found | — |
| Docker | **Not installed** | `docker`/`docker compose` not found; no Colima/Podman/Lima. |
| Make | Available | GNU Make 3.81 |
| C compiler | Available | Apple clang 21.0.0 |
| SQLite CLI | Available | 3.54.0 |
| curl | Available | 8.7.1 |
| Homebrew | Available | 7.0.10 |
| OS/arch | macOS 27.0.1, arm64 | `sw_vers`, `uname -a` |

### 2.3 GitHub

- Authenticated account: `am-fallah`; scopes include `repo`, `workflow`, `gist`, `read:org`.
- Repository `am-fallah/neda` exists and is **private**; default branch `main`; `origin/main` is `f2252e57f8272cdc3fd41c105f6e24396b4f626c`.
- PR #1 (`M1.1: bootable service skeleton and schema foundation`) is **merged** (2026-10-23T20:50:10Z) by `am-fallah`; PR head `b87cf56`, merge commit `f2252e5`.
- CI: PR-head run [40021998627](https://github.com/am-fallah/neda/actions/runs/40021998627) passed; `main` push run [40022275486](https://github.com/am-fallah/neda/actions/runs/40022275486) passed.
- The initial documentation commit contains exactly the nine approved paths; `.gitignore` is in history.
- No token, cookie, key, or credential value was printed or stored.

### 2.4 CodeGraph

| Check | Result |
|---|---|
| CLI | `codegraph 1.6.2` at `/Users/fallah/.local/bin/codegraph` |
| `codegraph --help` | Supported commands observed: `init`, `uninit`, `index`, `sync`, `status`, `query`, `explore`, `context`, `node`, `files`, `callers`, `callees`, `impact`, `affected`, `install`, `uninstall`, `telemetry`, `upgrade`, `daemon`, `unlock`, `version`, `help` |
| Root `codegraph status` | Project `/Users/fallah/Desktop/Neda` — **initialized** after M1.1 Go source; 17 files, 281 nodes, 611 edges; index reported up to date |
| Nested index | `docs/.codegraph/codegraph.db` exists; `codegraph status docs` reports an initialized **empty** index (0 files/nodes/edges, 0.16 MB); `docs/.codegraph/.gitignore` exists |
| Root `.gitignore` | **Present**; includes `.codegraph/` and `**/.codegraph/`; `git check-ignore` confirms root and nested graph data are ignored |
| `opencode mcp list` | `codegraph` connected; `shadcn` connected |
| MCP tool actually exposed | CodeGraph **explore** only (harness name `codegraph_codegraph_explore`; documented as `codegraph_explore`). No assumption of other MCP tools. |
| Index action | `codegraph init` run at the root after M1.1 Go source existed; `codegraph status` verified. The nested `docs/.codegraph` was not removed or moved. |
| Query action | MCP explore traced `cmd/neda` serve startup through config, db, and httpapi; 37 symbols returned. Findings checked against source/tests. |

### 2.5 Verification not performed

- No application build/test/lint/race/benchmark was run before M1.1 (no source).
- No live MrBilit request was made (planning phase forbids it; provider semantics unverified).
- No Docker build/compose validation (Docker absent).
- No server access, SSH, deployment, or production inspection.
- No secrets inspected or recorded.

---

## 3. Approval gates

These gates are tracked here, not duplicated as implementation tasks in §5.

| Gate | Scope | Status | Depends on | Acceptance / evidence | What approval authorizes / does **not** authorize |
|---|---|---|---|---|---|
| `GATE-001` | Plan approval | DONE | PLAN-001..004 | Owner confirmed completion at implementation start (2026-10-23). Repository `am-fallah/neda` is private; `origin/main` is `6c4470f`; the initial commit manifest is exactly the nine approved paths; `.gitignore` is in history; working tree clean; no generated graph data tracked. The original independent re-review/preflight transcripts are not present in the repository; this status records the owner's confirmation and the verified outcome, not a reconstructed review. | Authorized only repo creation/docs push. |
| `GATE-002` | Implementation start | DONE | REPO-003 | Explicit owner approval recorded 2026-10-23: tracker reconciliation from verified evidence, `brew install go` with exact-version recording, and M1.1 implementation on `feat/m1-service-skeleton` with a PR to `main`, small conventional commits, explicit-path staging, and staged-diff/secret reviews. | Authorizes code/tests/local toolchain for M1.1. Does not authorize live MrBilit calls or deployment. |
| `GATE-003` | Bounded MrBilit live validation | TODO | GATE-002 | Explicit owner approval + target route/date + terms confirmation; dedicated one-process run with no retries/redirects, at most five read-only HTTP GET attempts total, and each request starting at least five seconds after the previous response completes. | Authorizes only that bounded protocol. Does not authorize auth bypass, parallel probing, load testing, scraping, following redirects, or retries. |
| `GATE-004` | Deployment | TODO | OPS-005, DISC-005 | Explicit owner approval of the reviewed deployment plan; plan covers server, proxy/TLS, key/volume permissions, backups, rollback. | Authorizes only the approved deployment plan. No automatic approval from prior gates. |
| `GATE-005` | Scope/security/data change (standing) | TODO | — | Explicit owner approval recorded when such a change is proposed. | Authorizes only the approved change; prohibits silent deviation from the brief. |
| `GATE-006` | M1.2 implementation | DONE | GATE-002, FND-007 | Explicit owner approval recorded 2026-10-24 for `FND-008`/`FND-009` on `feat/m1.2-store-auth`, with the narrow store scope, auth primitives/middleware only (no HTTP auth endpoints), CSRF option A (separate HttpOnly `neda_csrf` cookie; hash only in DB), and the proposed security defaults. | Authorizes code/tests/CLI for M1.2 and a PR to `main`. Does not authorize HTTP auth endpoints, live MrBilit calls, server access, or deployment. |

**Current stop point:** `GATE-006` approved for M1.2 only. `GATE-003` (live provider validation) and `GATE-004` (deployment) remain blocked and unauthorized.

---

## 4. Milestones and exit criteria

| Milestone | Goal | Exit criteria |
|---|---|---|
| `M0` Discovery & planning | Environment facts, product/architecture/plan docs | Docs reviewed; `GATE-001` approved; no application code, Git repository, or remote yet. |
| `M1` Foundation | Private repo and initial documentation, Go module, config/logging, SQLite/migrations, auth skeleton, Go CI, CodeGraph initialized after Go source | `REPO-001..003` complete: repo private; initial manifest exactly the nine paths in `GATE-001`/`REPO-003` (`.gitignore`, `AGENTS.md`, `START_PROMPT.md`, `README.md`, `docs/BRIEF.md`, `docs/CODEGRAPH_WORKFLOW.md`, `docs/PRODUCT_SPEC.md`, `docs/ARCHITECTURE.md`, `docs/IMPLEMENTATION_PLAN.md`) committed and pushed; `.gitignore` in history; `git diff --cached --name-only` exact and `git status --porcelain` inspected; `go build/test/vet` pass; auth/CSRF tests; migrations/FK checks; `FND-011` CodeGraph initialized/verified after Go source; root and nested `.codegraph/` ignored; no graph data committed; frontend CI is a later task |
| `M2` Provider engine | Normalized provider interface/registry, fake provider, MrBilit adapter, bounded validation, fixtures | Provider unit tests; known negative sample; no error→NotReservable; fake provider proves independence; live validation evidence or explicit unverified status. |
| `M3` Scheduler & state | Persistent monitors, bounded scheduler, restart recovery, outbox schema/enqueue, observations, episodes, events, dedupe, health, expiry, running/outcome check lifecycle, atomic per-check outcome | Scheduler/state tests; `NTF-000` outbox enqueue; running start row/crash/startup-abandon tests; `SCH-010` post-I/O validity-gate (including booking slug) and success/empty, provider-failure, discarded, abandoned branch tests; conditional final-outcome test; restart dedupe; multi-provider isolation; Tehran expiry/date-edit recovery; provider health transition table/transactions (A-21 owner option recorded before DONE). |
| `M4` Notifications | Delivery worker, Telegram sender, retries, channel independence, provider-health messages, read model | Outbox worker retry/crash tests; M3 event+outbox atomicity tests pass; Telegram test/delivery with encrypted settings; no secret leakage; `NTF-006` read model. |
| `M5` API & dashboard | Monitor CRUD/detail, settings, events/history, provider health, Jalali input, responsive UI, frontend CI | API/auth tests; frontend component tests; `FND-013` frontend CI; build; manual CRUD/status acceptance. |
| `M6` Web Push & security hardening | Real Push/service worker, VAPID persistence, CSRF/rate/CSP/secret/Push validation | Push test with browser closed; invalid-subscription cleanup; security tests; secret audit. |
| `M7` Production readiness | Full checks, Docker/Compose, docs, backup/restore, resource measurement, deployment plan | All required checks pass or explicitly not run; Docker build verified; DoD audit; `GATE-004` requested. |

---

## 5. Task registry

`GATE-*` entries in dependency columns refer to the approval gates in §3 and are stable gate IDs, not implementation tasks. `M0`–`M7` refer to the milestones in §4. `Owner input` marks an external dependency that only the owner can unblock. All other dependencies are explicit task IDs in this section; do not use slash shorthand or implicit ranges beyond the documented `..` notation.

### 5.1 Discovery and planning

| ID | Pri | Status | Depends on | Task and acceptance criteria | Verification / evidence |
|---|---|---|---|---|---|
| `DISC-001` | P0 | DONE | — | Inspect working directory, files, Git state, existing work; preserve all existing files. AC: environment facts documented without modification. | `pwd`; `find`; `git rev-parse`/`git status` (fatal = no repo); file list. Evidence in §2.1. |
| `DISC-002` | P0 | DONE | — | Verify GitHub CLI auth and whether `neda` exists under the authenticated account; do not print tokens. AC: account/scopes/repo absence documented safely. | `gh --version`; `gh auth status` (masked); `gh api user`; `gh repo view am-fallah/neda`; `gh repo list` filter. Evidence §2.3. |
| `DISC-003` | P0 | DONE | — | Verify CodeGraph CLI version, root/nested project index status, MCP connection, and actual exposed tools. AC: no assumptions from config alone; root-not-initialized and nested-empty-index facts recorded; indexing deferred until meaningful source exists. | `codegraph --version`; `codegraph --help`; `codegraph status`; `codegraph status docs`; `opencode mcp list`; session tool catalog. Evidence §2.4. |
| `DISC-004` | P0 | DONE | — | Inspect Go/Node/package manager/Docker/build tool availability and versions. AC: exact versions/absence documented. | `git --version`; `go version`; `node --version`; `npm/pnpm --version`; `docker --version`; `docker compose version`; `make/gcc/sqlite3` versions. Evidence §2.2. |
| `DISC-005` | P0 | BLOCKED | Owner input | Record minimum production server facts without accessing the server. AC: OS/arch, CPU/RAM/disk, Docker/Compose, ports, proxy, domain/HTTPS, outbound access, backup target documented. **Blocker:** owner has not supplied facts and server access is forbidden in planning. **Unblock:** owner supplies facts or approves a later read-only inspection under a deployment plan. | Owner-supplied values only; no SSH/server commands. |
| `PLAN-001` | P0 | DONE | DISC-001..004 | Produce product specification preserving all mandatory requirements and classifying facts. AC: scope, workflows, availability, pause, timezone, providers, notifications, security, acceptance criteria, assumptions. | Working copy `PRODUCT_SPEC.md` in the planning workspace; reviewed against brief. Repository copy pending GATE-001. |
| `PLAN-002` | P0 | DONE | DISC-001..004 | Produce architecture: repo tree, schema/migrations, provider interface/registry, availability model, durable state/dedupe, health alerts, notification flow, security, testing, deployment, resource budget. AC: no invented server/provider facts; classifications. | Working copy `ARCHITECTURE.md` in the planning workspace; reviewed against brief. |
| `PLAN-003` | P0 | DONE | PLAN-001, PLAN-002 | Produce single implementation tracker with stable IDs, priorities, statuses, deps, acceptance, verification, gates. AC: no second tracker. | This file. |
| `PLAN-004` | P0 | DONE | PLAN-001..003 | Record assumptions/risks/owner inputs. AC: VERIFIED/PROVISIONAL/REQUIRES OWNER INPUT classification; no hidden assumptions. | Assumption tables in product/architecture; risks §9 here. |

### 5.2 M1 — Foundation

| ID | Pri | Status | Depends on | Task and acceptance criteria | Verification / evidence |
|---|---|---|---|---|---|
| `REPO-001` | P0 | DONE | GATE-001 | Immediately before any repository creation, preflight: re-run `gh auth status`; re-check `gh repo view am-fallah/neda`; verify the authenticated account and repository visibility; if `neda` already exists, stop and ask the owner before reusing, replacing, or modifying it; record the outcome. Then create the private GitHub repository `neda`, initialize Git in the project directory, and configure the remote. Do **not** commit docs yet. AC: preflight recorded; repo private; Git initialized; no history overwrite; no docs/generated data committed. | `gh auth status` (masked); `gh repo view am-fallah/neda`; `gh repo view` visibility; `git status`; `git remote -v`. No GitHub/network action in planning. |
| `REPO-002` | P0 | DONE | REPO-001 | Create the root `.gitignore` **before the first documentation commit**; include `.env*` (except example), DB/WAL/SHM, keys, `web/node_modules`, `web/dist` (except `.gitkeep`), `.codegraph/`, `**/.codegraph/`, logs, `.DS_Store`, build artifacts. The file may remain unstaged here; `REPO-003` explicitly stages and commits it with the initial documentation manifest. AC: root ignore exists; root and nested `docs/.codegraph/` are ignored; no docs/generated data staged; check-ignore passes. | `git check-ignore -v .codegraph/ docs/.codegraph/ .env data/neda.db web/node_modules web/dist`; `git status`. |
| `REPO-003` | P0 | DONE | REPO-002 | Copy the reviewed `PRODUCT_SPEC.md`, `ARCHITECTURE.md`, and `IMPLEMENTATION_PLAN.md` from the planning workspace into `docs/` (they are not yet present in the repository); prepare/replace the repository README with an English planning-state README; review that README against the actual files before committing. `REPO-002` provides `.gitignore`; the other manifest files already exist in the starter repository, and the README is replaced by this task. **Do not use `git add .`, `git add -A`, or `git add --all`; stage only the nine paths below explicitly by path.** The README must explicitly state that application implementation is not yet present; must not invent commands, environment-variable names/values, unimplemented features, misleading setup instructions, or configuration placeholders presented as supported settings; must distinguish planned from implemented functionality; and must contain no secrets. Stage only this explicit initial manifest by path: `.gitignore`, `AGENTS.md`, `START_PROMPT.md`, `README.md`, `docs/BRIEF.md`, `docs/CODEGRAPH_WORKFLOW.md`, `docs/PRODUCT_SPEC.md`, `docs/ARCHITECTURE.md`, `docs/IMPLEMENTATION_PLAN.md`. Do not stage `.DS_Store`, `docs/.codegraph/`, database/WAL/SHM files, credentials, or generated index artifacts. Before committing, require `git diff --cached --name-only` to list exactly the nine paths and inspect `git status --porcelain` for unexpected staged or untracked artifacts; stop and correct any mismatch. Run a secret scan/staged-diff review, commit descriptively, and push to the verified private remote. AC: manifest exactly as listed; broad staging commands not used; `git diff --cached --name-only` exact; `git status --porcelain` inspected; `.gitignore` in history; English planning-state README reviewed against actual files and makes no implementation claim; no secrets/generated data; remote private; push successful; no force-push/history overwrite. | README review; `git diff --cached --name-only` (must equal the nine paths); `git status --porcelain` (unexpected artifacts inspected); `git ls-files`; `git log`; `gh repo view`; `git check-ignore -v docs/.codegraph/`. |
| `FND-001` | P0 | DONE | GATE-002 | Install/verify Go and pin toolchain. Homebrew stable `1.27.3` was verified and installed; exact version recorded in `go.mod`/CI. AC: selected version builds the module. | `brew info go` → stable 1.27.3; `brew install go`; `go version` → `go1.27.3 darwin/arm64`; `go env GOVERSION` → `go1.27.3`; `go mod init github.com/am-fallah/neda`; `go build ./...` passed. |
| `FND-002` | P2 | TODO | GATE-002 | Optional local Docker/Compose runtime. AC: `docker version`/`docker compose version` work; if not installed, CI Docker build is the fallback and this task remains P2/not run. | `docker version`; `docker compose version`; `docker run hello-world` only if approved. |
| `FND-003` | P0 | DONE | FND-001, GATE-002 | Scaffold repository tree, `go.mod`, `cmd/neda`, package skeletons, Makefile, `.dockerignore`, `.env.example` (placeholders only); preserve/update the root `.gitignore` created by `REPO-002` without weakening `.codegraph/`, `**/.codegraph/`, or secret/database rules. AC: tree matches architecture; no secrets; build succeeds. | `go build ./...` passed; `gofmt -l .` empty; `go test ./...` passed; `go vet ./...` passed; secret scan clean; `git check-ignore -v .codegraph/ docs/.codegraph/codegraph.db` confirms generated graph data remains ignored. Implemented packages are `cmd/neda`, `internal/config`, `internal/logging`, `internal/db`, `internal/migrations`, `internal/httpapi`; no empty future packages. |
| `FND-004` | P0 | DONE | FND-003 | Config loader/validation (env, paths, key file, public URL, TTLs, worker bounds, fake-provider flag). AC: fail-closed missing/invalid config; tests. | `go test ./internal/config/...` passed; production missing/invalid key/public URL cases; key mode/length/base64 tests; URL/worker/TTL/boolean validation tests. |
| `FND-005` | P0 | DONE | FND-003 | Structured logging, request IDs, redaction helpers. AC: JSON logs; no tokens/cookies/endpoints/bodies; redaction tests. | `go test ./internal/logging/...` passed; JSON/text handlers; attribute/URL/bearer/query redaction; request-ID context tests. |
| `FND-006` | P0 | DONE | FND-003..005 | HTTP server skeleton: timeouts, body limits, middleware, `/healthz`, `/readyz`, graceful shutdown, 404/405, panic recovery; explicit Host-header behavior per architecture §14.5. AC: tests and local curl. | `go test ./internal/httpapi/...` and `go test -race ./internal/httpapi/...` passed; `go test ./cmd/neda/...` passed; local production-mode run verified `/healthz` and `/readyz` 200, wrong Host 421, `neda healthcheck` ok, SIGTERM graceful stop; log redaction/query omission tests. |
| `FND-007` | P0 | DONE | FND-003 | SQLite open/PRAGMAs/migration runner/embedded baseline migrations (`0001`–`0003`); `NTF-000` adds the notifications migration. AC: WAL/FK/busy timeout; `synchronous=FULL` per §7.1 and §6.1; effective `PRAGMA synchronous` is `FULL` (2) on initial and replacement connections; `check_runs` running/finished CHECK and enum/boolean/counter constraints per Architecture §7.2; idempotent migrations; checksum mismatch rejected; failing migration rolls back; `foreign_key_check`/`integrity_check` clean. | `go test ./internal/db/...` passed; `go test -race ./internal/db/...` passed; initial/replacement PRAGMA tests; checksum/rollback/newer-database tests; constraint matrix; FK/integrity checks; `modernc.org/sqlite v1.61.0`. |
| `FND-008` | P0 | DONE | FND-007 | Repository layer and transaction helper for core tables. AC: CRUD + tx tests; no SQL string concatenation; FK behavior verified. | `go test ./internal/store/...` and `go test -race ./internal/store/...` passed; transaction commit/rollback/panic/canceled-context/nested tests; settings CRUD; single-row user CRUD and optimistic password update; session create/lookup/touch/revoke/revoke-all; FK rejection/cascade and `foreign_key_check` clean; SQL-concatenation scan empty; `go vet ./...` passed. |
| `FND-009` | P0 | IN_PROGRESS | FND-007, FND-008 | Auth foundation: users/sessions, Argon2id, `neda set-password` TTY-only, session middleware, CSRF for authenticated routes, login Origin/Referer + rate limiting. AC: no default password; no password args; cookie flags; login CSRF policy matches architecture §14.2/§14.7; tests. | `go test ./internal/auth/...`; manual `neda set-password`; inspect cookie flags via `httptest`. |
| `FND-010` | P1 | DONE | FND-003 | Go CI workflow: gofmt/vet/test/race/build; no secrets/deploy; pinned Go version. AC: Go CI passes on a clean branch; frontend CI is separate. | GitHub Actions run [40019219808](https://github.com/am-fallah/neda/actions/runs/40019219808) passed in 2m27s on PR #1; latest head run [40019673683](https://github.com/am-fallah/neda/actions/runs/40019673683) passed in 39s; local `make check`/`make race` also passed. |
| `FND-011` | P0 | DONE | FND-003 | Initialize CodeGraph only after meaningful Go source exists. Ensure root `.gitignore` has `.codegraph/` and `**/.codegraph/`; `codegraph status`; `codegraph init` at root; `codegraph status`; use explore for one non-trivial trace; record. AC: graph initialized; root and nested graph data ignored; no graph data committed; existing `docs/.codegraph` not deleted/moved without owner approval. | `codegraph status` → not initialized; `codegraph init` → 17 files, 281 nodes, 611 edges; `codegraph status` → up to date; MCP `codegraph_codegraph_explore` traced serve startup (`main`/`runServe` → config/db/httpapi) and returned 37 symbols; `git check-ignore -v .codegraph/ docs/.codegraph/codegraph.db` passed; no graph data tracked. |
| `FND-012` | P1 | DONE | FND-003 | Update the English planning-state README as implementation starts; add only verified commands/configuration and distinguish implemented from planned functionality; do not add environment-variable names/values or setup details until they exist and are verified; final implementation-specific sections remain in `OPS-004`. AC: English-only; README reviewed against actual files; no unverified setup/details/features; no secrets; commands reflect reality. | README reviewed against implemented packages/commands; `make check`, `make race`, `make run`, health/ready curl, and `NEDA_ENV=development go run ./cmd/neda healthcheck` verified; planned features explicitly marked not implemented; `.env.example` placeholders identified as reserved where applicable; no secrets. |

### 5.3 M2 — Provider engine

| ID | Pri | Status | Depends on | Task and acceptance criteria | Verification / evidence |
|---|---|---|---|---|---|
| `PRV-001` | P0 | TODO | FND-003, FND-008 | Normalized provider types, interface, error taxonomy, explicit registry. AC: core has no MrBilit-specific JSON/business rules; registry rejects nil/duplicate; unit tests. | `go test ./internal/provider/...`; import-boundary check (`rg 'mrbilit' internal/availability internal/scheduler`). |
| `PRV-002` | P0 | TODO | PRV-001 | Deterministic fake/test provider with different route-ID requirements and availability rule; never presented as real. AC: tests prove engine independence; fake disabled by default; UI labels test-only when enabled. | `go test ./internal/provider/fake/...`; config test `NEDA_FAKE_PROVIDER_ENABLED=false`; UI copy review. |
| `PRV-003` | P0 | TODO | PRV-001 | MrBilit request builder and bounded HTTP client. AC: exact endpoint/param names; Gregorian date; optional params omitted until validated; **honors the single 20s context deadline created by `SCH-004`** and owns only the 6s per-attempt timeout, bounded retry policy, `Retry-After` interpretation, redirect rejection, and retry/backoff behavior **within that caller-supplied deadline**; must not create a competing overall 20s timer; 2 MiB response and journey/offering caps; no cookies/secrets; httptest coverage. Does not own scheduler target backoff or per-provider spacing. | `go test ./internal/provider/mrbilit/...`; request URL assertions; timeout/redirect/oversize/retry/cap tests; slow retry stops at caller deadline. |
| `PRV-004` | P0 | TODO | PRV-003 | MrBilit response decoding, optional-field handling, journey/offering identity, sanitized warnings. AC: unexpected types become Unknown/error; exact normalized identity contract per architecture §9.3; caps enforced before allocation; stable keys; no raw storage; fixtures labeled observed/synthetic. | `go test ./internal/provider/mrbilit/...`; identity fixture tests; cap/overflow tests; fixture inventory; no positive observed fixture unless captured. |
| `PRV-005` | P0 | TODO | PRV-004 | Availability decision table and table-driven tests. AC: known negative sample; positive only with sufficient capacity + both flags; capacity shortfall Unknown; missing/null/unexpected/contradictions Unknown; error never NotReservable; empty list no closure; oversized/duplicate/conflicting results fail closed. | `go test ./internal/provider/mrbilit/... -run TestClassify`; case table review; test labels. |
| `PRV-006` | P0 | TODO | GATE-003, PRV-003 | Bounded live validation using a dedicated one-process client: one owner-approved future route/date; read-only GET only; no parallel requests; no automatic retries; redirects disabled; any redirect stops; at most five read-only HTTP GET attempts total; next request starts ≥5s after previous response completes; booking page excluded unless explicitly budgeted; metadata logged without credentials/sensitive content; update semantics only from evidence. | Timestamped request log; attempt count/timing audit; sanitized fixture/metadata; provider semantics memo; no live test in CI. |
| `PRV-007` | P1 | TODO | PRV-004 | Booking URL and Jalali date format validation. AC: only validated slug/pattern; no derivation from city names; nil if unverified; URL scheme/host checks; tests. Owner-provided example is required for final real-world validation, but optional-slug implementation/tests can proceed. | `go test ./internal/provider/mrbilit/... -run TestBookingURL`; owner-verified fixture; negative slug/date tests. |
| `PRV-008` | P1 | TODO | PRV-001 | Evaluate/select maintained Go Jalali library; wrapper + known date tests. AC: no manual algorithm; round trips/leap/boundaries; document library/version. | `go test ./internal/calendar/...`; `go list -m`; known fixture table (brief example provisional). |
| `PRV-009` | P1 | TODO | PRV-001, PRV-002 | Provider config validation and multi-provider registry integration. AC: per-provider IDs validated; duplicate provider rejected; fake/MrBilit differences covered. | `go test ./internal/provider/...`; API validation tests later. |
| `PRV-010` | P0 | TODO | PRV-001 | Provider error taxonomy and shared infrastructure/config/route/cancel classification contract used by health. AC: stable categories; no state change on error; provider-package tests; health consumes the contract without redefining it. | `go test ./internal/provider/...`; category table tests; import-boundary review. |

### 5.4 M3 — Scheduler, state, health

| ID | Pri | Status | Depends on | Task and acceptance criteria | Verification / evidence |
|---|---|---|---|---|---|
| `SCH-001` | P0 | TODO | FND-008, PRV-001 | Monitors/monitor_providers schema, repositories, CRUD service, validation, config-version bump rules, active/inactive provider rows. AC: create/edit/pause/resume/delete; provider removal deactivates without deleting history; material-change reset; a departure-date edit recomputes `expires_at`, clears `expired_at` when the new expiry is future, leaves a paused monitor paused, and schedules only active/enabled/non-expired targets near-term after commit; material edit closes old episodes and publishes the scheduler wake signal after commit (wake-channel wiring is `SCH-002`); label-only changes do not reset scheduling; booking-slug-only changes do not bump `config_version` or reset state and are handled by the `SCH-010` gate; no state loss on label edit; tests. | `go test ./internal/store/... ./internal/domain/...`; migration tests; material-edit reschedule test; expired → edit future → resume; paused-across-expiry test. |
| `SCH-002` | P0 | TODO | SCH-001, PRV-002 | Scheduler loop: due selection, in-flight guard, bounded workers, jitter, wake channel (including material config edits), restart stagger, no busy polling. **Current proposed interpretation (not approved):** guard is per monitor/provider pair (see `PRODUCT_SPEC.md` A-33); monitor-wide serialization is an open owner decision requiring a separate update. AC: no overlap per current assumption; bounds; restart recovery; fake clock tests. | `go test ./internal/scheduler/...`; race test; goroutine count check optional. |
| `SCH-003` | P0 | TODO | SCH-002, PRV-010 | Scheduler-level per-provider concurrency/spacing and target-level backoff. It consumes the `PRV-010` typed outcome/backoff hint and does not implement a second HTTP retry, redirect, or timeout engine. AC: concurrency 1 per provider; minimum spacing; target backoff after an applicable failure; config/route/4xx normal interval; no hammering; no HTTP timeout/redirect ownership; tests. | `go test ./internal/scheduler/... -run TestBackoff`; target-backoff tests; no duplicate HTTP timeout/redirect tests. |
| `SCH-004` | P0 | TODO | SCH-002, PRV-001 | Provider-check stage: (a) open and commit exactly one attempt-start transaction inserting `check_runs(outcome='running', config_version, started_at)` before I/O, writing only `check_runs`; (b) load config, call the registered `Provider` through the `PRV-001` interface, consume a normalized result/typed error/cancellation, retain the target ID, request `config_version`, and request booking slug for `SCH-010`’s post-I/O gate; (c) create the **single overall 20s context deadline** and pass it to `Check`, without implementing a second retry or HTTP timeout engine. It does not open or commit the outcome transaction; `SCH-010` owns the outcome commit and gate. AC: start row is `running` and contains only check-run fields; no availability/target/health/outbox writes in the start transaction; outcomes distinct; caller cancellation vs deadline vs provider-side cancellation mapped per Architecture §10.3; slow retry/backoff stops at 20s and is classified as timeout, not abandoned; target/version/booking slug retained for gate; tests use the fake provider, so `PRV-003` is not required at this stage (MrBilit integration is covered in M2 and `SCH-009`). | `go test ./internal/scheduler/...`; temp DB; fake provider; start-row/deadline/cancellation tests. |
| `NTF-000` | P0 | TODO | FND-007, FND-008 | Notification/event schema migration + atomic enqueue and minimal dedupe read-back: `0004_notifications.sql` (provider-health tables including `last_duration_ms`, deliveries, attempts, Push subscriptions); insert event+delivery rows in the caller’s transaction; unique targets. It does not own the API read model. AC: schema applies; check constraints for enums/booleans/counters per Architecture §7.2; enqueue atomic; duplicate enqueue no-op; rollback leaves no rows; dedupe read-back only. | `go test ./internal/db/... ./internal/notify/...`; transaction rollback/fault injection; `PRAGMA foreign_key_check`; constraint tests. |
| `SCH-005` | P0 | TODO | SCH-004, FND-007, NTF-000 | Availability state machine/episodes/events/dedupe applier. AC: exposes `Apply(tx, ...)`, never begins/commits; initial Reservable; Not→Res; continuous no dup; Res→Not close; Unknown/error/empty/missing/discarded/abandoned no close; later new episode; restart dedupe; conflicting keys Unknown; event+outbox rows committed atomically when called by `SCH-010`. | `go test ./internal/availability/...`; transaction rollback/replay; unique constraint tests; outbox row assertions. |
| `SCH-006` | P0 | TODO | SCH-004, FND-007, NTF-000, PRV-010 | Provider-health counters, degraded episode, dedupe, recovery, stale status, and diagnostics. AC: `ApplySuccess(tx, ...)` and failure-transition applier accept the caller transaction and never begin/commit; implement the transition table in Architecture §12 exactly for provider and target counters, `last_attempt`/`last_error`/`last_duration_ms`, and availability/outbox effects; applicable success/empty (including valid empty) counts as health success/recovery; applicable infrastructure failures increment; config/route/ordinary 4xx, cancellation/abandoned, and discarded outcomes update or leave counters as specified and do not create health events; health counter+episode+event+outbox commit inside the `SCH-010` outcome transaction (with availability state on applicable success; without availability writes on provider failure); **A-21 is unresolved: Option A is only the provisional test default; the task cannot be `DONE` until the owner records A/B/C**; deterministic fault-injection test. | `go test ./internal/health/...`; transition-table matrix including interleaved non-counting outcomes; rollback/fault injection; restart persistence; `last_duration_ms`/last-attempt assertions. |
| `SCH-007` | P0 | TODO | SCH-001, FND-007, SCH-005 | Pause/resume/expiry with Tehran end-of-day; a wake/timer is scheduled for the next expiry so jobs expire even when their next poll would be later; an expiry sweep includes paused/disabled monitors for `expired_at`/episode reconciliation but never polls them; expired resume rejected until a material future date edit recomputes `expires_at` and clears `expired_at`; no polling; open episodes closed on expiry. AC: expired → edit future → resume succeeds; paused-across-expiry → edit future → resume; status/scheduling use reconciled values; tests under `TZ=UTC` and another zone with fixed clock. | `TZ=UTC go test ./internal/scheduler/... ./internal/calendar/...`; fixed-clock expired/paused recovery tests. |
| `SCH-008` | P0 | TODO | SCH-004, SCH-005, SCH-006, SCH-007, PRV-002 | Multi-provider independence: one fails, another succeeds; independent states/events; no cross-provider merge/suppression. **Current proposed interpretation (not approved):** providers may run concurrently per monitor/provider pair (see `PRODUCT_SPEC.md` A-33); monitor-wide serialization is an open owner decision requiring a separate update. AC: targeted tests across check pipeline, health, and expiry. | `go test ./internal/scheduler/... -run TestMultiProvider`; event/outbox count assertions. |
| `SCH-010` | P0 | TODO | SCH-004, SCH-005, SCH-006, NTF-000 | Per-check outcome orchestrator with a mandatory post-I/O validity gate and four mutually exclusive final outcomes. `SCH-004` has already committed the `running` check-run row. **Validity gate:** after provider I/O returns a result, typed error, or cancellation, re-load the target and verify row exists, `active = 1`, monitor enabled, not expired (`expires_at > now`, `expired_at` null), `config_version` equals the request version, and the request booking slug equals current `booking_slug`; re-evaluate at the start of the outcome transaction before any write. If invalid/booking-changed, take discarded (precedence over cancellation); if caller cancellation/shutdown and target valid, take abandoned; deadline expiry is a timeout failure, not abandoned. **Success/empty (valid target):** one outcome transaction transitions `running` → `success`/`empty`, sets `finished_at`/counts/duration, and covers target timing, `SCH-005.Apply`, `SCH-006.ApplySuccess`, and `NTF-000` event/outbox rows; commit, then wake. **Provider failure (valid target):** one outcome transaction transitions `running` → `error`, sets `finished_at`, updates target timing, and calls `SCH-006` failure transition only for an applicable infrastructure failure; health event/outbox only at threshold; never `SCH-005.Apply`; no availability state/episode/event changes. **Discarded (invalid/removed/config or booking changed):** transition the existing `running` row to `outcome='discarded'` with the request’s `config_version`, set `finished_at`; no target timing/scheduling, availability, health, or events; no worker wake. **Abandoned (caller cancellation/shutdown):** best-effort transition the running row to `abandoned` and set `finished_at` if target/DB available, otherwise leave it `running` for startup; no target timing/scheduling, availability, health, or events; no worker wake. Use conditional `WHERE id=? AND outcome='running'`; zero rows means rollback and no duplicate final outcome. Any applier/DB error rolls back the outcome transaction; no external delivery before commit. | Validity-gate matrix for all result types including booking slug; start/outcome transaction boundaries; crash after start and during outcome; startup abandons running rows; conditional update/no double final outcome; success/rollback; provider-failure no-availability-change; config/route/4xx health behavior; discarded/abandoned no target/health changes; old-config worker cannot overwrite; health-threshold atomic/rollback; restart/replay; race. |
| `SCH-009` | P0 | TODO | SCH-002..008, SCH-010, NTF-000 | End-to-end scheduler/state integration with fake provider, temp SQLite, restart/replay. AC: (1) applicable successful check including valid empty commits check result + availability + health success/recovery + applicable events/outbox atomically; (2) injected failure during success rolls all writes back and leaves the running row for startup abandonment; (3) applicable infrastructure error (timeout/network/429/5xx/invalid response) records failed check/target diagnostics and health transition while availability state/episodes remain unchanged; (4) config/route/ordinary 4xx records target diagnostics and follows the selected A-21 provider-counter rule without creating an unintended health event/outbox; (5) threshold health transition and its event/outbox commit/rollback atomically; (6) discarded and abandoned outcomes — including a successful result or provider error/timeout that completes after pause/expiry/config/booking change — cause no availability/health transitions/events and no target scheduling updates, finalizing only the running `check_runs` row; (7) caller cancellation/shutdown is abandoned, deadline/`ErrTimeout` is a timeout failure, and provider-side cancellation is not abandoned; (8) no external delivery before commit; (9) material edit schedules the new config near-term and an old-config worker cannot overwrite target scheduling/health; (10) attempt start writes exactly one `running` row before I/O; crash after start leaves it running, startup marks it abandoned, and no target/availability/health/outbox changes occur; (11) crash during the outcome transaction rolls back, leaves the running row, and startup abandons it without duplicate event/outbox rows; (12) slow retries/backoff stop at the 20s overall deadline and are classified as timeout, not abandoned; (13) booking-slug edit during an in-flight request discards that attempt, creates no event with the old URL, and the next check uses the new slug; (14) expired → edit future date → resume and paused-across-expiry → edit future → resume behave consistently; (15) provider/target health counters, `last_attempt`/`last_error`/`last_duration_ms`, and interleaved non-counting outcomes match the documented transition table. | `go test ./internal/...`; race; DB integrity check; branch/validity-gate matrix; crash/restart harness; fake clock/timezone tests. |

### 5.5 M4 — Notifications

| ID | Pri | Status | Depends on | Task and acceptance criteria | Verification / evidence |
|---|---|---|---|---|---|
| `NTF-001` | P0 | TODO | NTF-000, SCH-005, SCH-006 | Delivery claim/lease/retry worker only; it must not create schema or enqueue events. AC: claims committed rows; no duplicate target; crash reclaim; bounded attempts; event/outbox atomicity is owned by `NTF-000`/`SCH-005`/`SCH-006`; tests. | `go test ./internal/notify/...`; transaction fault injection; reopen DB. |
| `NTF-002` | P1 | TODO | NTF-001 | Notification content formatting for availability/health/recovery; HTML escaping; redaction; validated URLs only. AC: required fields; distinct labels; no secrets; tests. | `go test ./internal/notify/... -run TestFormat`; golden strings; redaction cases. |
| `NTF-003` | P0 | TODO | NTF-001, FND-009 | Telegram client/settings/test/delivery. AC: encrypted token/chat ID; never returned/logged; getMe/send; transient vs permanent; test endpoint; httptest. | `go test ./internal/notify/... ./internal/auth/...`; manual test after settings UI; log review. |
| `NTF-004` | P1 | TODO | NTF-001..003 | Channel independence and expiry skip with fake channel senders; real Push sender integration remains `PSH-004`. AC: Telegram failure does not block a fake Push attempt and vice versa; expired availability delivery skipped; health delivery unaffected. | `go test ./internal/notify/... -run TestChannelIndependence`; fake senders. |
| `NTF-005` | P1 | TODO | NTF-000, NTF-001, FND-007 | Retention/cleanup worker for check runs, attempts, events/deliveries, sessions, closed episodes, disabled subscriptions. AC: defaults enforced; deterministic FK-safe order per architecture §7.5; no open episode pruned; tests. | `go test ./internal/retention/...`; temp DB row counts; `PRAGMA foreign_key_check`. |
| `NTF-006` | P1 | TODO | NTF-000 | Notification/event/delivery read-model repository only: queries for detected vs delivered, per-channel status, pagination. No HTTP endpoints. AC: read model tests; `API-003` owns endpoints and depends on this task. | `go test ./internal/store/...`; query/read-model tests; no duplicate HTTP API acceptance. |

### 5.6 M5 — API and dashboard

| ID | Pri | Status | Depends on | Task and acceptance criteria | Verification / evidence |
|---|---|---|---|---|---|
| `API-001` | P0 | TODO | FND-006, FND-009 | Router/middleware/error model/auth protection/CSRF/body limits/security headers; explicit Host-header behavior. AC: private routes 401; authenticated CSRF 403; login uses Origin/Referer + rate limit, not session CSRF; no CORS; headers; Host mismatch 421; tests. | `go test ./internal/httpapi/...`; `httptest` matrix; Host header tests. |
| `API-002` | P0 | TODO | SCH-001, API-001 | Monitor CRUD/pause/resume/delete DTOs and validation. AC: create/edit/pause/resume/delete; expired resume 409; field errors; tests. | `go test ./internal/httpapi/... -run TestMonitors`; API examples. |
| `API-003` | P1 | TODO | API-001, SCH-004..006, NTF-000, NTF-006 | Providers/health/events/deliveries/overview endpoints; cursor pagination; filters; use `NTF-006` read model. AC: correct data; no secrets; no duplicate notification API ownership; tests. | `go test ./internal/httpapi/...`; pagination/read-model tests. |
| `API-004` | P1 | TODO | API-001, NTF-003 | Settings + Telegram config endpoints; secret set/clear; never return secrets. Push config/public-key/test endpoints are owned by `PSH-006`. AC: token absent from all responses/logs; tests. | `go test ./internal/httpapi/...`; response snapshots; log scan. |
| `API-005` | P0 | TODO | FND-009, API-001 | Auth endpoints: login/session/logout/password; login rate limit + same-origin Origin/Referer; authenticated CSRF; setup-required response. AC: tests match architecture §14.2/§14.7. | `go test ./internal/auth/... ./internal/httpapi/...`; cookie/CSRF/origin assertions. |
| `API-006` | P2 | TODO | API-001..005, API-008 | Optional minimal OpenAPI documentation (hand-written; no codegen). AC: endpoints/status/error models documented; validator if available. | `docs/openapi.yaml` review; optional lint. |
| `API-007` | P1 | TODO | API-001..005, API-008 | API integration tests with `httptest` + temp SQLite + fake provider. AC: auth, CRUD, detail/diagnostics, events, settings; Push endpoint tests are owned by `PSH-006`; no live provider. | `go test ./internal/httpapi/...`; race. |
| `WEB-001` | P1 | TODO | FND-003 | Scaffold React/TS/Vite/Tailwind/shadcn, router, theme, layout, `components.json`, scripts. AC: dev/build/lint work. | `cd web && npm ci && npm run build && npm run lint`; visual smoke. |
| `WEB-002` | P1 | TODO | WEB-001, API-001 | Typed API client, auth/session, CSRF header, error normalization, 401 redirect. AC: tests with mocked fetch. | `npm test`; typecheck. |
| `WEB-003` | P1 | TODO | WEB-001, API-005 | Login UI + setup-required guidance + logout. AC: form/errors/rate-limit message; tests. | `npm test`; manual login. |
| `WEB-004` | P1 | TODO | WEB-001 | Jalali date picker + Tehran date utilities. AC: conversion/leap/boundary/min-today tests; Gregorian API value. | `npm test`; manual date selection. |
| `WEB-005` | P1 | TODO | WEB-001..004, API-002, API-008 | Monitor list/detail/create/edit/pause/resume/delete UI. AC: validation, provider IDs, status distinction, current observations/diagnostics, delete confirmation; tests. | `npm test`; manual CRUD. |
| `WEB-006` | P1 | TODO | WEB-001, API-003 | Overview/events/notification status/provider health UI. AC: filters, booking link, unknown vs no-tickets distinction; tests. | `npm test`; manual review. |
| `WEB-007` | P1 | TODO | WEB-001, API-004 | Settings UI: Telegram configured/test, polling interval, password change. Push setup is owned by `PSH-005`. AC: secrets never prefilled; feedback; tests. | `npm test`; manual settings. |
| `WEB-008` | P1 | TODO | WEB-001..007 | Responsive/accessibility/light-dark polish. AC: keyboard, labels, contrast, mobile; manual checklist. | Manual checklist; optional axe; build. |
| `WEB-009` | P1 | TODO | WEB-001, FND-003 | Embed frontend in Go; dev proxy; SPA fallback; `.gitkeep` build guard. AC: one binary serves SPA/API; assets cache correctly. | `npm run build`; `go build`; `curl` index/asset/API fallback. |
| `FND-013` | P1 | TODO | WEB-001 | Frontend CI workflow: npm ci/lint/typecheck/test/build; no deployment. AC: frontend CI passes after scaffold; separate from M1. | GitHub Actions run log; local `npm test`/build. |
| `API-008` | P1 | TODO | API-001, API-002, SCH-004, SCH-005 | Monitor detail/current-observations/check-diagnostics endpoints and DTOs. AC: `GET /monitors/{id}` includes current inventory views and recent check outcomes/errors; paused/expired/unknown/missing cases; no secrets; tests. | `go test ./internal/httpapi/... -run TestMonitorDetail`; fixture data; API examples. |

### 5.7 M6 — Web Push and security hardening

| ID | Pri | Status | Depends on | Task and acceptance criteria | Verification / evidence |
|---|---|---|---|---|---|
| `PSH-001` | P1 | TODO | FND-009, API-001 | VAPID key generation/persistence/encryption; stable across restart. AC: private key never returned/logged; public key available to the config endpoint owner; tests. | `go test ./internal/push/...`; restart and compare public key; log scan. |
| `PSH-002` | P1 | TODO | PSH-001, API-001, NTF-000 | Push subscription API: parse/validate the endpoint at subscription time (HTTPS-only, no userinfo/fragments, bounded length, exact allowlist, literal private/loopback/link-local/metadata IPv4/IPv6 rejection); validate `keys.p256dh`/`auth`; encrypt/store; deduplicate; disable/delete; 410 cleanup. Does not own send-time connection validation. AC: invalid-input/subscription/encryption tests. | `go test ./internal/push/... ./internal/httpapi/...`; negative URL/key matrix; schema/FK tests. |
| `PSH-003` | P1 | TODO | PSH-001, WEB-001 | Service worker: registration, push display, click handling, subscription change. AC: real browser test with server stopped/closed browser; no secrets in payload. | Manual browser checklist; SW unit/logic review; HTTPS/localhost. |
| `PSH-004` | P1 | TODO | PSH-001, NTF-001 | Server-side Push sender: library integration, TTL/urgency, retries, 404/410 cleanup; **send-time** DNS resolution/IP validation against the actually connected address, disabled redirects, per-send revalidation. AC: DNS-rebinding/redirect-to-private/IPv6 send-time tests; independent channel. | `go test ./internal/push/... ./internal/notify/...`; fake push endpoint/resolver; manual push. |
| `PSH-005` | P1 | TODO | PSH-002, PSH-006, WEB-007 | Push UI: permission, enable/disable, device list, test, unsubscribe. AC: states/feedback; tests. | `npm test`; manual browser test. |
| `PSH-006` | P1 | TODO | PSH-001, PSH-004, API-001 | Own the authenticated Web Push configuration/public-key and test endpoints (`GET /push/config`, `POST /push/test`); never expose private key/subscription material. AC: endpoint ownership is unique; tests; no duplicate API-004 acceptance. | `go test ./internal/httpapi/... ./internal/push/...`; response snapshots; log scan. |
| `SEC-001` | P0 | TODO | API-001..005, API-008, NTF-003, PSH-002, PSH-006 | Threat model/security review and findings. AC: checklist completed; unresolved risks documented. | `docs/SECURITY.md`/architecture checklist; review notes. |
| `SEC-002` | P0 | TODO | API-001 | CSP/headers/cookies/login CSRF policy/authenticated CSRF/rate-limit/host-header/SSRF/body-limit tests and fixes. AC: matrix passes; no CORS; no internals; Host mismatch 421. | `go test ./internal/httpapi/...`; header/CSRF/Host assertions; manual curl. |
| `SEC-003` | P0 | TODO | FND-009, NTF-003, PSH-001 | Encryption/key lifecycle/backup-restore tests. AC: tamper/wrong key fail safely; key file readability/mode/ownership checks; `/data` volume permissions; restart persistence; key-loss docs; restore drill. | `go test ./internal/crypto/...`; startup permission tests; manual backup/restore; no plaintext secrets. |
| `SEC-004` | P2 | TODO | M1..M6 | Dependency/vulnerability review (`govulncheck`, `npm audit`), pinning, licenses. AC: no unresolved critical/high; exceptions documented. | `govulncheck ./...`; `npm audit`; dependency list. |
| `SEC-005` | P1 | TODO | FND-005, NTF-003 | Log redaction audit across API/provider/Telegram/Push. AC: no tokens/endpoints/cookies/keys; tests. | Log capture tests; grep scan; manual review. |

### 5.8 M7 — Production readiness

| ID | Pri | Status | Depends on | Task and acceptance criteria | Verification / evidence |
|---|---|---|---|---|---|
| `OPS-001` | P0 | TODO | FND-003, WEB-009 | Multi-stage Dockerfile, non-root runtime, read-only root, `.dockerignore`, healthcheck, `/data` directory ownership for runtime UID. AC: image builds; runs as non-root; no secrets; empty volume writable. | `docker build` (local or CI); `docker run --read-only`; `id`/health; volume ownership check. |
| `OPS-002` | P0 | TODO | OPS-001 | Compose config: one replica, volumes, explicit key delivery (long-form secret with verified `mode`/`uid`/`gid`, or read-only owner-only `0400`/`0600` bind mount), restart, limits, logging, localhost/proxy port. AC: `docker compose config` valid; key readable by non-root without violating policy; `/data` ownership; restart preserves DB/key; no secrets committed. | `docker compose config`; startup with correct/wrong key mode; restart test; review. |
| `OPS-003` | P1 | TODO | OPS-001, OPS-002 | Docker build verification in CI or local. AC: image builds and healthcheck passes. | CI workflow log or local `docker build`/`compose up`. |
| `OPS-004` | P1 | TODO | M1..M6 | Full README in English: purpose/scope, core features, architecture, prerequisites, verified local dev/setup/run, configuration/environment variables, security/secret handling, initial password setup, Telegram setup, browser Push setup, Docker deployment, reverse proxy/HTTPS, backups/restore, testing/validation, troubleshooting, and deployment/operational boundaries. AC: English-only; implemented vs planned clearly distinguished; every command, environment variable, and feature verified against the real repository; no secrets or fabricated setup. | Document review; command/env-var validation; secret scan. |
| `OPS-005` | P0 | TODO | DISC-005, OPS-001, OPS-002, OPS-004 | Deployment plan for the existing Ubuntu host: ports/proxy/TLS/firewall/volumes/key ownership/backup/rollback/resource limits. AC: no server access before approval; plan reviewed. | `docs/DEPLOYMENT.md`; owner approval `GATE-004` follows this task. |
| `OPS-006` | P1 | TODO | M3, OPS-001 | Measure resource use with simulated monitors/fake provider: RSS/CPU/DB/WAL over ≥1h. AC: exact commands/results recorded; pass/fail vs targets. | `/usr/bin/time -l`, `ps`, `docker stats`; DB size; results in tracker. |
| `OPS-007` | P0 | TODO | M1..M6 | Full verification suite: gofmt/vet/test/race/build; frontend lint/typecheck/test/build; Docker build if available; manual checklist. AC: pass or explicit not-run reason; no live provider. | Exact commands in §8; record outputs. |
| `OPS-008` | P0 | TODO | OPS-007 | Definition-of-done audit and final report. AC: every DoD item mapped; limitations/approvals clear. | §10 checklist; final report. |
| `OPS-009` | P1 | TODO | OPS-001, OPS-002, SEC-003 | Backup/restore drill with DB + key on a fresh volume. AC: restored instance works; secrets decrypt; key ownership/mode preserved; `/data` writable; docs corrected. | Manual drill log; `PRAGMA integrity_check`; permission/restart checks. |
| `OPS-010` | P1 | TODO | OPS-001 | CI Docker build if local Docker unavailable. AC: image builds in CI; no deploy. | Actions log. |

---

## 6. Critical task details

### 6.1 FND-007 — SQLite and migrations

- Use embedded sorted SQL files; bootstrap `schema_migrations`; checksum applied files.
- Set `foreign_keys=ON`, WAL, busy timeout, `synchronous=FULL`, WAL cap. Single writer.
- Verify the effective `PRAGMA synchronous` is `FULL` (SQLite value `2`) on the initial connection and after opening a replacement/new connection; per-connection PRAGMAs must be reapplied automatically.
- Each migration in a transaction; fail startup on error/checksum mismatch.
- Tests: empty DB → all migrations; re-run no-op; checksum mismatch; failing migration rollback; effective PRAGMA checks on replacement connections; `foreign_key_check`; `integrity_check`.
- No raw provider responses or secrets in DB.

### 6.2 FND-009 — Authentication foundation

- `neda set-password` requires a TTY; reads twice without echo; never accepts a password argument; no public bootstrap.
- Argon2id params benchmarked; tests may use lower cost params via injection.
- Sessions: 32-byte token, SHA-256 at rest, HttpOnly/Secure/SameSite, idle/absolute expiry, revoke.
- CSRF per session; Origin check; login rate limiting; generic errors; constant-time comparisons.
- Tests cover missing password, wrong password, expiry, CSRF, logout, password change revocation.

### 6.3 PRV-005 — Availability decision table

- Implement exactly the table in `ARCHITECTURE.md` §9.4; keep reason codes stable.
- The known negative sample is mandatory: `capacity=0`, `isAvailable=false`, `reservationAvailable=true` → NotReservable.
- Positive requires numeric capacity ≥ conservative required count **and** both flags true. This is provisional; do not claim real-world verification without a captured positive sample.
- Capacity below requested → Unknown (not NotReservable) until semantics validated.
- Errors/invalid envelopes → error; never a state.
- Empty list → successful empty; no closure.
- Tests label synthetic vs observed. No fabricated observed positive fixture.

### 6.4 PRV-006 — Bounded live validation

- Requires `GATE-003` and a provider-terms/permission check.
- One process; one owner-approved future route/date; no parallel requests.
- No automatic retries; redirects disabled (`CheckRedirect` error / `--max-redirs 0`); any redirect stops the run.
- At most five read-only HTTP GET attempts in total; every attempt counts; the next request starts at least five seconds after the previous response completes.
- Booking-page fetch is excluded unless explicitly included in the approved five-request budget; otherwise use an owner-supplied example.
- Log timestamp, method, host/path, status, duration, response size, and sanitized shape metadata; no cookies, credentials, auth headers, or sensitive bodies.
- Stop on blocking/rate-limit/terms signals (e.g. 403/429/451); do not retry.
- If blocked, stop and document; do not make live access a test requirement.
- Update semantics/docs only from evidence; retain unverified labels.

### 6.5 SCH-005 — State machine and dedupe

- State key: `monitor_provider_id × config_version × journey_key × offering_key`.
- Episode sequence + partial unique open-episode index.
- Event dedupe key `availability:episode:<id>` unique.
- Expose `Apply(tx *sql.Tx, ...)`; never begin or commit a transaction. `SCH-010` owns the transaction; `NTF-000` enqueues event+delivery rows inside it.
- Initial Reservable event; continuous no dup; decisive close; later new event; unknown/error/empty/missing no close.
- Restart/replay test: reopen DB, replay same result, event and outbox row counts unchanged.
- Conflicting duplicate keys → Unknown, no event.
- Config change closes old episodes with `config_changed`, no event.

### 6.6 SCH-006 — Provider health

- Implement the transition table in `ARCHITECTURE.md` §12 exactly. Provider-level `consecutive_failures`/episode and target-level `consecutive_failures` are separate.
- Option A (infrastructure-only, current provisional default): applicable success/empty resets both counters; applicable infrastructure failures increment; config/route/ordinary 4xx leave counters unchanged but update last-attempt/error/duration diagnostics; discarded/abandoned update neither provider nor target.
- Option B (brief-literal) and Option C (strict consecutive) are documented alternatives. **A-21 is unresolved:** `SCH-006` cannot be `DONE` until the owner records the selected option and its false-positive/negative consequences; do not present Option A as approved.
- `provider_health.last_attempt_at` and `last_duration_ms` update for every applicable completed outcome; `last_success_at` updates on success/empty; `last_failure_at` and `last_error_kind` update on applicable errors. Discarded/abandoned do not update them. `provider_health` is upserted on first applicable outcome.
- Expose `ApplySuccess(tx *sql.Tx, ...)` and the failure-transition applier; never begin or commit. `SCH-010` owns the outcome transaction.
- Health counter + episode open/close + event + outbox enqueue commit within the `SCH-010` outcome transaction: on applicable success together with availability state; on applicable infrastructure failure in the failure transaction that performs no availability writes. No adjacent-transaction option.
- Deterministic fault-injection/rollback test proves no partial health/event/outbox state.
- Health state persists; stale derived in API.
- Messages distinct from ticket availability; provider-side cancellation is not abandoned and follows the Architecture §10.3 classification.

### 6.7 NTF-000 / NTF-001 — Outbox boundaries

- `NTF-000`: schema migration + atomic enqueue and minimal dedupe read-back. It owns event/delivery inserts and unique targets; callers provide the surrounding transaction. API read-model queries belong solely to `NTF-006`.
- `NTF-001`: delivery claim/lease/retry worker only. It never creates schema or enqueues events.
- Event + delivery rows in one transaction via `NTF-000`; unique `(event_id, channel, target_key)`.
- One delivery per Telegram target; one per active Push subscription.
- Claim with lease; send outside DB transaction; bounded retries; permanent vs transient.
- Crash recovery reclaims expired leases; attempt history; no exactly-once claim.
- Expired availability delivery skipped; health delivery unaffected.
- Channel independence tests.

### 6.8 SEC-002 / SEC-003 — Security proof

- Header matrix: CSP/HSTS(prod)/nosniff/referrer/permissions/cache; no CORS.
- Login uses same-origin Origin/Referer + rate limit; authenticated CSRF missing/mismatch → 403; Origin mismatch → 403.
- Host-header match/mismatch/missing/proxy cases; mismatch returns 421.
- Session cookie flags; expiry/revocation; rate limit.
- AES-GCM round trip; tamper/wrong key fail; AAD binding; key file readability/mode/ownership; `/data` volume permissions.
- Push endpoint validation: HTTPS-only exact allowlist, userinfo/fragment/oversize, private/loopback/link-local/metadata IPv4/IPv6, DNS rebinding, redirect-to-private, 410 cleanup.
- Secret redaction tests for API/logs/errors.
- No production access.

### 6.9 OPS-007 — Required checks

Run and record exact results:

```bash
gofmt -l .
go vet ./...
go test ./...
go test -race ./...
go build ./...
cd web && npm ci && npm run lint && npm run typecheck && npm test && npm run build
docker build -t neda:local .          # only if Docker is available or in CI
docker compose config                  # only if Docker/Compose is available
```

Optional: `govulncheck ./...`, `npm audit`, `staticcheck ./...`. Never run live provider tests in CI. Report not-run checks honestly.

### 6.10 SCH-010 — Per-check transaction orchestrator

- `SCH-004` owns one short attempt-start transaction: insert `check_runs(outcome='running', config_version, started_at)` before provider I/O, writing only `check_runs`; then perform provider I/O outside any database transaction. It does not open the outcome transaction.
- `SCH-010` is the only production path that opens and commits the per-check **outcome transaction**. It transitions the existing running row and owns exactly one of the four final outcomes below. The start and outcome transactions are distinct; the outcome transaction covers completion plus availability/health/outbox as applicable.
- **Mandatory post-I/O validity gate:** after I/O returns and before selecting success or provider failure, re-load the target and verify the row exists, `active = 1`, the monitor is enabled, the monitor is not expired (`expires_at > now`, `expired_at` null), `config_version` equals the version used for the request, and the request booking slug equals the current `booking_slug`. Re-evaluate the same conditions at the start of the outcome transaction before any write. The gate applies regardless of whether the provider returned a result, an error, or a cancellation. Target invalidity/booking mismatch takes precedence over cancellation. A deadline is a timeout failure, not abandoned.
- If the target is invalid/removed/config- or booking-changed, take the **discarded** outcome (precedence over cancellation); if the target is valid but caller cancellation/shutdown is in progress, take the **abandoned** outcome. Only an applicable result may take the success/empty or provider-failure path.
- **Success/empty (valid target):** one outcome transaction transitions `running` → `success`/`empty`, sets `finished_at`/counts/duration, and covers target timing, `SCH-005.Apply(tx, ...)`, `SCH-006.ApplySuccess(tx, ...)`, and `NTF-000` event/outbox rows. Commit first; only then wake the worker.
- **Provider failure (valid target):** one outcome transaction transitions `running` → `error`, sets `finished_at`, updates target timing, and calls `SCH-006` failure applier only for an applicable infrastructure failure; health event/outbox rows only if the threshold is crossed. It must not call `SCH-005.Apply`, change availability state, create availability events, or close episodes.
- **Discarded (invalid/removed/config or booking changed):** transition the existing running row to `outcome='discarded'`, set `finished_at`, retain the request’s `config_version`; do not update `next_due_at`, `last_attempt_at`, `last_outcome`, failure counters, availability, episodes, or provider health; do not create events/outbox. If deletion cascaded the target and running row away, no diagnostic row can exist. No worker wake.
- **Abandoned (caller cancellation/shutdown):** best-effort transition the running row to `outcome='abandoned'` and set `finished_at` if the target row and DB remain available; otherwise leave it running for startup recovery. No target scheduling/timing, availability, health, events, or outbox. No worker wake.
- Use `UPDATE ... WHERE id = ? AND outcome = 'running'`; zero rows means no final outcome is applied. Any applier or database failure rolls back the outcome transaction; the running row remains for startup abandonment. No state, health, event, outbox, target, or final check-run partial write remains.
- Event/outbox rows commit with the state changes that produced them. No external delivery starts before commit. A crash after commit but before waking is recovered by the outbox worker’s due-row polling; replay is idempotent. Startup changes remaining running rows to abandoned with no other writes.
- `SCH-009` tests the validity gate (including booking slug), start/outcome boundaries, all four outcomes, crash/restart recovery, conditional update, rollback cases, and the old-config-worker case described in its acceptance criteria.

---

## 7. Verification evidence log (append-only)

| Date | Task | Status change | Evidence |
|---|---|---|---|
| 2026-10-23 | `DISC-001` | TODO → DONE | `pwd` `/Users/fallah/Desktop/Neda`; file tree docs-only; `git rev-parse`/`git status` fatal (no repo). **Superseded in part:** `docs/.codegraph` now exists; see CORR-001. |
| 2026-10-23 | `DISC-002` | TODO → DONE | `gh` 2.102.0; authenticated `am-fallah`; scopes `repo`, `workflow`, `gist`, `read:org`; `gh repo view am-fallah/neda` not found; no token printed. |
| 2026-10-23 | `DISC-003` | TODO → DONE | `codegraph 1.6.2`; root `codegraph status` not initialized; `.codegraph/` absent at time of original inspection; `opencode mcp list` codegraph connected; session exposes explore tool only. **Superseded:** nested `docs/.codegraph` empty index found later; see CORR-001. |
| 2026-10-23 | `DISC-004` | TODO → DONE | Git 2.54.0; Go absent; Node 26.10.0; npm 11.19.1; pnpm 12.11.2; Docker absent; Make 3.81; clang 21; sqlite3 3.54.0; curl 8.7.1. |
| 2026-10-23 | `DISC-005` | TODO → BLOCKED | No server facts supplied; server access forbidden in planning. Owner input required. |
| 2026-10-23 | `PLAN-001` | TODO → DONE | Working copy created at `/Users/fallah/.opencode/plan/PRODUCT_SPEC.md`. |
| 2026-10-23 | `PLAN-002` | TODO → DONE | Working copy created at `/Users/fallah/.opencode/plan/ARCHITECTURE.md`. |
| 2026-10-23 | `PLAN-003` | TODO → DONE | This tracker created at `/Users/fallah/.opencode/plan/IMPLEMENTATION_PLAN.md`. |
| 2026-10-23 | `PLAN-004` | TODO → DONE | Assumptions/risks/owner inputs recorded in docs. |
| 2026-10-23 | `GATE-001` | TODO → IN_PROGRESS | Plan presented; awaiting explicit owner approval. No repo/code/deploy action taken. |
| 2026-10-23 | `GATE-001` | IN_PROGRESS → DONE | Owner confirmed completion at implementation start. Verified `gh repo view am-fallah/neda` → private, default `main`; `git status -sb` → clean `main...origin/main`; `git log -1` → `6c4470f`; `git show --name-only 6c4470f` → exactly the nine approved paths; `.gitignore` includes `.codegraph/` and `**/.codegraph/`. Original re-review/preflight transcripts are not in the repository; status records owner confirmation and verified outcome. |
| 2026-10-23 | `REPO-001` | TODO → DONE | Repository exists, is private, default branch `main`, remote configured; current state verified read-only. Original creation preflight transcript is not preserved in the repository. |
| 2026-10-23 | `REPO-002` | TODO → DONE | Root `.gitignore` committed in `6c4470f`; `git check-ignore -v .codegraph/ docs/.codegraph/codegraph.db .env data/neda.db` confirms ignore rules. |
| 2026-10-23 | `REPO-003` | TODO → DONE | Initial commit `6c4470f` contains exactly nine paths (`.gitignore`, `AGENTS.md`, `README.md`, `START_PROMPT.md`, and the five `docs/*.md` files); tracked tree contains no graph data or secrets; remote `origin/main` matches. |
| 2026-10-23 | `GATE-002` | TODO → DONE | Owner explicitly approved implementation start, Go installation, tracker reconciliation, and M1.1 on `feat/m1-service-skeleton` with PR to `main`. |
| 2026-10-23 | `FND-001` | BLOCKED → IN_PROGRESS | GATE-002 approved; installing Go and verifying the exact version. |
| 2026-10-23 | `FND-001` | IN_PROGRESS → DONE | `brew info go` → stable 1.27.3; `brew install go` succeeded; `go version` → `go1.27.3 darwin/arm64`; `go env GOVERSION` → `go1.27.3`; `go mod init github.com/am-fallah/neda`; `go build ./...` passed. |
| 2026-10-23 | `FND-003` | TODO → IN_PROGRESS | `go.mod`, `cmd/neda` version/help skeleton, `Makefile`, `.dockerignore`, `.env.example` created; `go build ./...`, `go test ./...`, `go vet ./...`, `gofmt -l .` passed. Package skeletons continue with FND-004/005/007/006. |
| 2026-10-23 | `FND-004` | TODO → DONE | `internal/config` implemented; `go test ./internal/config/...` passed: production defaults/required key+URL, development defaults, key mode/length/base64, URL/query/userinfo/path rejection, workers/TTL/boolean/log validation. |
| 2026-10-23 | `FND-005` | TODO → DONE | `internal/logging` implemented; `go test ./internal/logging/...` passed: JSON/text handlers, level filtering, token/authorization/URL/body redaction, request ID context. |
| 2026-10-23 | `FND-007` | TODO → DONE | Embedded migrations `0001`–`0003` and SQLite runner implemented with `modernc.org/sqlite v1.61.0`; `go test ./internal/db/...` and `go test -race ./internal/db/...` passed. Tests cover WAL/FK/busy timeout/`synchronous=2` on initial and replacement connections, idempotent migration, checksum mismatch, failing-migration rollback, newer-database rejection, enum/boolean/counter/`check_runs` constraints, `foreign_key_check`, and `integrity_check`. |
| 2026-10-23 | `FND-003` | IN_PROGRESS → DONE | `go build ./...`, `go test ./...`, `go vet ./...`, and `gofmt -l .` passed. Implemented package tree: `cmd/neda`, `internal/config`, `internal/logging`, `internal/db`, `internal/migrations`, `internal/httpapi`; no empty future packages. Secret scan clean; graph data remains ignored. |
| 2026-10-23 | `FND-006` | TODO → DONE | HTTP server with request IDs, JSON errors, security headers, Host validation (421), body limit, panic recovery, DB-backed `/readyz`, and graceful shutdown. `go test ./internal/httpapi/...`, `go test -race ./internal/httpapi/...`, and `go test ./cmd/neda/...` passed. Local production-mode run: `/healthz` 200, `/readyz` 200, wrong Host 421, `neda healthcheck` → `ok`, SIGTERM → clean stop. |
| 2026-10-23 | `FND-010` | TODO → IN_PROGRESS | `.github/workflows/ci.yml` added with formatting, tidy, vet, test, race, and build steps. Local `make check`, `make race`, and `go mod tidy` diff check passed; GitHub Actions run pending PR. |
| 2026-10-23 | `FND-011` | TODO → DONE | `codegraph status` → not initialized; `codegraph init` → 17 files, 281 nodes, 611 edges; `codegraph status` → index up to date; MCP explore returned the serve startup path and 37 symbols; `git check-ignore -v .codegraph/ docs/.codegraph/codegraph.db` confirms both ignored; no graph data staged. |
| 2026-10-23 | `FND-012` | TODO → DONE | README updated to implementation-in-progress status with implemented/planned split. Verified `make check`, `make race`, `make run`, `/healthz`, `/readyz`, and development healthcheck; all commands and configuration listed match the implemented skeleton; no secrets. |
| 2026-10-23 | `FND-010` | IN_PROGRESS → DONE | PR #1 opened; GitHub Actions run [40019219808](https://github.com/am-fallah/neda/actions/runs/40019219808) passed the Go checks job (2m27s). Latest head run [40019673683](https://github.com/am-fallah/neda/actions/runs/40019673683) passed in 39s. Local `make check`/`make race` also passed. |
| 2026-10-24 | `GATE-006` | TODO → DONE | Owner explicitly approved M1.2: narrow `FND-008` store scope, `FND-009` primitives/middleware/CLI only (no HTTP auth endpoints), CSRF option A, proposed security defaults, branch `feat/m1.2-store-auth`, PR #2 without merge. `GATE-003`/`GATE-004` remain unauthorized. |
| 2026-10-24 | `FND-008` | TODO → IN_PROGRESS | Verified PR #1 merged (`f2252e5`), `main` CI run [40022275486](https://github.com/am-fallah/neda/actions/runs/40022275486) passed, and local tree clean at `origin/main`. Created `feat/m1.2-store-auth`; implementing transaction helper plus settings/users/sessions repositories only. |
| 2026-10-24 | `FND-008` | IN_PROGRESS → DONE | `internal/store` added with `WithTx` and settings/users/sessions repositories. `go test ./internal/store/...` passed (1.405s); `go test -race ./internal/store/...` passed (1.714s); `go vet ./...` passed. Tests cover commit/rollback/panic/canceled context/nested rejection, settings CRUD and validation, single-row user upsert/optimistic password update, session create/lookup/touch/revoke/revoke-all/revoke-other, FK rejection/cascade, and clean `foreign_key_check`. SQL-concatenation scan returned no matches; all queries are constants using placeholders. |

**Not run in M1.1:** Docker build/compose validation (Docker not installed), live MrBilit/Telegram/Push requests, server inspection, deployment. No claim of success for these.

### 7.1 Correction history (document-only, 2026-10-23)

| ID | Change | Evidence |
|---|---|---|
| `CORR-001` | Corrected CodeGraph/repository-state claims: root not initialized; empty `docs/.codegraph` index exists (0 files/nodes, 0.16 MB); no root `.gitignore`; nested index not removed/moved; `**/.codegraph/` required before first docs commit. | `codegraph status`; `codegraph status docs`; `find docs/.codegraph`; `ls .gitignore`; review report REV-003. |
| `CORR-002` | Split outbox ownership: added `NTF-000` (schema + atomic enqueue/read-back) before `SCH-005`/`SCH-006`; narrowed `NTF-001` to claim/lease/retry worker; added dependencies and atomicity acceptance criteria. API read model was later assigned solely to `NTF-006` by CORR-006. | Document review; no code/tests run. |
| `CORR-003` | Required provider-health counter, episode, event, and outbox to commit in one transaction; removed “same or an adjacent transaction”; added deterministic fault-injection requirement. | Document review; no tests run. |
| `CORR-004` | Added explicit key delivery/permission options, `synchronous=FULL` provisional policy, Push DNS/IP/redirect SSRF controls, login CSRF policy, host-header behavior, identity canonicalization, result caps, retention order, and dependency/ownership repairs. | Document review; no code/tests run. |
| `CORR-005` | No task status was changed; no gate was approved; no implementation or live validation occurred. `GATE-001` remains unapproved pending review (the currently pending review is the independent final review; see CORR-006/CORR-007). | Inspection only. |
| `CORR-006` | Targeted correction pass for NF-001–NF-006: aligned `synchronous=FULL` and effective-PRAGMA checks; added `SCH-010` per-check atomic orchestrator and combined fault test; restricted `NTF-000` to enqueue/dedupe read-back; added `NTF-000` to `PSH-002`; split subscription-time vs send-time Push validation; corrected Product A-01 and the architecture tree wording. No task status changed; no gate approved; independent final review pending. | Document review; no code/tests; searches, dependency parser, table/fence checks. |
| `CORR-007` | Correction pass for NF-002-A, NF-002-B, NF-007: defined the three mutually exclusive `SCH-010` branches (success/empty, provider failure, discarded) in the task row and §6.10; expanded `SCH-009` to six branch/rollback/delivery-order acceptance tests; updated the `M3` exit criteria; replaced the §10.2 sequence diagram and outcome-branch text with the SCH-010-owned three-branch flow; removed temporary plan-directory/Plan-mode header metadata from all three documents; replaced stale review-status wording in §9 and CORR-005; added the English README/documentation requirement to Product §3.11, Architecture §5, `REPO-003`, `FND-012`, `OPS-004`, the README risk row, and `GATE-001` criteria. No task status changed; no gate approved; independent final review not performed by this pass. | Document review; no code/tests; header and stale-wording searches; dependency parser; table/fence checks. |
| `CORR-008` | Final documentation correction pass for GATE-001 findings F-01–F-09 and F-11: added the mandatory post-I/O target-validity gate and four outcomes (success/empty, provider failure, discarded, abandoned); defined discarded/abandoned write sets and material-edit near-term rescheduling; aligned provider-health eligibility and `SCH-009` test criteria; aligned README scope/timing and the explicit initial-commit manifest (including `.gitignore`); recorded the monitor-concurrency owner decision as Product A-33; normalized dependency notation and moved `REPO-001..003` into M1; assigned HTTP retry/timeout/redirect behavior to `PRV-003` and scheduler-level backoff/spacing to `SCH-003`; added the `REPO-001` preflight. F-10 historical paths were preserved. No task status changed; no gate approved; independent re-review pending. | Read-only document inspection; dependency/table/fence checks; no code/tests/Git/GitHub/provider/CodeGraph actions. |
| `CORR-009` | Targeted documentation-only correction pass for independent-review findings R-01–R-08 and L-01 (plus safe L-03/L-04 clarifications). Added the explicit `check_runs` running→final lifecycle and start/outcome transaction split; added the provider-health transition table and A-21 option analysis; added booking-slug gate and expiry-date-edit recovery rules; made broad staging explicitly prohibited and the nine-path manifest exact; added `SCH-003 → PRV-010` and clarified `SCH-004`’s fake-provider test boundary; made `SCH-004` the sole 20s deadline owner; added review traceability and working-copy notes; added `FND-011` to M1 exit criteria; added enum/boolean/counter constraints and cancellation/timeout mapping. No task status changed; no gate approved; no implementation performed. | Read-only document inspection; local text edits; table/dependency checks; no code/tests/Git/GitHub/provider/CodeGraph/server actions. |

### 7.2 Review traceability (post-CORR-009)

**Preserved evidence:** the `CORR-001`–`CORR-009` rows and the path references recorded in them are preserved. **Reconstructed mapping:** the original `REV-003` report and the exact F-finding wording are not present in the repository. The F mapping below is reconstructed from the `CORR-008` summary and checked against the current document text; it is not a verbatim preservation of the prior review. `F-11` is explicitly **unmapped**: no original finding text exists, so no pass is claimed. The R mapping records the corrections made in this pass; it is not an independent verification or approval.

Working-copy note: these three documents are the plan-directory working copies at `/Users/fallah/.opencode/plan/`. They are not yet in `docs/`; `REPO-003` copies them only after `GATE-001`. Do not copy them before approval, and do not describe a `docs/` review until the copies are made and checked.

| ID | Preserved/reconstructed basis | Correction location | Correction-pass status |
|---|---|---|---|
| `F-01` | CORR-008 summary; original wording absent | Product §4.3/§4.10; Architecture §10.2/§10.3; `SCH-010`, `SCH-009` | Addressed; independent review not performed by this pass |
| `F-02` | CORR-008 summary; original wording absent | Product §4.3/§4.8; Architecture §10.2/§11.5; `SCH-001`, `SCH-010` | Addressed; independent review not performed by this pass |
| `F-03` | CORR-008 summary; original wording absent | Product §4.4; Architecture §12; `SCH-006`, `SCH-009` | Partially addressed by documentation; A-21 owner decision remains |
| `F-04` | CORR-008 summary; original wording absent | Product §3.11; Architecture §5; `REPO-003`, `FND-012`, `OPS-004` | Addressed |
| `F-05` | CORR-008 summary; original wording absent | `GATE-001`, `REPO-003`, M1 | Addressed by CORR-009 |
| `F-06` | CORR-008 summary; original wording absent | Product A-33/§3.2/§7; Architecture §10.1; `SCH-002`, `SCH-008` | Documentation addressed; A-33 owner decision remains |
| `F-07` | CORR-008 summary; original wording absent | Plan §5/§5.2 | Addressed; dependency check repeated |
| `F-08` | CORR-008 summary; original wording absent | Architecture §9.1; `PRV-003`, `SCH-003`, `SCH-004` | Addressed by CORR-009 |
| `F-09` | CORR-008 summary; original wording absent | `REPO-001` preflight | Addressed |
| `F-10` | CORR-008 says historical paths preserved; `REV-003` has no file/path | Plan correction history | Preserved paths only; no claim of preserved review wording |
| `F-11` | No original finding text or report path | — | **Unmapped; not verified** |
| `R-01` | Independent review finding; document text inspected | Architecture §7.2/§10.1/§10.2; `SCH-004`, `SCH-009`, `SCH-010` | Addressed in documents; needs independent re-review |
| `R-02` | Independent review finding; document text inspected | Product §4.4; Architecture §12; `SCH-006` | Partially addressed; A-21 owner decision remains |
| `R-03` | Independent review finding; document text inspected | Product §4.8; Architecture §10.2/§11.5; `SCH-010`, `SCH-009` | Addressed in documents; needs independent re-review |
| `R-04` | Independent review finding; document text inspected | Product §4.3/§4.8; Architecture §10.1/§11.5/§17; `SCH-001`, `SCH-007` | Addressed in documents; needs independent re-review |
| `R-05` | Independent review finding; document text inspected | `GATE-001`, `REPO-003`, M1 | Addressed in documents; needs independent re-review |
| `R-06` | Independent review finding; document text inspected | `SCH-003`, `PRV-010`, `SCH-004` | Addressed in documents; needs independent re-review |
| `R-07` | Independent review finding; document text inspected | Architecture §9.1; `PRV-003`, `SCH-004`, `SCH-009` | Addressed in documents; needs independent re-review |
| `R-08` | Independent review finding; document text inspected | This §7.2; Plan §2.1 | Addressed in documents; no original `REV-003` report invented |

Additional safe clarifications: `L-03` (enum/boolean/counter validation) is addressed in Architecture §7.2 and the schema ACs; `L-04` (caller cancellation vs deadline vs provider-side cancellation) is addressed in Architecture §10.3 and `SCH-004`/`SCH-009`. `L-02` (Unicode NFC dependency) remains a minor observation outside this targeted pass and is not claimed as corrected.

---

## 8. Expected verification commands (future tasks)

These are expected commands, not evidence that they have run:

```bash
# Go
gofmt -l .
go vet ./...
go test ./...
go test -race ./...
go build ./...
go test ./internal/provider/mrbilit/... -run TestClassify
go test ./internal/availability/...
go test ./internal/scheduler/...
go test ./internal/notify/...
go test ./internal/httpapi/...

# Frontend
cd web
npm ci
npm run lint
npm run typecheck
npm test
npm run build
cd ..

# Database
sqlite3 /tmp/neda-test.db 'PRAGMA foreign_key_check;'
sqlite3 /tmp/neda-test.db 'PRAGMA integrity_check;'

# CodeGraph (only after source exists)
codegraph status
codegraph init
codegraph status
git check-ignore -v .codegraph/ docs/.codegraph/

# Containers (only if available/approved)
docker build -t neda:local .
docker compose config
```

No ordinary test may require live MrBilit access, real Telegram credentials, or real Push subscriptions.

---

## 9. Risks, blockers, and open decisions

| Item | Type | Impact | Mitigation / next action | Owner input? |
|---|---|---|---|---|
| Go not installed | Blocker | Cannot build/test Go implementation | `FND-001`: approve/install Go after `GATE-002`; Homebrew’s observed `1.27.3` is not an approved pin—reverify current stable and compatibility. | Yes (install approval) |
| Docker not installed | Blocker for local container checks | No local image/compose verification | Use CI Docker build (`OPS-010`); optionally install Docker/Colima (`FND-002`). | Yes (optional install) |
| Server specs unknown | Blocker for deployment/resource proof | Cannot finalize deployment/resource budget | `DISC-005`: owner supplies minimum facts; no server access now. | Yes |
| CodeGraph nested index exists under `docs/` | Repository hygiene | Generated data could be staged | Root `.gitignore` must include `.codegraph/` and `**/.codegraph/` before or in the first docs commit; stage only the exact nine-path manifest; `git add .`, `git add -A`, and `git add --all` are prohibited; verify `git diff --cached --name-only` and `git status --porcelain`; removal/move requires owner approval. | Yes (for removal/move) |
| Check-run start/outcome split | Recovery/data-integrity risk | A crash or duplicate finalisation could lose/duplicate diagnostics or apply an outcome twice | Running `check_runs` row inserted before I/O; conditional running→final update; startup abandons running rows; `SCH-009` crash/replay tests. | No |
| Provider-health A-21 unresolved | Product/behavior decision | Option A/B/C changes alert frequency and false positives/negatives | Product/Architecture transition table records options; `SCH-006` cannot be `DONE` until owner chooses. | Yes |
| Stale booking slug during in-flight check | Correctness/URL safety | Event could carry an obsolete booking URL | Post-I/O gate compares request slug with current `booking_slug`; mismatch discards the attempt; `SCH-009` test. | No |
| Expired monitor date-edit recovery | State/scheduling risk | Monitor could remain permanently expired or discard all results | Date edit recomputes `expires_at`, clears future `expired_at`, preserves paused state; `SCH-001`/`SCH-007` tests. | No |
| Existing README is the Persian starter | Documentation readiness | Initial documentation commit could include a non-English/misleading README | `REPO-003` replaces it with an English planning-state README reviewed against actual files before commit; `FND-012` adds only verified sections as implementation starts; `OPS-004` completes the final README. | No |
| Docker secret permissions vary by platform | Startup/security risk | Non-root key unreadable or mode check fails | Explicit long-form secret mode/uid/gid or read-only bind mount; test startup/restart; final values at `GATE-004`. | Yes (deployment) |
| Push DNS rebinding/redirect SSRF | Security risk | Internal network access | Exact allowlist, DNS/IP validation at dial time, disabled redirects, per-send revalidation, deterministic tests. | Yes (security scope) |
| MrBilit semantics unverified | Correctness risk | Wrong availability classification if guessed | Conservative table; `PRV-006` bounded validation; synthetic labels; fail closed. | Yes (permission/target) |
| Capacity/passenger semantics | Correctness risk | False positives/negatives | Count all passengers; shortfall Unknown; validate with owner/provider. | Yes |
| Booking URL/slug unverified | Feature limitation | No link or misleading link | Omit unless validated; optional slug; tests. | Yes (example) |
| Gender/unknown params | Compatibility risk | Query may fail or be incomplete | Omit unknown params; validate; no invented enums. | Yes |
| Empty list policy | Product tradeoff | May not re-notify if offering disappears/returns | Documented; safety over spam; revisit with evidence/approval. | Optional |
| At-least-once delivery | Product limitation | Rare duplicate notifications | Stable IDs/dedupe/retries; document; no exactly-once claim. | No |
| SQLite single writer | Performance risk | Read latency under long writes | Short transactions, no network in tx, single user; measure. | No |
| Key loss | Data loss | Encrypted settings/Push unrecoverable | Backup DB+key; recovery docs; no silent regeneration. | Yes (backup location) |
| Hard delete history | Data retention choice | History lost on delete | Confirm preference; soft delete possible later. | Yes |
| Local TZ is Tehran | Test blind spot | Timezone bugs masked | Force UTC/other TZ in tests; fixed clocks. | No |
| CodeGraph MCP only explore | Tooling limitation | Less in-session impact analysis | Use CLI `callers`/`impact`/`node` + source/tests; index after source. | No |
| Monitor concurrency interpretation | Open decision | Per-pair vs monitor-wide serialization affects worker guard, rate limits, and `SCH-002`/`SCH-008` tests | Recorded as Product A-33; owner must confirm. If monitor-wide is chosen, update architecture and those tasks separately. | Yes |
| No production access | Deployment risk | Unknown host conflicts | `OPS-005` plan + `GATE-004`; no SSH now. | Yes |

**Current blockers summary:** M1.1 is merged and green; M1.2 is authorized under `GATE-006` and in progress. Remaining external blockers for later milestones: `DISC-005` (server facts), `GATE-003` (live provider validation), `GATE-004` (deployment), and the owner decisions `A-21` and `A-33`. Optional Docker remains unavailable; it is not required for M1.2.

---

## 10. Definition-of-done mapping

| # | Brief DoD item | Tasks | Current status |
|---|---|---|---|
| 1 | Code in verified private GitHub repository `neda` | `REPO-001`, `REPO-002`, `REPO-003` | DONE (private repo; initial docs push verified) |
| 2 | Go backend; React/TS/Vite/Tailwind/shadcn frontend | `FND-*`, `WEB-*`, `FND-013` | Go foundation M1.1 merged; M1.2 store/auth in progress; frontend not started |
| 3 | Persistent SQLite and migrations work safely | FND-007/008, SCH-001 | Migrations/PRAGMAs done (`FND-007`); core auth/settings repository layer done (`FND-008`); monitor repositories remain `SCH-001` |
| 4 | Multiple monitors survive restarts | SCH-001/002/009 | Not started |
| 5 | One job can select multiple providers | SCH-001/008, PRV-009 | Not started |
| 6 | MrBilit first real adapter; fake provider proves independence | PRV-001..009 | Not started |
| 7 | Provider-specific evidence-backed rules; unknown/error never sold out | `PRV-005`, `SCH-005`, `SCH-010`, `NTF-000` | Not started |
| 8 | One provider failure does not invalidate another success | SCH-008 | Not started |
| 9 | Durable events/dedupe across restarts/retries/episodes | `SCH-005`, `SCH-010`, `NTF-000` | Not started |
| 10 | Genuine later episode creates new event | `SCH-005` | Not started |
| 11 | Telegram setup/test/update/remove/delivery without leaks | `NTF-003`, `API-004`, `WEB-007` | Not started |
| 12 | Real server-side Web Push/service worker; browser closed | `PSH-001`–`PSH-006` | Not started |
| 13 | Validated booking links included; unverified omitted | `PRV-007`, `WEB-006` | Not started |
| 14 | Jalali/Gregorian conversion and Tehran expiry tested | `PRV-008`, `WEB-004`, `SCH-007` | Not started |
| 15 | Auth/sessions/CSRF/rate/secrets/private routes protected | `FND-009`, `API-001`/`API-005`, `SEC-002`/`SEC-003` | `FND-009` in progress; HTTP endpoints and private-route enforcement pending `API-001`/`API-005` |
| 16 | Critical tests/builds/static checks pass | `OPS-007` | M1.1 checks pass; full suite pending |
| 17 | Deployment/backup workflow documented for constrained host | `OPS-004`/`OPS-005`/`OPS-009` | Not started |
| 18 | No production deployment without approval | `GATE-004` | Not started |
| 19 | Docs/limitations accurately reflect behavior | `PLAN`, `OPS-004`/`OPS-008` | In progress/planning |

---

## 11. Next proposed action

1. **M1.1 status:** all M1.1 tasks (`FND-001`, `FND-003`, `FND-004`, `FND-005`, `FND-007`, `FND-006`, `FND-010`, `FND-011`, `FND-012`) are `DONE`; PR #1 is merged into `main` at `f2252e5` and both PR-head and `main` CI passed.
2. **M1.2:** `GATE-006` authorizes `FND-008` (repository/transaction layer) and `FND-009` (auth foundation) on `feat/m1.2-store-auth`. `FND-008` is `DONE`; `FND-009` is `IN_PROGRESS`. The narrow store scope, auth primitives/middleware only, and CSRF option A are approved. M1 remains `IN_PROGRESS` until both tasks are `DONE` and the milestone checks pass.
3. **`GATE-003`:** remains blocked; owner must approve the bounded live-validation protocol before `PRV-006`.
4. **`GATE-004`:** remains blocked; only after `OPS-005`, a separate deployment plan and explicit approval authorize server access/deployment.
5. **PR #2:** open against `main` for owner review; the agent must not merge it.

**Hard stop:** no live provider calls, server access, or deployment under the current approval. Silence or a status request is not approval for a new gate.

---

## 12. Tracker maintenance rules

- Update status when a task starts, becomes blocked, and completes.
- Add an evidence row to §7 for every `DONE`/`BLOCKED` transition.
- Record exact commands and outcomes; distinguish passed/failed/not run.
- At session/milestone end, reconcile this tracker against files, tests, commits, and repository state.
- Report `DONE`, `IN_PROGRESS`, `BLOCKED`, next `TODO`, and approvals needed.
- Do not create a second tracker. Do not mark `DONE` without evidence.
