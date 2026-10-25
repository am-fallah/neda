# Neda Dashboard — Manual Acceptance Checklist

**Status: checklist prepared and partially executed on 2026-10-24 with an automated Chrome/Playwright audit against a local development build using the fake provider.** A separate New Monitor corrective audit was executed on 2026-10-25 against a production frontend build with mocked API/search fixtures and 0 axe violations across the states recorded below. A further New Monitor hierarchy/price audit on 2026-10-25 verified the train→wagon→fare presentation, wagon/all-offering selection, view-only price filtering, and 320/360/390 responsive behavior with 0 axe violations. The automated audit covered login, the unauthenticated redirect, wrong-password feedback, overview, theme persistence, monitor creation with the Jalali picker, a real reservable observation, pause/resume, history filtering, settings secret handling, mobile navigation, logout, skip-link keyboard focus, dialog Escape handling, and an axe WCAG 2 A/AA audit with zero violations on six screens. Screenshots were visually reviewed. Human-only cases and any flow requiring real Telegram credentials, Web Push, production providers, or a second device remain marked **NOT RUN — manual**. Do not use production providers, real Telegram credentials, or production infrastructure for this checklist unless separately authorized.

## Preconditions

- [ ] Repository is clean and the latest `main` is checked out.
- [ ] Go and Node prerequisites from the README are installed.
- [ ] Local password is set with `neda set-password`.
- [ ] Backend runs in development with `NEDA_FAKE_PROVIDER_ENABLED=true` (test-only data).
- [ ] Frontend runs with `cd web && npm run dev`, or a production-style build is embedded in the Go binary.
- [ ] Browser uses `http://localhost:5173` (Vite) or the configured local origin, not a mismatched Host.
- [ ] No real Telegram token/chat ID is used for this checklist.

## Executed automated browser audit (2026-10-24)

Environment: production-style embedded frontend served by the Go binary on `127.0.0.1`, temporary SQLite, development mode, `NEDA_FAKE_PROVIDER_ENABLED=true`, system Chrome via Playwright 1.64.1. No live provider, real Telegram, Push, or production server was used. Local test credentials were temporary and are not recorded in the repository.

| Flow | Result | Evidence |
|---|---|---|
| Unauthenticated `/` redirect | PASS | Redirected to `/login`. |
| Wrong password | PASS | Generic "Incorrect password." field error; browser logged the expected 401 resource status only. |
| Correct login | PASS | Overview rendered with no console/page errors. |
| Overview empty state and provider warning | PASS | Empty counters, no-events/no-deliveries states, and test-provider labeling shown. |
| Theme toggle and persistence | PASS | Dark class applied; survived reload; returned to light. |
| Create monitor through the UI | PASS | Jalali "Today" selection, fake-provider route IDs, submit, redirect to detail. |
| Initial availability observation | PASS | Scheduler completed a fake-provider check; `Reservable` and the historical/last-observed wording appeared. |
| Pause and resume | PASS | Status changed to Paused and back to Active. |
| History | PASS | Availability event rendered; availability filter fetched and displayed the event. |
| Settings secret handling | PASS | Bot token/chat fields empty; no temporary password in rendered text; interval 300 shown. |
| Mobile 390 px | PASS | Sidebar collapsed, mobile navigation worked, Monitors list rendered, no horizontal overflow. |
| Logout | PASS | Signed out and returned to the password form. |
| Keyboard focus | PASS | First Tab focused "Skip to main content"; Enter moved focus to `main-content`. |
| Dialog keyboard dismissal | PASS | Escape closed the delete confirmation without submitting. |
| Axe WCAG 2 A/AA + 2.1 A/AA | PASS | 0 violations on overview, monitor create, monitor detail, history, settings, and mobile monitors. |
| Console/page/network errors on successful flows | PASS | None recorded. |

**Not covered by the automated audit:** real Telegram save/test delivery, Web Push, production provider data, long-running polling, human screen-reader/zoom/contrast review, and the remaining error/empty permutations listed below. The automated audit does not replace human accessibility review.

## New Monitor corrective audit (2026-10-25)

Environment: production `vite preview` build, system Chrome through a temporary Playwright 1.64.1 install and axe-core 4.10.3. Private API and MrBilit-search responses were mocked with deterministic fixtures. No backend database was read or changed; no live provider, Telegram, Push, or booking action occurred. Screenshots were reviewed at widths 1440, 1280, 1024, 390, 360, and 320 in light and dark modes.

This audit supersedes the earlier content-only review. It explicitly covers the shared shell, standard MrBilit URLs with omitted defaults, no-count URLs, manual edits, invalid values, and the full state set.

