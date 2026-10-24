# MrBilit fixtures

Everything in this directory is **synthetic**. It is not an observed MrBilit
response. The real response envelope, offering-container key, time/price
formats, and capacity semantics remain unverified until the owner approves and
runs the bounded live-validation protocol (`GATE-003` / `PRV-006`).

The parser's synthetic contract is deliberately narrow:

- a top-level JSON array of train objects;
- train fields `id`, `fromName`, `toName`, `trainNumber`, `departureTime`,
  `arrivalTime`, `providerName`, `corporationName`, and `classes`;
- offering fields `id`, `price`, `capacity`, `wagonName`, `shortTitle`,
  `compartmentType`, `isAvailable`, `reservationAvailable`, `capacityString`,
  `ownerName`, and `wagonID`;
- `classes` is the only nested offering container the parser accepts.

These names come from the illustrative field list in the brief, but the
container shape is not evidence of the production contract. Any other envelope
is rejected as an invalid response rather than interpreted as an empty result.
Positive availability fixtures are synthetic; no observed positive sample is
claimed.
