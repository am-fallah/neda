# Neda Design System — Master

> Page overrides live in `pages/`. If a page file exists, its rules override this
> master. Otherwise, this file is the only visual source of truth.

---

**Project:** Neda — private train availability monitoring dashboard
**Generated:** 2026-10-25 (UI/UX Pro Max `--design-system`)
**Category:** Status / incident operations dashboard (nearest supported product profile)
**Design dials:** Variance 3/10 · Motion 2/10 · Density 8/10

## Provenance

The system was generated with the installed UI/UX Pro Max search tool at
`/Users/fallah/.agents/skills/ui-ux-pro-max/scripts/search.py`.

- Design-system query: `operations console data-dense admin panel minimal`
- Design-system query (first pass, rejected after fit review):
  `train availability monitoring operations SaaS dashboard` — returned
  Glassmorphism, which conflicts with the brief's no-excessive-glassmorphism and
  restrained-surface requirements.
- Style query: `data dense dashboard real-time monitoring` → **Data-Dense
  Dashboard** (`data-dense-dashboard`), secondary **Minimalism & Swiss Style**.
- Product query: `operations monitoring dashboard` → **Status Page / Incident
  Management**: Data-Dense Dashboard + Real-Time Monitoring, with status
  green/incident red/maintenance amber.
- Color query: `status page incident neutral teal` and
  `railway travel monitoring teal neutral` → neutral surfaces, calm teal, status
  semantics. The palette below preserves Neda's existing teal identity while
  raising contrast and removing the generated dark-only recommendation.
- Typography query: `data dashboard professional humanist sans` → **Dashboard
  Data** (Fira Sans + Fira Code). Fira Code is used for data, identifiers, and
  micro-labels; Fira Sans carries headings and prose. Vazirmatn is the
  self-hosted Persian/Arabic fallback so mixed route names stay legible.
- Stack queries: React, shadcn, and html-tailwind. Results reinforced semantic
  HTML, compound components, native focus management, table structure, and
  responsive testing.
- UX queries: dashboard hierarchy, responsive table overflow, keyboard focus,
  dark-mode contrast, error summaries, icon labels, touch targets, and table
  semantics.

The generated dark navy + bright green palette and 24px card treatment were
**not** adopted: they would erase Neda's teal identity, exceed the intended
density, and contradict the brief's restrained-surface requirement. The final
tokens below are the adapted, verified system.

## Visual direction

**“Railway operations console.”** Swiss restraint with a warm paper canvas, ink
typography, hairline structure, deep pine/teal actions, and a single amber
railway accent. The interface should read like a dependable instrument panel,
not a marketing dashboard: compact, calm, legible, and explicit about state.

### Principles

1. **State before decoration.** Availability, delivery, and provider health use
   semantic color plus text and icon. Color is never the only signal.
2. **Dense, but breathable.** 8/10 density: 12–20px card padding, 8px rhythm,
   compact tables, no oversized hero cards or decorative whitespace.
3. **One primary action per screen.** Use a single filled primary action; all
   other actions are outline, ghost, or link.
4. **Hairlines, not shadows.** Cards are flat surfaces with a 1px border. Depth
   is reserved for overlays.
5. **Numbers are data.** Use tabular figures and Fira Code for prices, counts,
   times, IDs, and timestamps.
6. **No fake liveness.** Label timestamps and show stale/unknown states. Never
   describe an observation as live when it is a saved check.
7. **Motion explains cause and effect.** 120–180ms for feedback, 200ms for
   surfaces, and no entrance choreography. Reduced motion renders the final
   state immediately.

## Tokens

All values are defined in `web/src/styles.css`. Components consume semantic
Tailwind tokens; raw hex values must not appear in route components.

### Color — light

| Role | Value | Notes |
|---|---|---|
| Background | `#F6F5F1` | Warm paper canvas |
| Foreground | `#19211F` | 15.0:1 on background |
| Card | `#FFFFFF` | Flat workspace surface |
| Muted | `#EFF1EC` | Secondary surface |
| Muted foreground | `#5A6561` | 6.1:1 on card |
| Border | `#D7DED8` | Hairline separators |
| Input | `#7E8F87` | 3.4:1 control boundary |
| Primary | `#0F5B50` | Neda pine/teal; 8.0:1 with white |
| Primary foreground | `#F7FBF9` | On primary |
| Accent | `#E2EFE9` | Selected/active tint |
| Accent foreground | `#0B4A41` | On accent |
| Success | `#1B6B3A` | Reservable / sent |
| Warning | `#8A5A00` | Unknown / stale / retrying |
| Destructive | `#B42318` | Failed / degraded |
| Info | `#1F5FA8` | Neutral operational information |
| Railway accent | `#B45F16` | Brand rule and rare emphasis only |
| Ring | `#0F5B50` | 3px visible focus |

