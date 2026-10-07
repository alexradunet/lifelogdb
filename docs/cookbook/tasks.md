# Personal tasks, occurrences and deadlines

These examples use the [planning profile](../contract/planning.md), the ordinary
[write connection](../contract/connections.md), and explicitly captured intent. Project context is an existing
plain page, created through the normal [page writer](capture.md). These SQL examples are executed against a fresh
file; the profile's vectors additionally exercise calendar and clock calculations through a writer.

Create a one-off and its single occurrence atomically. `:due_day` may be NULL. The returned IDs belong to separate
namespaces; neither is an entity ID.

```sql
BEGIN IMMEDIATE;
INSERT INTO tasks(label,project_page_id,created_at,updated_at,source)
VALUES (:task_label,:project_page_id,strftime('%Y-%m-%dT%H:%M:%fZ','now'),strftime('%Y-%m-%dT%H:%M:%fZ','now'),:source)
RETURNING id; -- retain as :planning_once_id
INSERT INTO task_occurrences(task_id,occurrence_key,due_day,created_at,updated_at,source)
VALUES (:planning_once_id,'once',:due_day,strftime('%Y-%m-%dT%H:%M:%fZ','now'),strftime('%Y-%m-%dT%H:%M:%fZ','now'),:source);
COMMIT;
```

Create a monthly definition without filling future rows. Bind `:planning_clock` to `09:00`; its default reminder means
09:00 in the selected zone
on each occurrence's current due day. The repeated label need not be a globally unique page name.

```sql
INSERT INTO tasks(label,project_page_id,repeat_unit,repeat_every,anchor_day,reminder_local_time,reminder_zone,created_at,updated_at,source)
VALUES (:task_label,:project_page_id,'month',1,'2026-01-31',:planning_clock,'Europe/Bucharest',strftime('%Y-%m-%dT%H:%M:%fZ','now'),strftime('%Y-%m-%dT%H:%M:%fZ','now'),:source)
RETURNING id; -- retain as :planning_task_id
```

Materialize February's slot before an edit or a durable delivery reference is needed. A retry finds the same
row and leaves its current deadline, outcome, overrides and lifecycle intact. The writer first reads/validates the
parent and key in this transaction; a retry of an existing row reads it before admission, since later shortening
or tombstoning can legitimately prevent a new insertion. Imports additionally compare their source-key binding
as the [planning profile](../contract/planning.md) requires.

```sql
BEGIN IMMEDIATE;
INSERT INTO task_occurrences(task_id,occurrence_key,due_day,created_at,updated_at,source)
SELECT :planning_task_id,'2026-02-28','2026-02-28',strftime('%Y-%m-%dT%H:%M:%fZ','now'),strftime('%Y-%m-%dT%H:%M:%fZ','now'),:source
WHERE NOT EXISTS (SELECT 1 FROM task_occurrences WHERE task_id=:planning_task_id AND occurrence_key='2026-02-28')
ON CONFLICT(task_id,occurrence_key) DO NOTHING;
SELECT id,revision,due_day,state,reminder_mode FROM task_occurrences
WHERE task_id=:planning_task_id AND occurrence_key='2026-02-28';
COMMIT;
```

Save a current outcome, deadline and reminder choice with both edit tokens. Bind `:planning_state='done'` with
`:planning_completed_at=NULL` for known completion at an unknown time. Reopen or skip with NULL completion evidence.
Reschedule by changing `:due_day`, including clearing it; the original key remains February's. Bind `inherit` or
`off` with a NULL override, or `at` with an explicit instant. Require exactly one matched row, otherwise roll back
and report a stale or unavailable record. Read fresh revisions after the update; trigger increments are not the
pre-trigger values of an UPDATE RETURNING result.

```sql
BEGIN IMMEDIATE;
UPDATE task_occurrences
SET due_day=:due_day,state=:planning_state,completed_at=:planning_completed_at,
    reminder_mode=:planning_reminder_mode,reminder_override_at=:planning_reminder_at
WHERE task_id=:planning_task_id AND occurrence_key='2026-02-28'
  AND revision=:planning_occurrence_version AND deleted_at IS NULL
  AND EXISTS (SELECT 1 FROM tasks WHERE id=:planning_task_id AND revision=:planning_task_version AND deleted_at IS NULL);
SELECT id,revision,due_day,state,completed_at,reminder_mode,reminder_override_at FROM task_occurrences
WHERE task_id=:planning_task_id AND occurrence_key='2026-02-28';
COMMIT;
```

Stop the series at an inclusive end. Check that the update matched exactly one definition. The end guard and
stop trigger apply the same operation to already-materialized future slots. Earlier slots moved to later deadlines
remain attached to their original keys.

