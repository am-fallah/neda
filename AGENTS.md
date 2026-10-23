# Neda — Agent Operating Rules

## 1. Authority and scope
- Read `docs/BRIEF.md` completely before planning or changing the project. It is the source of truth for product requirements and acceptance criteria.
- Read `docs/CODEGRAPH_WORKFLOW.md` before using CodeGraph.
- Do not silently change requirements. Record assumptions as `VERIFIED`, `PROVISIONAL`, or `REQUIRES OWNER INPUT`; request approval for changes affecting behavior, security, data integrity, cost, or scope.
- Prefer the simplest maintainable implementation that satisfies the brief. Do not add infrastructure or abstraction layers without a demonstrated need.

## 2. Mandatory staged workflow
1. Inspect the environment and existing files without overwriting, deleting, reinitializing, or exposing secrets.
2. Prepare `docs/PRODUCT_SPEC.md`, `docs/ARCHITECTURE.md`, and `docs/IMPLEMENTATION_PLAN.md` as documentation-only planning outputs. Include explicit task IDs, priorities, status, dependencies, acceptance criteria, verification evidence, and approval gates.
3. Present findings and the complete plan; stop and wait for explicit owner approval.
4. Only after approval, verify GitHub authentication/repository state and create or configure private repository `neda` safely, if possible. Commit only reviewed documentation after secret review.
5. Implement incrementally, with tests and tracker updates alongside each task.
6. Initialize CodeGraph only after meaningful application source code exists, not while the project is documentation-only. Keep `.codegraph/` ignored by Git.
7. Never access or alter production infrastructure until a separate deployment plan is reviewed and explicitly approved.

## 3. Task tracking contract
- `docs/IMPLEMENTATION_PLAN.md` is the single source of truth for tasks and live progress. Do not create a second active task tracker.
- Every task must have stable ID, priority (`P0`, `P1`, `P2`), status (`TODO`, `IN_PROGRESS`, `BLOCKED`, `DONE`, `DEFERRED`), dependencies, acceptance criteria, and verification commands/evidence.
- Update status when a task starts, becomes blocked, and immediately after it completes. Update the file after each meaningful atomic task, not only at milestone end.
- `BLOCKED` must identify the blocker and who/what can unblock it. `DEFERRED` requires a reason and owner approval if required scope changes.
- Mark `DONE` only when acceptance criteria are met and required checks actually pass. Record exact commands and outcomes; distinguish passed, failed, and not run.
- At session and milestone end, reconcile the tracker against actual files, tests, commits, and repository state. Report `DONE`, `IN_PROGRESS`, `BLOCKED`, next `TODO`, and approvals needed.

## 4. CodeGraph rules
- Verify the actual CodeGraph CLI version, index status, and tools exposed to the current OpenCode session. Do not assume MCP configuration means it is active.
- The owner has already run the CodeGraph installer. Do not rerun `codegraph install` or alter global OpenCode configuration unless a genuine problem is established and the owner explicitly approves the change.
- During documentation-only planning, defer `codegraph init`; a useful code graph requires meaningful source files. Record that this is intentionally deferred, not a failure.
- After approved source code exists, ensure `.codegraph/` is in `.gitignore`, then run `codegraph status`; if no project index exists, run `codegraph init` from the repository root and verify status. Follow installed CLI help if command syntax differs.
- Use CodeGraph for non-trivial navigation, call-path tracing, and impact analysis (e.g., Provider interface, scheduler, availability transitions, persistence, notifications, migrations, cross-layer frontend/backend paths). Use only MCP tools actually exposed by this installed version.
- Verify important findings against source, migration files, and tests. CodeGraph is not proof of correctness and does not replace test/build/static analysis.
- Prefer automatic sync. If stale, inspect status and use supported sync; do not full-reindex or `uninit` without a specific reason. Keep `.codegraph/` out of Git and never claim queries/indexing that did not occur.
- If CodeGraph is unavailable, continue with normal source tools and tests, report the precise limitation, and do not block unrelated work.

## 5. Safety and repository boundaries
- Never expose or commit passwords, tokens, cookies, private keys, Push subscription secrets, encryption keys, production environment files, or private runtime data.
- Never print secret values during inspection. Do not ask the owner to paste a GitHub token into chat.
- Never SSH to, inspect, alter, or deploy to production until the owner approves a reviewed deployment plan.
- Never create a public repository as fallback. Do not force-push, overwrite remote history, or modify unrelated repositories.
- Before any GitHub create/push operation, verify the authenticated account and intended repository. If `neda` exists, do not overwrite or assume it may be reused; inspect safely and ask if ambiguous.
- Do not perform uncontrolled traffic against MrBilit. Live validation must be read-only, sequential, bounded, and comply with the limits in `docs/BRIEF.md`.
- Never infer `NotReservable` from a provider error, timeout, malformed response, or unknown state.

## 6. Engineering conventions
- Keep provider-specific request/response parsing, reservability interpretation, and booking URL logic inside provider adapters. Core engine consumes normalized outcomes only.
- Persist authoritative monitoring state, availability episodes, events, and delivery state in SQLite; in-memory state is not authoritative.
- Use context-aware operations, bounded concurrency/retries, explicit timeouts, structured logs with redaction, and graceful shutdown.
- Add deterministic tests with fixtures and mock servers. Ordinary tests must not require live provider access or real credentials.
- Keep `AGENTS.md`, `docs/BRIEF.md`, product/architecture docs, and implementation tracker aligned with real behavior. Review staged diffs before commits.
