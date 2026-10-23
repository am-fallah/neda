# Start Prompt — Neda (Planning Gate)

You are the senior software architect and implementation engineer for Neda. Work from the current directory. Read `AGENTS.md`, `docs/BRIEF.md`, and `docs/CODEGRAPH_WORKFLOW.md` fully before acting.

**This is a planning-only run. Do not implement application code and do not create the GitHub repository yet.** You may inspect files and safely run read-only environment/status commands. You may create or edit planning documentation listed below, but do not modify unrelated existing files.

## Step 1 — Inspect the actual environment

Inspect and report:
- Current directory, files, Git status/branch/remotes, and whether existing work must be preserved.
- Existing `AGENTS.md`, README, OpenCode instructions/config relevant to this project; do not dump config secrets.
- `git`, `gh`, Go, Node.js, package managers, Docker and relevant build tooling availability/versions.
- GitHub CLI authentication status without printing tokens; if safe and possible, read-only check whether `neda` already exists under the authenticated account. Do not create it.
- CodeGraph CLI availability/version (`codegraph --version`, `codegraph --help` as appropriate), project index status, and what CodeGraph tools the current OpenCode session actually exposes. Do not assume that a global configuration file means MCP loaded successfully.

The owner reports that CodeGraph v1.6.2 has been installed and its installer was run globally for OpenCode, and the current folder is `~/Desktop/Neda`. Verify rather than blindly assume this remains true. The folder may currently contain only docs. If there is no meaningful application source code, do NOT run `codegraph init`: explain that indexing is intentionally deferred until initial source modules exist. Do not rerun `codegraph install` or edit global configuration; if MCP still is not loaded after an OpenCode restart, report the exact issue and safe next step.

Never print secrets, raw environment variable values, tokens, cookies, private keys, or GitHub credentials. Do not access the production server, use SSH, install/update packages, run live provider requests, initialize Git, create branches, make commits, push, or create/modify a GitHub repository during this phase. Do not overwrite or delete existing work.

## Step 2 — Produce the planning artifacts

After inspection, create/update only these planning documents as needed:
1. `docs/PRODUCT_SPEC.md`
2. `docs/ARCHITECTURE.md`
3. `docs/IMPLEMENTATION_PLAN.md`

Use `docs/BRIEF.md` as the authoritative source. Preserve all mandatory requirements. Document a proposed repository tree, SQLite schema/migrations, Go provider interface and registry, conservative availability model, durable state transitions and deduplication, provider-health alerts, notification delivery flow, security review, testing/acceptance strategy, milestone plan, and a resource-budget estimate based only on evidence available. Do not invent server CPU/RAM/disk or assert unverified MrBilit behavior. Flag unknown server details and list the minimum data needed later; do not access the server to discover them.

Classify important facts as `VERIFIED`, `PROVISIONAL`, or `REQUIRES OWNER INPUT`. Explicitly address MrBilit request parameters and response semantics, initial availability behavior, passenger-capacity semantics, booking URL/route slug, provider-outage alerts, pause/disable semantics, timezone, and cross-provider notification behavior. Use the brief's proposed safe defaults but explain any unresolved assumption.

`docs/IMPLEMENTATION_PLAN.md` must be the single source of truth for task tracking. It must contain stable task IDs, priority (`P0`/`P1`/`P2`), status (`TODO`/`IN_PROGRESS`/`BLOCKED`/`DONE`/`DEFERRED`), dependencies, acceptance criteria, verification commands/evidence, and approval gates. Include tasks for CodeGraph setup at the appropriate milestone: verify MCP during discovery; defer project indexing while the tree is docs-only; after the first meaningful approved Go/TypeScript source files exist, ensure `.codegraph/` is ignored, initialize the project graph, verify it, and use it for non-trivial call-path/impact analysis. Do not create a second active tracker.

## Step 3 — CodeGraph-specific rules

Read `docs/CODEGRAPH_WORKFLOW.md`. In planning, verify availability only; do not force tools to appear by initializing a graph before source exists. List the actual MCP tools available; do not invent CodeGraph tool names. The supported CLI of the installed version is authoritative. The graph will be initialized only after meaningful app code is present in an approved implementation milestone. The generated `.codegraph/` data must stay out of Git. CodeGraph will supplement—not replace—source inspection, tests, builds, and static analysis.

## Step 4 — Final report and hard stop

Report:
- Environment findings and any existing work preserved.
- Verified/provisional/owner-input assumptions.
- Important architecture and dependency decisions, including proposed deviations from the baseline stack.
- Files created/changed.
- Actual commands and checks run, with exact results; explicitly say when tests were not run.
- Git/GitHub status without claiming any creation/push.
- CodeGraph CLI/MCP/index state and when indexing should occur.
- Summary of prioritized tasks and the next proposed action.

Then **STOP and wait for explicit owner approval**. Do not create the private repository, initialize Git, commit/push, implement application code, or access/deploy to the server until the owner approves the relevant next phase. Do not interpret silence or a request to report status as approval.