| Check | Result | Evidence |
|---|---|---|
| Initial state — light/dark | PASS | Two-column layout at 1440/1280; page header, stepper, compact URL-first search card, empty confirmation panel, and global delivery status render without console/page errors. |
| Page-wide neutral accent | PASS | Route-scoped body class plus content scope. Computed light `--primary` on `<body>` and `.monitor-create` is `oklch(21% .006 285)`; brand background and primary buttons use the same neutral; active nav and portaled menu hover use neutral secondary; dark values invert correctly. Other routes retain the global teal brand (`oklch(0.45 0.09 180)`). |
| Standard URL passenger extraction | PASS | `adultCount=2&childCount=1` renders `2 adults · 1 child`, leaves the three-field editor collapsed, and sends `{adults: 2, children: 1, infants: 0}` in the search request. Omitted `infantCount` uses MrBilit's zero default. |
| No-count URL fallback | PASS | A URL without passenger parameters shows clearly labelled `1 adult` defaults and sends `{adults: 1, children: 0, infants: 0}`; it does not claim extraction. |
| Manual passenger edit | PASS | Opening the compact editor, changing a count, and searching sends the edited values; the monitor-create payload preserves the same counts. |
| Invalid URL counts | PASS | Present-but-invalid values show a warning, are not applied, and open the editor; search is blocked until the URL is fixed or counts are edited manually. |
| Journey/class-family grouping | PASS | Equivalent classes are grouped under one family header; distinct offerings remain individual `Option` rows; different wagons/compartments remain separate; unavailable options remain selectable; unknown remains distinct. |
| Selection state | PASS | Selection uses a custom checkbox with a visible check, row emphasis, and an explicit `Selected` badge (not colour alone). Keyboard focus is visible on the checkbox. |
| Exact selection and summary | PASS | Selecting two offerings updates the count, selected list, label, and payload. Captured create payload included exactly the two selected offerings and `adults: 2, children: 1, infants: 0`. |
| Stale-search invalidation | PASS | Changing the URL clears selections, disables results and creation, shows "Search settings changed", and ignores an aborted in-flight response. |
| Loading state | PASS | Search button and results skeleton announce progress; the confirmation panel also shows a searching state; duplicate submission is guarded. |
| Empty result | PASS | Compact "No trains returned" block says this is a successful empty result, not a sold-out signal; confirmation panel says "No classes to review"; creation is unavailable. |
| Search error | PASS | Provider failure shows an actionable error without implying unavailability; URL errors focus the URL field. |
| Creation error | PASS | Summary-level error plus mapped selection field error; create action is re-enabled for retry. |
| Label preservation | PASS | A manually edited label survives a subsequent search; the generated label uses the URL slug and Gregorian date to avoid bidi reordering. |
| Global delivery status | PASS | Polling interval is humanized; Telegram/Push status is read-only, labelled `Global`, and links to Settings; delivery status is also visible before search. |
| Persian/English bidi | PASS | Persian route/operator/class/wagon/compartment values were rendered with `bdi` isolation; the route arrow, dates, capacities, and English labels remained readable and correctly ordered. |
| Desktop layout | PASS | 1280px grid: `584px 352px` (results + summary); 1024px stacks to a `704px` single column. Summary rail is sticky only at `xl` and above; it does not dominate the results. |
| Mobile layout | PASS | 390px, 360px, and 320px: search, grouped results, editor, selected options, delivery, and create action stack cleanly; no horizontal overflow at any width. |
| Axe WCAG 2 A/AA + 2.1 A/AA | PASS | 0 violations on initial light/dark, selected success light/dark, searching, empty, invalid URL counts, provider error, stale, creation error, bidi, and 390/360/320px results. |
| Console/page errors | PASS | No page errors or unexpected console errors on the audited flows; mocked provider-error/creation-error requests logged only their expected 502/422 resource statuses. |

One payload-only capture intentionally did not mock the post-create monitor-detail route; the resulting expected 404 after navigation was excluded from console-error assertions. The create request itself succeeded and its exact payload was captured.

Reported gaps from the earlier review are closed: standard MrBilit URLs with omitted defaults now prefill without manual entry, and the neutral accent covers the shared shell, not only the content subtree.

Remaining manual acceptance for the owner: human screen-reader/zoom review and final preference review of density, Persian/English typography, and the summary rail at the owner's viewport. Real Telegram/Push delivery, live provider monitoring, and production deployment remain out of scope for this audit.

## New Monitor hierarchy and price audit (2026-10-25, NM-01…NM-10)

