# The streak: days written in a row

A streak is the count of day pages with words in them, one for each day, that follow each other and end today. If
today has no words yet, the count ends yesterday: the day is not over, so it does not break the streak. If neither
today nor yesterday has words, the streak is 0.

```sql
-- :today is the local day of the reader, 'YYYY-MM-DD'. The columns: the streak, and whether today has words.
WITH RECURSIVE
  written(day) AS (
    SELECT p.day FROM entities p
     WHERE p.is_journal AND p.deleted_at IS NULL AND p.body <> ''
       AND p.day > date(:today, '-3660 day') AND p.day <= :today),
  start(day) AS (
    SELECT CASE WHEN EXISTS (SELECT 1 FROM written WHERE day = :today) THEN :today ELSE date(:today, '-1 day') END),
  run(day, n) AS (
    SELECT s.day, 1 FROM start s WHERE s.day IN (SELECT day FROM written)
    UNION ALL
    SELECT date(r.day, '-1 day'), r.n + 1 FROM run r
     WHERE date(r.day, '-1 day') IN (SELECT day FROM written) AND r.n < 3660)
SELECT coalesce(max(n), 0) AS streak,
       EXISTS (SELECT 1 FROM written WHERE day = :today) AS written_today
  FROM run;
```

- `is_journal` is the stored column of the day page ([D5](../decisions/D05-pages-and-day-pages.md)): the query reads it
  and never works out again what a day page is.
- The query looks back at most 3660 days (about ten years), so its cost has a limit. A longer streak reads as 3660.
- A day page with no words (an empty body) does not count as written.
- A tombstoned day page does not count ([D11](../decisions/D11-tombstones.md)).
- A day page of a future day does not count: the window ends at `:today`.
- A habit has no streak here. Habits have no periods ([D24](../decisions/D24-habits.md)).
