# Measurement session scope

`measurements.session_id` explicitly associates a fact with a recorded session; `captured_with_id` remains
capture provenance. NULL scope means **unassociated**, not "daily total". The metric and attribution distinguish
daily totals from point quantities; shared units never justify combining different metrics or scopes.
Session totals never generate another daily total or stored period membership.

The [schema](../schema/schema.sql)'s append-only and same-metric/same-scope correction triggers protect chains.
Value corrections copy scope and reading attribution, never a session's current reporting day or kind spelling.
Source/key/metric uniqueness is unchanged: adding session to uniqueness would silently duplicate a moved source fact.
The cross-table lifecycle rule is `lifelog_meta.measurement_scope`.

`measurement_values` remains the unfiltered non-retracted leaf view. Active application queries exclude facts
associated with tombstoned sessions. Explicit historical/include-deleted queries and individual measurement audits
retain original scope/lifecycle. Session edits/tombstones never rewrite or retract facts; revival exposes the same
current IDs. New associated non-NULL values require a live session; NULL retraction remains possible after its tombstone.
Ordinary metric lifecycle rules remain unchanged.

Series default to unassociated; explicit session or labeled all-scope selections keep scope visible without summing.
Daily habit/Mood capture uses unassociated facts; session-scoped values are not daily check-ins. When comparing a
session to [periods](period-boundaries.md), name reporting-day attribution explicitly, not physical interval overlap.

## Wrong-scope correction

An owner may relocate a current non-retracted **ordinary unkeyed** leaf to a different scope, retaining metric
identity. Validate replacement value/day/instant/zone/provenance and live destination before writes. In one immediate
transaction append a NULL retraction in the old chain and a new independent root with explicit replacement attribution.
Return both IDs; no supersedes edge crosses scope. Refuse same scope, stale/retracted/missing leaves and invalid
replacement atomically. Cancellation or failure after the first insert rolls back both.

Imported/keyed chains are unavailable for relocation, including an imported/keyed ancestor whose owner-authored leaf
has no key. Existing imported value correction/recovery remains separate and preserves scope. Relocation does not reinterpret source identity or cross a correction chain into another scope. Agents cannot run owner relocation.

For a synthetic day with unassociated 10000 Steps and session 4000/2000 Steps, the default series returns 10000 only;
a labeled all-scope query returns three facts, **not 16000 daily steps**. Correcting 4000 to 3500 retains its session.
Relocation retracts that old leaf and adds 3500 under the destination, without changing the 10000 unassociated fact.
