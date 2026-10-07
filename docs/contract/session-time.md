# Recorded session time evidence

The named `sessions` CHECKs/triggers in [the schema](../schema/schema.sql) hold its storage rules.
[D22](../decisions/D22-events.md) explains why these are lightweight records rather than titled events.

A session has an independently supplied exact reporting day and one kind reference to an existing plain
non-journal page. Names/aliases resolve to the stable kind ID. A renamed kind does not change retry identity.
New selections require a live kind; its later tombstone does not cascade to sessions or readings and stays
visible on historical/current references. No session title, graph endpoint, attendance or recurrence is implied.

## Endpoint profile

Exactly one UTC or unresolved local start is required. The end is optional, with at most one representation.
UTC uses [exact-time](exact-time.md)'s canonical milliseconds-Z profile. Local clocks use the same ASCII,
Gregorian0000–9999, hour00–23 and millisecond requirements, without a suffix: `YYYY-MM-DDTHH:MM:SS.SSS`.
Reporting day is not inferred from start/end; an associated reading's own day may differ.

Known offset claims are canonical ±HH:MM (hours 00–23, minutes 00–59), only on populated UTC endpoints.
`+00:00` is known zero; `-00:00` is not known zero and refuses, rather than being normalized. Do not impose
current civil-zone extrema as storage truth: `+14:01` and `+23:59` are representable claims. A known offset
with a local endpoint, dual UTC/local representations, or evidence without an endpoint refuses. A later
format-specific normalization must be explicit and lossless; this profile does not infer it.

`start_zone_unverified`/`end_zone_unverified` are bounded syntactic labels: **unverified supplied evidence**,
not validated IANA membership, offset agreement or an instruction to consult the host/current timezone.
They never change UTC, reporting day, ordering or period membership. If a source interpreter knows competing
claims conflict, it must expose/refuse for review, not hide the conflict under an unverified label.

UTC/UTC endpoints compare absolutely; reversed refuses, equality is valid. Exact elapsed milliseconds are
derived across the entire admitted calendar range using integer instant arithmetic, not saturated durations
or floating day subtraction. Elapsed span is not active time or time asleep. Local/local, mixed or missing
endpoints do not establish elapsed/absolute order. Descending fold clocks and gap clocks may be retained only
as unresolved evidence: never choose a fold or normalize a gap.

These canonical endpoint vectors are shared with storage validation (executed):

<!-- session-endpoint-vectors -->
```json
[
 {
  "start_at": "0000-02-29T00:00:00.000Z",
  "end_at": "9999-12-31T23:59:59.999Z",
  "accepted": true
 },
 {
  "start_at": "2020-01-01T23:00:00.000Z",
  "end_at": "2020-01-02T07:00:00.000Z",
  "accepted": true
 },
 {
  "start_at": "2020-01-01T00:00:00.000Z",
  "end_at": "2020-01-01T00:00:00.000Z",
  "accepted": true
 },
 {
  "start_at": "2020-01-01T00:00:00.001Z",
  "end_at": "2020-01-01T00:00:00.000Z",
  "accepted": false
 },
 {
  "start_local": "2020-11-01T01:50:00.000",
  "end_local": "2020-11-01T01:10:00.000",
  "start_zone_unverified": "America/New_York",
  "accepted": true
 },
 {
  "start_local": "2020-03-08T02:30:00.000",
  "start_zone_unverified": "America/New_York",
  "accepted": true
 },
 {
  "start_local": "2020-01-01T23:00:00.000",
  "end_at": "2020-01-02T07:00:00.000Z",
  "accepted": true
 },
 {
  "end_local": "2020-01-02T07:00:00.000",
  "accepted": false
 },
 {
  "start_at": "2020-01-01T00:00:00.000Z",
  "start_local": "2020-01-01T00:00:00.000",
  "accepted": false
 },
 {
  "start_local": "2020-01-01T00:00:00.000",
  "start_offset": "-05:00",
  "accepted": false
 },
 {
  "start_at": "2020-01-01T00:00:00.000Z",
  "start_offset": "+14:01",
  "accepted": true
 },
 {
  "start_at": "2020-01-01T00:00:00.000Z",
  "start_offset": "+23:59",
  "accepted": true
 },
 {
  "start_at": "2020-01-01T00:00:00.000Z",
  "start_offset": "+00:00",
  "accepted": true
 },
 {
  "start_at": "2020-01-01T00:00:00.000Z",
  "start_offset": "-00:00",
  "accepted": false
 },
 {
  "start_at": "2020-01-01T00:00:00.000Z",
  "start_offset": "+24:00",
  "accepted": false
 },
 {
  "start_at": "2020-01-01T00:00:00.000Z",
  "end_zone_unverified": "Claimed/Zone",
  "accepted": false
 },
 {
  "start_local": "2020-01-01T24:00:00.000",
  "accepted": false
 },
 {
  "start_local": "2019-02-29T00:00:00.000",
  "accepted": false
 },
 {
  "start_at": "2020-01-01T00:00:00.000Z",
  "end_at": "2020-01-02T00:00:00.000Z",
  "end_local": "2020-01-02T00:00:00.000",
  "accepted": false
 },
 {
  "start_at": "2020-01-01T00:00:00.000Z",
  "start_zone_unverified": "not a zone",
  "accepted": false
 },
 {
  "start_at": "2020-01-01T00:00:00.000Z",
  "end_at": "2020-01-02T00:00:00.000Z",
  "end_offset": "-00:00",
  "accepted": false
 },
 {
  "start_at": "2020-01-01T00:00:00.000Z",
  "end_local": "2019-02-29T00:00:00.000",
  "accepted": false
 },
 {
  "start_at": "2020-01-01T00:00:00.000Z",
  "end_at": "2020-01-01T01:00:00.000Z",
  "start_offset": "+00:00\u0000hidden",
  "accepted": false
 },
 {
  "start_at": "2020-01-01T00:00:00.000Z",
  "end_at": "2020-01-01T01:00:00.000Z",
  "end_offset": "+00:00\u0000hidden",
  "accepted": false
 },
 {
  "start_at": "2020-01-01T00:00:00.000Z",
  "end_at": "2020-01-01T01:00:00.000Z",
  "start_zone_unverified": "UTC\u0000hidden",
  "accepted": false
 },
 {
  "start_at": "2020-01-01T00:00:00.000Z",
  "end_at": "2020-01-01T01:00:00.000Z",
  "end_zone_unverified": "UTC\u0000hidden",
  "accepted": false
 },
 {
  "start_at": "2020-01-01T00:00:00.000Z",
  "end_at": "2020-01-01T01:00:00.000Z",
  "source": "ui\u0000hidden",
  "accepted": false
 }
]
```

## Identity, edits and attribution

A source key is lossless TEXT, never a numeric/float conversion. Same source/key and semantically identical
canonical payload on a live session returns the same unchanged identity; changed payload or tombstone conflicts.
Compare kind IDs, not preferred spelling. Different sources remain distinct. Metadata and lifecycle mutations
use the exact-string revision inside the write transaction; no-ops/rollback preserve counters and exhaustion refuses.
No hard delete or stored edit history; snapshots retain prior contents.

For [period](period-boundaries.md) comparisons, explicitly choose reporting-day attribution; it is not proof
of physical interval overlap/location. [Measurement scope](measurement-scope.md) distinguishes associated
summaries from unassociated daily/point facts without generating another daily total.
