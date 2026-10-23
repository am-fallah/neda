# CodeGraph workflow for Neda

This document describes how Neda should use the owner-installed `@colbymchenry/codegraph` CLI/MCP integration. Check the installed CLI's help/version and tools exposed in the running OpenCode session; do not blindly assume online docs exactly match an installed version.

## Current intended state

- CodeGraph CLI was reported as v1.6.2 on the owner's Mac.
- The installer was run and reported updating `~/.config/opencode/opencode.jsonc` for OpenCode.
- OpenCode must be restarted after the MCP config changes.
- Installing/configuring MCP and indexing a project are separate operations.
- Neda currently starts as a docs-first starter. Do not build a project code graph until useful source files exist.

## Safe check sequence

From the Neda project root, the owner/agent may inspect:

```bash
codegraph --version
codegraph --help
codegraph status
opencode mcp list
```

`opencode mcp list` is a shell/CLI check and may differ from what an already-running OpenCode session has loaded. When in doubt, restart OpenCode and ask the current agent which CodeGraph MCP tools are actually exposed. Never print `opencode.jsonc` wholesale if it might contain credentials or unrelated settings.

If the index is absent in the initial docs-only stage, this is expected. Do not run `codegraph init` just to populate the MCP tool menu.

## Initialize when source code exists

After the owner approves implementation and the first meaningful Go/TypeScript source files exist:

1. Ensure `.gitignore` includes `.codegraph/` before indexing.
2. From the repository root, run `codegraph status`.
3. If no index exists, run `codegraph init` using syntax confirmed by the installed `codegraph --help`.
4. Run `codegraph status` again to confirm initialization.
5. Restart the OpenCode session only if MCP tools do not become available after indexing or if the CLI docs/version require it.
6. Verify `.codegraph/` is ignored by Git (e.g. `git check-ignore -v .codegraph/`). Do not stage graph data.

Do not re-run the global installer just because a project's index is missing. Do not delete `.codegraph/`, run `uninit`, or edit global MCP config for routine troubleshooting without a specific reason and owner approval.

## How the agent should use it

Use the MCP tool actually exposed by the installed CodeGraph version for non-trivial tasks involving symbol relationships, calls, dependencies, or impact. Common Neda examples:

- Trace a poll from scheduler → provider interface/adapter → normalized result → state transition/SQLite transaction → event/delivery record → Telegram/Web Push.
- Before changing a provider interface, identify implementations, callers, related tests, and configuration points.
- Before changing persistence/schema, locate repository methods, transactions, migration code, and affected tests.
- Before changing notification behavior, trace event generation separately from channel delivery/retry logic.
- For frontend work, trace hooks/API clients to backend endpoints and affected component tests where supported.

Prompt examples for the agent:

> Use the connected CodeGraph MCP to trace the monitoring path from scheduler through availability transition, persistence, and notification delivery. Report relevant symbols/files and call paths. Verify critical details against source code. Do not edit files yet.

> Before changing the Provider interface, use CodeGraph to locate implementations, callers, dependent tests, and likely impact. Show the source evidence and propose targeted tests before editing.

> Analyze this change's blast radius using CodeGraph, then inspect the actual source and run the targeted tests. Clearly separate graph findings from verified test results.

## Index health

- Prefer automatic file watching/sync when the installed version supports it.
- If results appear stale, check `codegraph status` and use supported incremental `codegraph sync` if necessary.
- Use full reindex (`codegraph index`) only when status or a reproducible problem justifies it; state the reason.
- If the CLI/MCP is unavailable, continue using source files, language tooling, and tests. Report the blocker accurately and do not invent tool results.

## Boundaries

CodeGraph is a code-navigation and impact-analysis aid. It does not prove business correctness, security, or test success. It does not replace the task registry, product/architecture docs, tests, compiler, `go vet`, frontend lint/build, or security review. Never claim a query or index verification occurred unless it actually did.
