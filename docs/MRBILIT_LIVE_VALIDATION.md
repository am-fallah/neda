# MrBilit Bounded Live Validation — Evidence

**Date:** 2026-10-24/25 UTC
**Tasks:** `GATE-003`, `PRV-006`; supports `PRV-007`
**Authorization:** Owner explicitly authorized bounded live requests to the
public MrBilit service for provider validation. No booking, reservation, or
purchase request was made.

## Protocol actually used

- Route: Yazd (`from=219`) → Mashhad (`to=191`)
- Date: `2026-11-05` (the owner reference date; Jalali `1405-08-14`)
- Passengers: 1 adult, 0 children, 0 infants
- Endpoint: `https://train.mrbilit.com/api/GetAvailable/v2`
- Read-only GET only; no credentials, bearer token, cookies, player ID, or
  session ID; no auth bypass.
- No redirects (`--max-redirs 0` / adapter `CheckRedirect` rejects).
- No retries (`--retry 0` / adapter `NoRetries: true`).
- Two requests total (budget was five), separated by more than three minutes.
- No booking-page fetch; the reference URL was supplied by the owner.

### Request 1 — minimal public request

Started `2026-10-24T23:06:36Z`, completed `2026-10-24T23:06:37Z`.

```text
GET /api/GetAvailable/v2?from=219&to=191&date=2026-11-05&adultCount=1&childCount=0&infantCount=0
```

Observed: HTTP 200, `application/json; charset=utf-8`, 14,078 bytes, 0.348 s.
No redirect. No authentication was required. `genderCode`, `exclusive`, and
`availableStatus` were deliberately omitted because the minimal documented
request succeeded and their enum semantics remain unverified.

Sanitized shape:

- top-level object;
- `trains` length 1;
- `prices` length 3, each with two `classes`;
- `meta.routeNotFound=false`, `meta.alwaysHasTrain=false`;
- train-level `id`, `trainNumber` numeric; `from`/`to` numeric;
  `departureTime=2026-11-05T14:50:00`, `arrivalTime=2026-11-06T06:30:00`
  (local wall-clock, no offset);
- class fields `capacity`, `capacityString`, `isAvailable`,
  `reservationAvailable`, `price`, `wagonID`, `shortName`, and display labels.

### Request 2 — existing adapter

Started `2026-10-24T23:09:46.425Z`, completed `2026-10-24T23:09:47.470Z`
(1,044 ms). The adapter used the same endpoint, route, date, and passenger
count with `NoRetries: true` and a 20 s caller deadline. No auth headers or
cookies were sent.

Observed normalized result:

```text
result=success observed_at=2026-10-24T23:09:47.468885Z
journeys=1 offerings=6 reservable=2 not_reservable=4 unknown=0
booking_urls=6 warnings=[]
```

Per-offering classification:

| Offering | Capacity | isAvailable | reservationAvailable | State | Reason |
|---|---:|---|---|---|---|
| 1 | 0 | false | true | NotReservable | `capacity_zero_unavailable` |
| 2 | 3 | true | true | Reservable | `positive_capacity_and_flags` |
| 3 | 0 | false | true | NotReservable | `capacity_zero_unavailable` |
| 4 | 0 | false | true | NotReservable | `capacity_zero_unavailable` |
| 5 | 2 | true | true | Reservable | `positive_capacity_and_flags` |
| 6 | 0 | false | true | NotReservable | `capacity_zero_unavailable` |

All six received the validated booking URL
`https://mrbilit.com/trains/yazd-mashhad?departureDate=1405-08-14`.

## Findings and semantics

1. **A valid positive class can establish Reservable.** Two classes with
   positive capacity, `isAvailable=true`, and `reservationAvailable=true`
   classified as Reservable for the requested one adult. This is observed
   positive evidence for the conservative rule, not a claim that every
   positive-looking class is bookable.
2. **`reservationAvailable=true` alone is not sufficient.** Four classes had
   `reservationAvailable=true` with capacity 0 and `isAvailable=false`; all
   classified NotReservable via the known-negative rule.
3. **`meta.routeNotFound` is decisive route information.** It was `false` in
   this sample. The parser maps `true` to a typed `route_not_found` provider
   error, never to NotReservable.
4. **Empty `trains[]` is an empty observation, not a sell-out verdict.** Empty
   lists are handled as successful empty results (no offering rows, no episode
   closure). `routeNotFound=true` remains an error.
5. **`alwaysHasTrain` remains unverified.** It was `false` here. Its true
   meaning is not inferred; the parser emits a bounded warning when true and
   never uses it for availability state.
6. **Capacity shortfall stays Unknown.** For a larger requested party (for
   example 4 adults), the observed capacity 2/3 classes are below the requested
   count and classify Unknown, not NotReservable. This is tested with the
   sanitized observed-shape fixture.
7. **Times are local Tehran wall-clock values without an offset.** The parser
   interprets the observed `YYYY-MM-DDTHH:MM:SS` shape in `Asia/Tehran` and
   stores UTC; the live journey had both departure and arrival timestamps.
8. **Price currency is not verified.** The live `price` fields are numeric, but
   Rial vs Toman, per-passenger basis, and `sellType` semantics are not
   established. The parser preserves a positive numeric amount as
   `Offering.Price.Amount` with an empty currency; zero, missing, negative,
   fractional, and malformed values stay absent. The dashboard shows the
   provider amount with an explicit qualification; notifications omit price
   until a currency is validated.
9. **The booking URL is verified by the owner reference.** The route slug is
   manually configured (`yazd-mashhad`); it is never derived from city names.
   The date conversion fixture is `2026-11-05` ↔ `1405-08-14`.

## Public page vs API

The owner reference URL is `https://mrbilit.com/trains/yazd-mashhad?departureDate=1405-08-14`.
The API returned one train and multiple class/price groups for the same
Gregorian date. No booking page was fetched. The API's `dateString`/`weekday`
display fields were not used for canonical dates; the Gregorian request date and
parsed timestamps are authoritative.

## Limitations

- One route/date and one point in time; inventory is transient.
- One adult only; capacity semantics for multi-passenger parties are tested with
  a fixture, not live.
- No live empty-list, route-not-found, malformed, auth-required, or rate-limit
  case was observed; those are covered by deterministic tests.
- No `genderCode`, `exclusive`, or `availableStatus` values were used; their
  semantics remain unverified.
- No booking/reservation/purchase request and no booking-page fetch.
- The provider remains unregistered in the production registry (`PRV-009`
  requires a separate decision).

## URL-first local search validation (2026-10-25)

The local URL-first search path was validated separately from scheduled
monitoring with the fixed public example:

- URL:
  `https://mrbilit.com/trains/yazd-mashhad?departureDate=1405-08-24&utm_source=neda-test`
- Resolved route: Yazd `219` → Mashhad `191`
- Resolved date: `1405-08-24` (`2026-11-15`)
- The private local search endpoint returned HTTP 200 and a normalized result:
  1 train (`682`), 6 classes, 2 Reservable (capacity 38 and 2), 4
  NotReservable (capacity 0), 0 Unknown, no warnings.
- Route display names were the upstream Persian names.
- One earlier page request used the wrong default page host
  (`train.mrbilit.com`) and returned 404; the default was corrected to
  `https://mrbilit.com`.
- Total public read-only GETs for the session inspection and validation: five
  (two inspection GETs, one failed page GET, one successful page GET, one
  successful API GET). No auth headers, cookies, session IDs, tokens, retries,
  booking requests, live monitors, Telegram messages, or Push messages were
  used.
- No live monitor was created; scheduled selected-option behavior is covered
  by deterministic fixture integration tests.