```sql
BEGIN IMMEDIATE;
UPDATE tasks SET repeat_until_day=:planning_until
WHERE id=:planning_task_id AND revision=:planning_task_version AND deleted_at IS NULL;
SELECT id,revision,repeat_until_day FROM tasks WHERE id=:planning_task_id;
COMMIT;
```

The following bounded open-deadline query walks each calendar day in `:planning_from` through `:planning_through`
and applies the anchored profile. A writer validates this finite window first. Persisted rows suppress virtual
slots **before** deadline filtering; the second arm includes moved-in persisted rows and one-offs. `id=NULL` marks
a virtual slot. This straightforward query is a reference for bounded reads, not a requirement to scan individual
days in an application expander. It filters task tombstones, but retains tombstoned project context.

```sql
WITH RECURSIVE calendar(day) AS (
  SELECT :planning_from
  UNION ALL
  SELECT date(day,'+1 day') FROM calendar WHERE day<:planning_through
), slots AS (
  SELECT t.id AS task_id,c.day AS occurrence_key
  FROM tasks t CROSS JOIN calendar c
  WHERE t.id=:planning_task_id AND t.deleted_at IS NULL AND t.repeat_unit IS NOT NULL
    AND c.day>=t.anchor_day AND (t.repeat_until_day IS NULL OR c.day<=t.repeat_until_day)
    AND CASE t.repeat_unit
      WHEN 'day' THEN CAST(julianday(c.day)-julianday(t.anchor_day) AS INTEGER)%t.repeat_every=0
      WHEN 'week' THEN CAST(julianday(c.day)-julianday(t.anchor_day) AS INTEGER)%7=0
        AND (CAST(julianday(c.day)-julianday(t.anchor_day) AS INTEGER)/7)%t.repeat_every=0
      WHEN 'month' THEN ((CAST(substr(c.day,1,4) AS INTEGER)-CAST(substr(t.anchor_day,1,4) AS INTEGER))*12
        +CAST(substr(c.day,6,2) AS INTEGER)-CAST(substr(t.anchor_day,6,2) AS INTEGER))%t.repeat_every=0
      WHEN 'year' THEN (CAST(substr(c.day,1,4) AS INTEGER)-CAST(substr(t.anchor_day,1,4) AS INTEGER))%t.repeat_every=0
        AND substr(c.day,6,2)=substr(t.anchor_day,6,2)
    END
    AND (t.repeat_unit IN ('day','week') OR CAST(substr(c.day,9,2) AS INTEGER)=min(CAST(substr(t.anchor_day,9,2) AS INTEGER),
      CASE WHEN substr(c.day,6,2)='02' THEN 28+(CAST(substr(c.day,1,4) AS INTEGER)%4=0
        AND (CAST(substr(c.day,1,4) AS INTEGER)%100<>0 OR CAST(substr(c.day,1,4) AS INTEGER)%400=0))
      WHEN substr(c.day,6,2) IN ('04','06','09','11') THEN 30 ELSE 31 END))
), work AS (
  SELECT NULL AS id,s.task_id,s.occurrence_key,s.occurrence_key AS due_day,'inherit' AS reminder_mode,NULL AS reminder_override_at
  FROM slots s WHERE NOT EXISTS (
    SELECT 1 FROM task_occurrences o WHERE o.task_id=s.task_id AND o.occurrence_key=s.occurrence_key)
  UNION ALL
  SELECT o.id,o.task_id,o.occurrence_key,o.due_day,o.reminder_mode,o.reminder_override_at
  FROM task_occurrences o JOIN tasks t ON t.id=o.task_id
  WHERE t.id=:planning_task_id AND t.deleted_at IS NULL AND o.deleted_at IS NULL AND o.state='open'
    AND o.due_day BETWEEN :planning_from AND :planning_through
)
SELECT w.*,t.label,t.revision AS task_revision,t.reminder_local_time,t.reminder_zone,
       t.project_page_id,n.title AS project_title,p.deleted_at AS project_deleted_at
FROM work w JOIN tasks t ON t.id=w.task_id
LEFT JOIN entities p ON p.id=t.project_page_id
LEFT JOIN entity_names n ON n.entity_id=p.id AND n.name_key=p.preferred_name_key
ORDER BY w.due_day,w.task_id,w.occurrence_key;
```

Resolve the selected rows' reminder intent using the [clock vectors](../contract/planning.md#resolving-a-reminder-clock).
The query does not store a computed reminder timestamp or send notifications. Historical reads select persisted
outcomes with explicit task/occurrence tombstones, rather than inventing history for absent rows of deleted tasks.
