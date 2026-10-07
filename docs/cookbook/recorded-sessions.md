# Recorded sessions and labeled readings

```sql
SELECT s.id,n.title AS kind,s.day AS reporting_day,s.start_at,s.start_local,
       s.end_at,s.end_local,s.start_offset,s.end_offset,
       s.start_zone_unverified,s.end_zone_unverified,s.deleted_at,e.deleted_at AS kind_deleted_at
FROM sessions s JOIN entities e ON e.id=s.kind_id
JOIN entity_names n ON n.entity_id=e.id AND n.name_key=e.preferred_name_key
WHERE :include_deleted=1 OR s.deleted_at IS NULL
ORDER BY s.day,s.id;
```

This order is reporting day then stable ID, not an absolute timeline of unresolved/mixed clocks.
[Endpoint evidence](../contract/session-time.md) never guesses host timezones. The reporting day can be used
with the [period profile](../contract/period-boundaries.md) only as named day attribution, not physical overlap.

```sql
SELECT me.id,me.day,me.value,me.session_id,
       CASE WHEN me.session_id IS NULL THEN 'unassociated' ELSE 'session' END AS scope,
       e.deleted_at AS metric_deleted_at,
       s.deleted_at AS session_deleted_at
FROM measurement_values me
JOIN entities e ON e.id=me.metric_id
JOIN entity_names n ON n.entity_id=me.metric_id AND n.name_key=:metric
LEFT JOIN sessions s ON s.id=me.session_id
WHERE :include_deleted=1 OR (e.deleted_at IS NULL AND (me.session_id IS NULL OR s.deleted_at IS NULL))
ORDER BY me.day,me.taken_at,me.id;
```

This is an explicit labeled all-scope query, not a daily total or a sum. For a single session add `me.session_id`
as an explicit filter; the ordinary [metric series](metric-series.md) selects unassociated facts only.
Historical queries retain [scope and lifecycle](../contract/measurement-scope.md) rather than erasing facts.
Here `:include_deleted=1` includes facts of deleted metrics and deleted sessions and labels both current tombstones.
