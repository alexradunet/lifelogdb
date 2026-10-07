# Recorded period boundaries

[The schema](../schema/schema.sql)'s `periods` constraints hold the storage rules; [D22](../decisions/D22-events.md)
explains recorded spans. Period bodies, names and optional exact entity day retain their ordinary meanings.
Classify periods with existing `part-of` categories; no primary kind is required. Overlap is allowed.
A new category selection names a live, non-journal plain page. Its later tombstone retains the existing edge.

## Boundary interpretation

The supported profile is ASCII `YYYY`, `YYYY-MM` or `YYYY-MM-DD`, years 0000–9999, optionally followed by one
whole-boundary `?`, `~` or `%`. The nominal calendar value must be valid even when qualified. This borrows
spellings, not general EDTF support: no seasons, sets or expanded years. NULL means unknown, for either endpoint;
`..` means ongoing only as an end. Unknown is not ongoing. Both endpoints may be unknown.

An unqualified year/month names an unknown endpoint day within that calendar bucket. Derived first/last days
are comparison bounds, never observations. Qualified boundaries have no usable numerical range in this
conservative profile: approximation/uncertainty supplies no tolerance. Keep explanatory wording in the body.
Reject only definitely reversed finite unqualified spans. An exact final day is inclusive.

## Day membership

Results are `outside`, `definite`, `possible` or `incomparable`; do not count incomparable as a definite match
or silently omit it from a response claiming all relevant periods. The basis is a supplied calendar day,
not physical interval overlap, geographic presence or causal evidence.

For finite unqualified start range [Smin,Smax] and end range [Emin,Emax], consider only endpoint pairs with
start ≤ end. Refuse if Smin > Emax. Outside if day < Smin or day > Emax; otherwise definite when
min(Smax,Emax) ≤ day ≤ max(Emin,Smin), possible otherwise. This conditions on valid ordering without inventing dates.
Unknown/qualified endpoints give incomparable unless the known opposite bound proves outside.

Ongoing queries require an explicit exact `as_of`, echoed by the response. It is the evaluation horizon for the
current ongoing assertion, not an observed end, historical database snapshot or observation of continued activity.
Do not prune possible starts using that horizon. A known start proves outside before Smin. After the horizon,
otherwise incomparable. Through the horizon, an unqualified start is definite at/after Smax, possible between
Smin and Smax; unknown/qualified start remains incomparable. Never substitute today's date silently.

These shared vectors exercise the writer and canonical validation (executed):

<!-- period-membership-vectors -->
```json
[
 {"start":"2018-09","end":"2019-06","day":"2018-09-01","as_of":"","result":"possible"},
 {"start":"2018-09","end":"2019-06","day":"2018-09-30","as_of":"","result":"definite"},
 {"start":"2018-09-20","end":"2018-09","day":"2018-09-20","as_of":"","result":"definite"},
 {"start":"2018-09","end":"..","day":"2018-09-15","as_of":"2018-09-15","result":"possible"},
 {"start":"2018-09","end":"..","day":"2018-09-15","as_of":"2020-12-31","result":"possible"},
 {"start":"2018-09","end":"2018-09-15","day":"2018-09-15","as_of":"","result":"definite"},
 {"start":"2018","end":"..","day":"2018-06-15","as_of":"2018-06-15","result":"possible"},
 {"start":"2018-09","end":"..","day":"2018-09-30","as_of":"2018-09-30","result":"definite"},
 {"start":"2021-01","end":"..","day":"2020-12-31","as_of":"2020-12-31","result":"outside"},
 {"start":"2018-09","end":"..","day":"2021-01-01","as_of":"2020-12-31","result":"incomparable"},
 {"start":null,"end":null,"day":"2019-01-01","as_of":"","result":"incomparable"},
 {"start":"2019~","end":"2020-01-01","day":"2020-01-02","as_of":"","result":"outside"}
]
```

[Recorded-period query](../cookbook/recorded-periods.md) returns original boundaries, not derived observations.
[Exact time](exact-time.md) continues to govern ordinary days and write instants.

Raw boundary storage vectors (executed; NUL suffixes are not canonical ASCII):

<!-- period-storage-vectors -->
```json
[
 {
  "column": "start_boundary",
  "boundary": "2018?",
  "accepted": true
 },
 {
  "column": "start_boundary",
  "boundary": "2018?\u0000hidden",
  "accepted": false
 },
 {
  "column": "start_boundary",
  "boundary": "2018-09~",
  "accepted": true
 },
 {
  "column": "start_boundary",
  "boundary": "2018-09~\u0000hidden",
  "accepted": false
 },
 {
  "column": "start_boundary",
  "boundary": "2018-09-15%",
  "accepted": true
 },
 {
  "column": "start_boundary",
  "boundary": "2018-09-15%\u0000hidden",
  "accepted": false
 },
 {
  "column": "end_boundary",
  "boundary": "2018?",
  "accepted": true
 },
 {
  "column": "end_boundary",
  "boundary": "2018?\u0000hidden",
  "accepted": false
 },
 {
  "column": "end_boundary",
  "boundary": "2018-09~",
  "accepted": true
 },
 {
  "column": "end_boundary",
  "boundary": "2018-09~\u0000hidden",
  "accepted": false
 },
 {
  "column": "end_boundary",
  "boundary": "2018-09-15%",
  "accepted": true
 },
 {
  "column": "end_boundary",
  "boundary": "2018-09-15%\u0000hidden",
  "accepted": false
 }
]
```

Category selection vectors (executed through the writer):

<!-- period-category-vectors -->
```json
[{"deleted":false,"accepted":true},{"deleted":true,"accepted":false}]
```