Environment: production `vite preview` build of the current branch, system Chrome through Playwright 1.64.1, and axe-core 4.10.3. Private auth/providers/settings/Push endpoints and the MrBilit search response were mocked with the saved six-offering observed shape (two wagons × three offerings, including zero and missing prices). No backend database was read or changed; no live MrBilit, Telegram, Push, booking, or deployment action occurred. Screenshots were reviewed at 1440 light/dark and 390/360/320 mobile widths.

| Check | Result | Evidence |
|---|---|---|
| Train/wagon hierarchy | PASS | The six offerings render as one train and two wagon groups (Samen, Sepehr), each with one reservable and two not-reservable children. The six low-level offerings are not shown as six default rows. |
| Wagon selection | PASS | Selecting the Samen wagon selects all three current underlying offering keys; the review rail shows `1 wagon · 3 fare options`, a partial/all state, and the exact price/state summary. |
| Partial fare selection | PASS | Expanding Sepehr exposes three fare rows; selecting one shows `1 of 3 selected`, and the create payload retains only that exact offering key. |
| Price display | PASS | Positive amounts render as provider-reported numbers with an explicit “currency and per-passenger basis are not verified” qualification. Zero/missing values show `Price unavailable`, never `0`; the wagon summary shows `2 unpriced`. |
| Price filter | PASS | A 2,000,000–3,000,000 filter reduces the visible result to one fare option and one wagon, reports four unpriced options hidden and one priced option outside the range, and offers an explicit include-unpriced control. |
| Filter/selection independence | PASS | A previously selected Samen wagon remains selected after the filter hides it; the page reports that three selected fare options are outside the view and keeps creation available with the original selection. A filtered wagon selection still selects every underlying offering, not only the visible priced child. |
| Sort | PASS | Departure and lowest-known-price sorting are available; sorting changes presentation only. Component tests verify order and payload preservation. |
| Empty/unknown/unavailable | PASS | The saved fixture shows Reservable, Not reservable, and Unknown as distinct states; a wagon group is never labeled not reservable unless all children are decisive negatives. Empty results remain a successful empty observation, not sold out. |
| No-offering state | PASS | A train with no usable fare/class offerings has a distinct “No fare options returned” state and does not imply sold out. |
| Stale/error/creation error | PASS | Existing URL/count invalidation disables results and creation; provider errors state that no availability conclusion was made; creation errors remain actionable and re-enable the action. |
| Desktop layout | PASS | 1440px two-column layout: results + sticky review rail. Detail expansion and price-filter notices remain aligned with the rail. |
| Mobile layout | PASS | 390/360/320px: one column, compact price/filter/sort toolbar, sticky `Review & create` action after selection, no horizontal overflow. |
| Axe WCAG 2 A/AA + 2.1 A/AA | PASS | 0 violations on initial light/dark, results light/dark, selected light/dark, expanded light, price-filtered light, and 390/360/320px mobile results. |
| Runtime/page errors | PASS | The audited mocked flows mounted and completed without page errors. |
| Live-provider and delivery scope | NOT RUN — intentionally | No live MrBilit request, real monitor creation, Telegram/Push message, booking, database, or production deployment was performed. |

## Authentication

| Check | Expected | Automated coverage | Manual result |
|---|---|---|---|
| Open `/` without a session | Redirect to `/login` | Page-level auth context; no browser redirect test | NOT RUN — manual |
| Submit a wrong password | Generic "Incorrect password." message; no field/value leak | `LoginPage.test.tsx` | NOT RUN — manual |
| Submit repeatedly to trigger rate limiting | "Too many sign-in attempts" and disabled retry countdown; `Retry-After` honored | `LoginPage.test.tsx`; API tests | NOT RUN — manual |
| Sign in with the owner password | Dashboard loads; session and CSRF cookies are HttpOnly; CSRF token exists only in memory | API/auth Go tests; client tests | NOT RUN — manual |
| Reload after sign-in | Session probe restores the authenticated layout | Go auth tests; no browser test | NOT RUN — manual |
| Delete/expire the session cookie, then load data | Private 401 returns the user to `/login`; no stale dashboard data shown | `client.test.ts`; Go auth tests | NOT RUN — manual |
| Sign out | Session revoked; login page shown | Go auth tests; client tests | NOT RUN — manual |
| Open when no password is configured | Setup guidance shows `neda set-password`; no web bootstrap form | `LoginPage.test.tsx` | NOT RUN — manual |
| Change the password in Settings | Other sessions revoked; current session remains; fields clear | `SettingsPage.test.tsx`; Go auth tests | NOT RUN — manual |

## Overview

