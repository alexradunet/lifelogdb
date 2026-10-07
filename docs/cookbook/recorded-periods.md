# Read recorded periods

```sql
SELECT p.id,n.title,p.start_boundary,p.end_boundary,e.deleted_at
FROM periods p JOIN entities e ON e.id=p.id
JOIN entity_names n ON n.entity_id=e.id AND n.name_key=e.preferred_name_key
WHERE e.deleted_at IS NULL
ORDER BY e.preferred_name_key,e.id;
```

Apply the [boundary profile](../contract/period-boundaries.md) with an explicit day and ongoing evaluation
horizon; return outside/definite/possible/incomparable separately, alongside original boundary values.
This day-attribution query is not proof of physical interval overlap or location. Historical access may
include tombstoned identities explicitly; their boundaries, names and readings are not deleted.