### Color — dark

| Role | Value | Notes |
|---|---|---|
| Background | `#101513` | Deep green-black |
| Foreground | `#E9EFEB` | 15.8:1 on background |
| Card | `#171D1B` | Raised surface |
| Muted | `#232C28` | Secondary surface |
| Muted foreground | `#A6B2AC` | 7.8:1 on card |
| Border | `#2C3733` | Hairline separators |
| Input | `#5E6E66` | 3.2:1 control boundary |
| Primary | `#72CDB8` | Light teal action |
| Primary foreground | `#06231E` | 8.8:1 on primary |
| Accent | `#1D3832` | Selected/active tint |
| Accent foreground | `#B5E6D9` | On accent |
| Success | `#6ACA8E` | Reservable / sent |
| Warning | `#F0C36A` | Unknown / stale / retrying |
| Destructive | `#F27A6D` | Failed / degraded |
| Info | `#8BBDF4` | Neutral operational information |
| Railway accent | `#E39B5B` | Brand rule and rare emphasis only |
| Ring | `#83D6C4` | 3px visible focus |

Semantic mapping is fixed: `reservable` is success, `not_reservable` is neutral,
`unknown` is warning, `failed`/degraded is destructive. Zero or missing prices
are never styled as success or as the cheapest option.

### Typography

| Role | Font | Size / line-height | Usage |
|---|---|---|---|
| Display | Fira Sans 600 | 28–36px / 1.15 | Page title only |
| Section | Fira Sans 600 | 18–20px / 1.3 | Major sections |
| Card title | Fira Sans 600 | 14–15px / 1.35 | Card headings |
| Body | Fira Sans 400 | 14px / 1.55 | Default |
| Small | Fira Sans 400 | 12–13px / 1.5 | Metadata |
| Micro-label | Fira Code 500 | 10–11px / 1.4, 0.12em tracking | Eyebrows, table heads |
| Data | Fira Code 400–500 | inherits, tabular-nums | Prices, times, counts, IDs |

Persian/Arabic text uses Vazirmatn as a per-glyph fallback. Do not force Latin
tracking or uppercase on Persian strings. Use `<bdi dir="auto">` around mixed
route, operator, class, and wagon names.

### Spacing and shape

- 4px base rhythm; component padding 12–20px; page section gaps 20–32px.
- Radius: 6px controls/badges, 8px inputs/buttons, 10px cards, 12px overlays.
- Shadow: only `0 1px 2px rgb(16 24 20 / 0.04)` on static cards and a 12–24px
  soft shadow for overlays. No hover lift.
- Content width: sidebar 240px; main `max-w-[80rem]`; page gutter 16/24/32px.
- Breakpoints: 360, 390, 640, 768, 1024, 1280, 1440.

## Component rules

### App shell

- Desktop: fixed 240px sidebar, flat card surface, hairline right border.
- Sidebar: brand, one primary “New monitor” action, grouped navigation
  (`Workspace` / `System`), quiet footer with theme and account.
- Active navigation: tinted surface, left 2px accent rule, icon and text both
  shift to the accent foreground; `aria-current` is provided by `NavLink`.
- Mobile: 64px top bar with visible menu, brand, theme, and account. Navigation
  opens as a modal drawer with focus trap, Escape dismissal, and a close control.
- Skip link is the first focusable element and targets `main-content`.
- After route changes, focus moves to the main landmark.

### Page header

- Eyebrow (optional) in Fira Code uppercase, then H1, then one-sentence
  description capped at 68ch.
- Actions sit on the same row at `sm` and above, stack full-width below.
- Include breadcrumbs on monitor detail/edit and event detail.

### Buttons and controls