| Check | Expected | Automated coverage | Manual result |
|---|---|---|---|
| Loading state | Skeleton/status region, no fabricated counts | Component structure | NOT RUN — manual |
| Empty installation | Clear empty states and provider warning; no fake monitors | `OverviewPage.test.tsx` | NOT RUN — manual |
| Counts | Active/paused/expired monitors, provider health, delivery statuses match the API | `OverviewPage.test.tsx` | NOT RUN — manual |
| Degraded provider | Separate warning says provider health is not ticket availability | `OverviewPage.test.tsx` | NOT RUN — manual |
| Failed/queued delivery | Failed/queued is not presented as delivered | `OverviewPage.test.tsx`; `status.test.tsx` | NOT RUN — manual |
| API error | Sanitized error with retry, no raw internals | `OverviewPage.test.tsx`; `ErrorState` | NOT RUN — manual |
| Auto-refresh | Refreshes while visible, pauses while hidden | Hook implementation; no timer browser test | NOT RUN — manual |

## Monitors

| Check | Expected | Automated coverage | Manual result |
|---|---|---|---|
| List | Label, route, Jalali/Gregorian date, providers, passengers, status, last attempt/next check | Component code; no full list test | NOT RUN — manual |
| Status distinction | Failed check says availability unknown; not-reservable says not reservable, never sold out; unknown stays unknown | `monitorSummary.test.ts`; `MonitorDetailPage.test.tsx` | NOT RUN — manual |
| Create with valid form | Gregorian date accepted; providers and route IDs saved; redirect to detail | `monitorForm.test.ts`; no end-to-end browser test | NOT RUN — manual |
| Create with missing fields | Field-level messages; focus moves to the first error; server remains authoritative | `monitorForm.test.ts`; form implementation | NOT RUN — manual |
| Identical origin/destination | Rejected client-side and by server | `monitorForm.test.ts`; Go monitor tests | NOT RUN — manual |
| Past Jalali date | Disabled in picker; rejected by client and server | Date/form tests; Go monitor tests | NOT RUN — manual |
| Provider route IDs | Required per provider; test provider prefixes and unsupported slug/gender are explained | `monitorForm.test.ts`; Go provider tests | NOT RUN — manual |
| Pause/resume | Pause stops scheduling; resume re-enables; expired resume is blocked with edit guidance | `MonitorDetailPage.test.tsx`; Go scheduler tests | NOT RUN — manual |
| Edit | Material changes reset availability; label-only changes do not | Go monitor tests; form code | NOT RUN — manual |
| Delete | Confirmation dialog; cancel does nothing; confirm deletes and navigates | `MonitorDetailPage.test.tsx` | NOT RUN — manual |
| Observation display | Last observed timestamp and "not a live guarantee"; missing/unknown never treated as unavailable | `MonitorDetailPage.test.tsx` | NOT RUN — manual |
| Booking link | Renders only for a valid `https` URL; unavailable links are omitted | `BookingLink`; Go provider/event tests | NOT RUN — manual |

## History and delivery status

| Check | Expected | Automated coverage | Manual result |
|---|---|---|---|
| Event filters | Kind, monitor, provider apply and reset correctly | `HistoryPage.test.tsx` | NOT RUN — manual |
| Event pagination | Cursor "Load more" appends without duplicates | `HistoryPage.test.tsx` | NOT RUN — manual |
| Delivery filters | Channel, status, event kind apply correctly | `HistoryPage.test.tsx` | NOT RUN — manual |
| Event detail | Historical label, journey/ticket, provider, booking link, per-channel delivery status | Event detail code; no page test | NOT RUN — manual |
| Delivery language | Pending/sending/retry/failed/skipped are clearly not sent | `status.test.tsx`; `HistoryPage.test.tsx` | NOT RUN — manual |
| Empty/error states | Clear messaging and retry | Component structure | NOT RUN — manual |

## Settings and Telegram

| Check | Expected | Automated coverage | Manual result |
|---|---|---|---|
| Polling interval bounds | Values outside 60–3600 rejected client-side; server remains authoritative | `SettingsPage.test.tsx`; Go settings tests | NOT RUN — manual |
| Telegram status | Configured/unreadable/unavailable states explained; masked chat label only | `SettingsPage.test.tsx`; Go API tests | NOT RUN — manual |
| Secret display | Token/chat fields are empty; no token, raw chat ID, or ciphertext is prefilled or rendered | `SettingsPage.test.tsx`; Go response/log scans | NOT RUN — manual |
| Save credentials | Write-only fields; encrypted at rest; success feedback; fields clear | `SettingsPage.test.tsx`; Go tests | NOT RUN — manual |
| Clear credentials | Confirmation required; stored config removed | `SettingsPage.test.tsx`; Go tests | NOT RUN — manual |
| Telegram test | Explicit button only; successful/failed result is sanitized | `SettingsPage.test.tsx`; fake-server Go tests | NOT RUN — manual |
| Real Telegram send | Requires owner credentials and sends a real message; **not authorized in this session** | Not applicable | NOT RUN — requires authorization |

