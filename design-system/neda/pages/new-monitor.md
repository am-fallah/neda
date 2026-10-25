# New Monitor — Page Override

> This file overrides `../MASTER.md` for the `/monitors/new` route and the shared
> application shell while that route is mounted, as approved by tasks
> `WEB-010`–`WEB-013`. All other routes keep the master teal primary.

## Why this override exists

The New Monitor workflow is a deliberate, focused search → select → confirm
flow. The owner-approved design uses a neutral accent so the workflow reads as a
calm instrument and the provider-reported availability/price semantics remain
the only meaningful color. This is a route-scoped override, not a second design
system.

## Scope

Applied through `.monitor-create` on the page and
`body.monitor-create-page` on the shell while `/monitors/new` (with or without a
trailing slash) is mounted. The body class must be removed on navigation and
unmount.

## Token overrides

Light:

- `--primary: var(--foreground)`
- `--primary-foreground: var(--background)`
- `--accent: var(--secondary)`
- `--accent-foreground: var(--foreground)`
- `--ring: #5A6561`

Dark:

- `--ring: #A6B2AC`

Semantic `success`, `warning`, `destructive`, and `info` remain unchanged.
Neutral primary must not be used to imply availability, success, or delivery.

## Rules

- Keep the shell consistent: brand mark, active navigation, buttons, portals,
  menus, and dialogs inherit the neutral accent on this route.
- Preserve the primary search action as a single filled action.
- Results remain provider-neutral; do not introduce provider-specific colors,
  iconography, or copy outside the provider adapter/registry.
- Selection state uses a checkbox, row tint, and explicit “Selected” text. Never
  color alone.
- Price is separate from availability. Zero/missing prices show “Price
  unavailable”, never `0`.
- The mobile review bar is sticky and must reserve bottom space so content and
  focus are never obscured.
- The summary rail is sticky only at `xl` and above; below that it stacks after
  the results.

## Verification

- Computed `--primary` on `body` and `.monitor-create` is neutral in light/dark.
- Other routes keep the master pine/teal primary.
- Body class is absent after navigating away or unmounting.
- Axe has zero violations on initial, selected, partial, filtered, empty, stale,
  error, and mobile states.
