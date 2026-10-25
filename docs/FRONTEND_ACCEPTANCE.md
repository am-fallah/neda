# Neda Dashboard — Manual Acceptance Checklist

**Status: checklist prepared and partially executed on 2026-10-24 with an automated Chrome/Playwright audit against a local development build using the fake provider.** The automated audit covered login, the unauthenticated redirect, wrong-password feedback, overview, theme persistence, monitor creation with the Jalali picker, a real reservable observation, pause/resume, history filtering, settings secret handling, mobile navigation, logout, skip-link keyboard focus, dialog Escape handling, and an axe WCAG 2 A/AA audit with zero violations on six screens. Screenshots were visually reviewed. Human-only cases and any flow requiring real Telegram credentials, Web Push, production providers, or a second device remain marked **NOT RUN — manual**. Do not use production providers, real Telegram credentials, or production infrastructure for this checklist unless separately authorized.

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

## New Monitor redesign audit (2026-10-25)

Environment: Vite development build, system Chrome through a temporary Playwright 1.64.1 install, private API and MrBilit-search responses mocked with deterministic fixtures. No backend database was read or changed; no live provider, Telegram, Push, or booking action occurred. Screenshots were reviewed at 1440×1000 and 390×844 in light and dark modes.

| Check | Result | Evidence |
|---|---|---|
| Initial search state | PASS | Two-column desktop layout; neutral page-scoped accent; search panel, empty summary, and global delivery status render without console/page errors. |
| URL passenger prefill | PASS | Complete valid `adultCount`/`childCount`/`infantCount` values prefill and are visibly marked as loaded from the URL; partial values show guidance and do not overwrite explicit counts. |
| Journey/class-family grouping | PASS | Strict unit tests cover same journey, repeated class names with different wagons/compartments, duplicate journey keys, and exact offering positions. Unavailable options remain selectable. |
| Exact selection and summary | PASS | Selecting two options updates the live count, label, selected list, and creation payload. The backend payload is unchanged in shape. |
| Stale-search invalidation | PASS | Changing the URL clears selections, disables results and creation, shows a "Search settings changed" notice, and an aborted in-flight response is ignored. |
| Loading state | PASS | Search button and skeleton status are announced; duplicate submission is guarded. |
| Empty result | PASS | "No trains returned" explicitly says this is not a sold-out signal; creation is unavailable. |
| Search error | PASS | Provider failure shows an actionable error without implying unavailability; URL errors focus the URL field. |
| Creation error | PASS | Summary-level error plus mapped selection field error; create action is re-enabled for retry. |
| Label preservation | PASS | A manually edited label survives a subsequent search; the generated label uses the URL slug to avoid bidi reordering. |
| Global delivery status | PASS | Polling interval is humanized; Telegram/Push state is read-only, labeled global, and links to Settings. |
| Desktop light/dark | PASS | Success screenshots reviewed; neutral black primary in light and inverted neutral primary in dark; status colors remain semantic. |
| Mobile 390 px | PASS | Search, grouped results, and summary stack cleanly; no horizontal overflow. Additional checks at 360 px and 320 px also pass. |
| Axe WCAG 2 A/AA + 2.1 A/AA | PASS | 0 violations on initial, selected success, stale, empty, and error states in light mode; selected success also passes in dark mode and at 390 px. |
| Console/page errors | PASS | No page errors and no unexpected console errors; the mocked provider-error scenario logged only the expected 502 resource status. |

Remaining visual acceptance for the owner: confirm the neutral page-scoped accent, the journey/class-family density, the Persian/English bidi presentation, and the summary rail at the owner's preferred viewport. Human screen-reader and zoom review remain manual.

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