## Browser Web Push

| Check | Expected | Automated coverage | Manual result |
|---|---|---|---|
| Server availability | If VAPID subject/key is missing, show operator guidance and disable controls; no fake success | `PushSettings.test.tsx`; Go push tests | Real browser control/empty states passed; unavailable-key state covered by tests |
| Unsupported browser/HTTPS | Explain service-worker/Push/HTTPS requirements; no control implying success | `push.test.ts`; `PushSettings.test.tsx` | Covered by tests; real unsupported browser not tested |
| Permission flow | Permission requested only after explicit "Enable this browser"; denial shows actionable guidance and saves nothing | `PushSettings.test.tsx`; real Chrome denial test | PASS — real CDP-denied permission showed actionable guidance; no save |
| Register device | Valid subscription is stored; device label/list refreshes; no endpoint/key material is rendered | `PushSettings.test.tsx`; Go API tests | Real FCM/Mozilla subscription NOT RUN; API/UI tests pass |
| Pause/resume | Global delivery switch persists; pause stops delivery without deleting devices | `PushSettings.test.tsx`; Go delivery-destination tests | Covered by tests; real browser state PASS (paused badge) |
| Device test | Per-device and all-device tests show sent/partial/failed feedback with sanitized categories | `PushSettings.test.tsx`; Go sender tests | Covered by fake-endpoint tests; real Push NOT RUN |
| Remove device | Confirmation is not required for a single device, but removal is explicit; server row disables and disappears | `PushSettings.test.tsx`; Go API tests | Covered by tests; manual browser NOT RUN |
| Forget this browser | Browser unsubscribes locally and UI explains that the server record is cleaned up on 404/410 | Component code review; no browser test | NOT RUN — manual |
| Service worker display | Payload is bounded; title/body/tag display correctly; no secret in payload | `serviceWorker.test.ts`; real Chrome CDP `deliverPushMessage` | PASS — real Chrome displayed the synthetic payload; see `docs/BROWSER_PUSH_VALIDATION.md` |
| Click handling | Same-origin event detail or validated `https://mrbilit.com` booking URL opens; unsafe targets fall back | `serviceWorker.test.ts` executing real `sw.js` | PASS — VM logic tests; OS notification click NOT RUN |
| Subscription change | Open dashboard refreshes the server registration; closed browser relies on later 404/410 cleanup | `serviceWorker.test.ts`; component code | PASS — VM logic test; real browser rotation NOT RUN |
| Closed browser delivery | Browser fully closed still receives a Push and click opens the correct page | Not automatable here | NOT RUN — requires HTTPS + live Push service |
| iOS/PWA constraints | Document HTTPS/install/platform limitations before claiming support | README/architecture | Documented; real iOS/PWA NOT RUN |

## Responsive, theme, and accessibility

| Check | Expected | Automated coverage | Manual result |
|---|---|---|---|
| Mobile width (360–430 px) | Sidebar collapses; tables scroll; forms stack; actions remain reachable | CSS review | NOT RUN — manual |
| Desktop width | Sidebar layout and max-width content | CSS review | NOT RUN — manual |
| Light/dark/system themes | Toggle and persisted preference; no first-paint flash | Theme implementation | NOT RUN — manual |
| Keyboard only | Skip link, focus rings, tab order, dialog focus trapping, date picker operation | Radix primitives; manual review | NOT RUN — manual |
| Screen reader labels | Landmarks, labels, table headers, error descriptions, live regions | Semantic review | NOT RUN — manual |
| Color contrast | Status is conveyed by text/icon, not color alone | Component review | NOT RUN — manual |
| Reduced motion | Animations reduced when preferred | CSS review | NOT RUN — manual |
| 200% zoom | No clipped content or lost actions | Manual review | NOT RUN — manual |

## Automated verification reference

Run from the repository root:

```bash
cd web
npm run lint
npm run typecheck
npm test
npm run build
cd ..
go test -count=1 ./...
go test -race -count=1 ./...
go vet ./...
go build ./...
```

Optional accessibility automation (axe) is not included. A full browser E2E framework is not part of v1; this checklist is the manual companion to the Vitest/Testing Library suite.
