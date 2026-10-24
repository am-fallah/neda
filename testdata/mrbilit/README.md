# MrBilit fixtures

The `synthetic/` fixtures are **hand-written**. They are not raw MrBilit
responses. The real envelope shape was observed in two authorized, bounded GET
requests on 2026-10-24/25 (see `observed/README.md` and
`docs/MRBILIT_LIVE_VALIDATION.md`). The parser now accepts the observed
`trains[]` / `prices[].classes[]` envelope and keeps the synthetic top-level
array only for the original deterministic fixtures.

The synthetic contract is deliberately narrow:

- a top-level JSON array of train objects;
- train fields `id`, `fromName`, `toName`, `trainNumber`, `departureTime`,
  `arrivalTime`, `providerName`, `corporationName`, and `classes`;
- offering fields `id`, `price`, `capacity`, `wagonName`, `shortTitle`,
  `compartmentType`, `isAvailable`, `reservationAvailable`, `capacityString`,
  `ownerName`, and `wagonID`;
- `classes` is the only nested offering container the parser accepts.

The observed shape additionally contains `prices[]`, numeric IDs/times, and
`meta.routeNotFound`/`alwaysHasTrain`. See `observed/` for the sanitized
reconstruction; no raw live response, authorization header, cookie, or session
data is stored. Any other envelope is rejected as an invalid response rather
than interpreted as an empty result. Positive availability is covered by the
observed-shape fixture and the live validation record.
