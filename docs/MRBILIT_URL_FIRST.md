# MrBilit URL-first monitor creation

**Status:** Implemented for local/non-production use. Production registration remains deferred (`PRV-009`).

## Owner flow

1. Open `http://localhost:<port>/monitors/new`.
2. Paste a public MrBilit train search URL, for example:
   `https://mrbilit.com/trains/yazd-mashhad?departureDate=1405-08-24`
3. Set passenger counts and choose **Find trains**.
4. Review normalized trains/classes and select one or more exact options.
5. Confirm the monitor label, route, date, passengers, global polling interval,
   and Telegram/Push status.
6. Choose **Create monitor**.

The URL is search input only. It is never treated as proof of availability.
Only the selected train/class options are tracked.

### New Monitor UI behavior (2026-10-25)

- **Passenger prefill is conservative and PROVISIONAL.** The frontend reads
  `adultCount`, `childCount`, and `infantCount` only when all three are present
  once, are valid integers, and satisfy the existing adult/child/infant and
  total-passenger bounds. Partial or invalid values are never guessed; the
  explicit fields remain authoritative. Counts embedded in the resolved
  MrBilit page are not used because they may be provider defaults. The search
  request still sends the explicit passenger counts.
- **Changing the URL or passenger counts invalidates the previous search.**
  In-flight responses are aborted and ignored through a revision guard, prior
  selections are cleared, stale results are disabled, and creation is blocked
  until a successful new search.
- **Grouping is display-only.** Journeys remain distinct by `journeyKey`.
  Classes are grouped under a strict wagon/class/compartment signature; each
  distinct `offeringKey` remains an individually selectable option. Repeated
  class names with different provider identity are not merged, and there is no
  group-level select-all control.
- **The generated label uses the URL route slug plus the Gregorian date** to
  avoid bidi reordering in the text input. The summary still displays the
  provider's route names and both Jalali and Gregorian dates. A manually edited
  label is preserved across later searches.
- **Polling and notification settings are global.** The summary displays their
  current status and links to Settings; it does not add per-monitor controls.
  The backend search and monitor-create contracts are unchanged.

## Local setup

```bash
# Development registers MrBilit automatically; no extra enable flag.
NEDA_FAKE_PROVIDER_ENABLED=true make run
```

Open `http://localhost:8080/monitors/new` (or the configured local URL),
sign in, and use the MrBilit search card. The production registry remains empty
until `PRV-009`.

## Backend flow

- `POST /api/v1/providers/mrbilit/search`
  - validates the exact `https://mrbilit.com/trains/<slug>?departureDate=<Jalali date>` shape;
  - ignores all other query parameters, including `utm_*`;
  - converts the Jalali date to Gregorian;
  - resolves numeric route IDs from the public page's embedded Nuxt
    `urlParams.originCode` / `destinationCode`;
  - fetches the initial result through the existing MrBilit adapter;
  - returns normalized trains/classes without writing monitor state.
- `POST /api/v1/monitors` persists route IDs, slug, date, passengers, and the
  selected option identities/metadata in `monitor_provider_selections`, tied to
  the provider target's config version.
- Scheduled checks continue to query the whole route. A deterministic filter
  reduces each successful response to the selected options only.

## Matching rules

- Exact `journeyKey` + `offeringKey` wins, subject to metadata guardrails.
- A unique fallback match is allowed using train number/date/time/operator and
  wagon/class attributes. The selected state keys are retained.
- Missing, ambiguous, conflicting, or reused identities become `Unknown`.
- API errors, timeouts, and malformed responses never update availability.
- Empty `trains[]` is a successful empty result; selected options become
  `Unknown` and open episodes are not closed.
- Unselected classes never create inventory states, events, or deliveries.
- Existing episode dedupe prevents duplicate events/deliveries.

## Safety boundaries

- No booking or reservation requests.
- No authentication headers, cookies, session IDs, or tokens are sent to
  MrBilit.
- Page/API requests are read-only GETs, redirect-free, body-capped, and
  bounded.
- Production keeps the provider registry empty.
- Tests use sanitized fixtures and mock servers; no real Telegram or Push
  notification is sent by automated tests.

## Local validation commands

The implementation checks are recorded in the task tracker and final PR report.

## Live validation evidence (2026-10-25)

- Target: `https://mrbilit.com/trains/yazd-mashhad?departureDate=1405-08-24`
  with ignored `utm_source=neda-test`.
- The request resolved to route IDs `219` → `191`, date `1405-08-24`
  (`2026-11-15`), and returned HTTP 200 through the private local search
  endpoint.
- Normalized result: 1 train (`682`), 6 classes, 2 Reservable (capacity 38
  and 2), 4 NotReservable (capacity 0), 0 Unknown, no warnings.
- The returned route display names were the upstream Persian names; the UI
  displays them as returned.
- One earlier page request used the wrong default page host
  (`train.mrbilit.com`) and returned a 404. The default was corrected to
  `https://mrbilit.com` and the successful page/API run above followed.
- Total public read-only GETs used for this session's inspection and
  validation: five (two inspection GETs, one failed page GET, one successful
  page GET, one successful API GET). No auth headers, cookies, session IDs, or
  tokens were sent. No retry was made after a failure. No booking request was
  made.
- No live monitor was created. No Telegram or Push notification was sent.

## Unresolved API assumptions

- The public page's embedded `urlParams` is not a documented API contract; the
  resolver fails closed if the page format changes.
- Native train/class ID stability is unknown; fallback matching is conservative.
- Price currency remains unverified, so price is omitted.
- Optional API parameters (`genderCode`, `exclusive`, `availableStatus`) remain
  omitted until verified.
- Capacity semantics for children/infants remain provisional.