- Primary: filled pine; one per screen. Secondary: outline. Tertiary: ghost/link.
- Visible press feedback through surface darkening, not displacement.
- Focus: 3px ring at 45% opacity plus 2px offset. Never remove outline without a
  replacement.
- Touch targets: 44px minimum on narrow viewports; 36–40px permitted on desktop
  for dense toolbars. Icon-only controls always have `aria-label`.
- Disabled: 45% opacity, `cursor-not-allowed`, no pointer events, semantic
  `disabled`.
- Never rely on hover for an action.

### Cards and data surfaces

- Flat 10px cards with 1px border, no drop shadow on hover, 16px header/content
  padding (20px on `sm`).
- Do not nest a dashed empty state inside a bordered card unnecessarily; use one
  surface and a quiet inline empty block.
- Tables: compact 36–40px rows, 10–11px uppercase micro-headers, tabular
  numerics, hover row tint, horizontal overflow contained inside the table
  wrapper with `overscroll-x-contain`. Never allow page-level horizontal scroll.
- Status badges: 11px, 6px radius, icon + text. Dot-only status is forbidden.
- Long IDs, URLs, and mixed-direction text must wrap (`overflow-wrap:anywhere`)
  and use `<bdi>` where direction is ambiguous.

### Forms and feedback

- Always visible labels; placeholders are examples only.
- Required fields marked with a visible `*` and an accessible required name.
- Validate on submit and clear per-field errors as the user corrects them.
- Inline errors below the field, connected with `aria-describedby`, announced via
  `role="alert"` or a polite live region.
- Multi-error forms show a focusable error summary at the top with links to each
  invalid field, and move focus to it after a failed submit.
- Async actions: disable the initiating control, show a spinner/progress label,
  then a success or error state. No duplicate submission.
- Toasts never steal focus; auto-dismiss in 4–5s. Destructive actions require a
  labelled confirmation dialog with a safe default.

### Navigation and overlays

- Radix primitives own dialog focus trapping and Escape handling.
- Dialog close control is 44px on mobile; title and description are always
  present.
- Mobile drawer content scrolls independently; underlying page is not scrollable.
- Sticky bars reserve bottom/top padding so content and focus are never obscured.

### Motion

- Tokens: `--motion-fast: 120ms`, `--motion-base: 180ms`,
  `--motion-surface: 240ms`; easing `cubic-bezier(0.2, 0.8, 0.2, 1)`.
- Allowed: color/opacity/ring transitions, chevron rotation, skeleton shimmer,
  toast/dialog enter-exit.
- Avoid: entrance animations on page load, animated layout properties, parallax,
  decorative loops.
- `prefers-reduced-motion: reduce` disables non-essential animation and smooth
  scrolling globally.

## Page overrides

- `pages/new-monitor.md` documents the approved neutral accent scoped to the New
  Monitor workflow (WEB-012). The workflow keeps neutral primary/accent/ring
  tokens while retaining semantic success/warning/destructive colors.

## Responsive contract

- 1440: sidebar + multi-column data views; summary rail sticky only when it
  fits.
- 1024: sidebar visible, results/summary stack when the rail would fall below
  ~22rem.
- 768: two-column cards and forms where useful; tables scroll inside their
  wrapper.
- 390/360/320: single column, 16px gutters, 44px controls, full-width page
  actions, no page-level horizontal overflow.

## Accessibility contract

- WCAG 2.2 AA: 4.5:1 text, 3:1 non-text boundaries, 3px visible focus.
- Icons beside text are `aria-hidden`; icon-only controls have an accessible name;
  toggles expose `aria-pressed`/`aria-expanded`.
- Heading order is sequential; each page has one H1.
- Status uses icon + text, not color alone.
- Touch targets meet 44px on mobile; adjacent targets have at least 8px spacing.
- Tables use semantic `thead`/`tbody`/`th`; scroll wrappers are keyboard reachable.
- No content or focus is hidden behind sticky headers/footers.
- Both themes are tested independently with axe and visual review.

## Anti-patterns

- No emoji as UI icons; use Lucide only.
- No excessive glassmorphism, gradient meshes, neon glow, or decorative blur.
- No oversized hero cards or metric tiles without a decision they support.
- No invented currency, per-passenger basis, totals, or “sold out” language.
- No zero/missing price rendered as free or cheapest.
- No color-only availability or delivery state.
- No hover-only affordances; no layout-shifting hover transforms.
