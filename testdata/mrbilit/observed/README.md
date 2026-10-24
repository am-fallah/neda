# MrBilit observed-shape fixtures

`sanitized-live-2026-11-05.json` is a **sanitized reconstruction** of the
envelope shape returned by one authorized, unauthenticated live GET on
2026-10-24/25. It is not a raw production response:

- route IDs, city names, date, and carrier names match the approved validation
  target;
- train/class/wagon numeric IDs and prices were replaced with stable synthetic
  values;
- Persian display strings were replaced with English equivalents;
- no authorization headers, cookies, session IDs, player IDs, or player data are
  present;
- no booking or reservation request was made.

Observed live shape (sanitized counts only): HTTP 200, top-level object,
`trains` length 1, `prices` length 3, two `classes` per price group, and
`meta.routeNotFound=false`. The first price group contained the known-negative
class (`capacity=0`, `isAvailable=false`, `reservationAvailable=true`) and a
positive class (`capacity=3`, `isAvailable=true`,
`reservationAvailable=true`). The other groups contained zero-capacity classes
and one additional positive class (`capacity=2`).

The date pairing was confirmed from the owner reference:
`2026-11-05` ↔ `1405-08-14`, with the route slug `yazd-mashhad`.

These values are evidence for parser/classification tests, not a claim about
the current live inventory.
