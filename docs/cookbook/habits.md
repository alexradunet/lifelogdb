# Habits: start and stop one, the habits of a day, completion over a period (D24)

`:metric` is the `title_key` of a unitless metric (a metric is a page, [D27](../decisions/D27-a-metric-is-a-page.md)); a check-in is a measurement of it, 1 or 0 ([correct a measurement](correct-a-measurement.md) corrects one).

```sql
-- start the habit on :day; no end yet
INSERT INTO habit_periods(metric_id, start_day, source)
SELECT id, :day, 'ui' FROM pages WHERE title_key = :metric AND entity_type = 'metric';

-- stop it: the open period ends on :day
UPDATE habit_periods SET end_day = :day
 WHERE metric_id = (SELECT id FROM pages WHERE title_key = :metric AND entity_type = 'metric') AND end_day IS NULL;

-- the habits of :day: done, not done, or not recorded
SELECT m.title,
       CASE (SELECT max(v.value) FROM measurement_values v WHERE v.metric_id = m.id AND v.day = :day)
         WHEN 1 THEN 'done' WHEN 0 THEN 'not done' ELSE 'not recorded' END AS state
  FROM habit_periods h JOIN pages m ON m.id = h.metric_id
 WHERE h.start_day <= :day AND coalesce(h.end_day, '9999-12-31') >= :day
 ORDER BY m.title_key;

-- completion between :from_day and :to_day, per habit: the days it was active, and of those the
-- days done, not done and not recorded (a rate is done / (done + not done))
WITH RECURSIVE days(day) AS (
  SELECT :from_day
  UNION ALL
  SELECT date(day, '+1 day') FROM days WHERE day < :to_day
),
active AS (
  SELECT h.metric_id, d.day
    FROM days d JOIN habit_periods h ON h.start_day <= d.day AND coalesce(h.end_day, '9999-12-31') >= d.day
)
SELECT m.title, count(*) AS active_days,
       sum(s.value IS 1) AS done, sum(s.value IS 0) AS not_done, sum(s.value IS NULL) AS not_recorded
  FROM active a
  JOIN pages m ON m.id = a.metric_id
  LEFT JOIN (SELECT metric_id, day, max(value) AS value FROM measurement_values GROUP BY metric_id, day) s
         ON s.metric_id = a.metric_id AND s.day = a.day
 GROUP BY m.id
 ORDER BY m.title_key;
```

A day outside every period is not a habit day at all: it is in no count. Two check-ins on one day
count once (the higher wins). A re-sent period is idempotent: insert it with
`ON CONFLICT(metric_id, start_day) DO NOTHING`, carrying the `end_day` it was sent with — the
insert trigger fires first, so a genuine overlap still raises, and a closed period re-sent without
its `end_day` is an open one that overlaps any later period of the habit (executed) — then apply a
changed `end_day` with an UPDATE of the row at that metric and start day; periods are keyed by the
owner's data, not by a sender's `import_key` ([D24](../decisions/D24-habits.md)).
