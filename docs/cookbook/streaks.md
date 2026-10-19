# The streak: days written in a row

A streak is the count of day pages with words in them, one for each day, that follow each other and end today. If
today has no words yet, the count ends yesterday: the day is not over, so it does not break the streak. If neither
today nor yesterday has words, the streak is 0.

```sql
-- :today is the local day of the reader, 'YYYY-MM-DD'. The columns: the streak, and whether today has words.
WITH RECURSIVE
  start(day) AS (
    SELECT CASE WHEN EXISTS (SELECT 1 FROM entities p WHERE p.day = :today AND p.is_journal AND p.deleted_at IS NULL AND trim(p.body, ' ' || char(9,10,13)) <> '')
                THEN :today ELSE date(:today, '-1 day') END),
  run(day, n) AS (
    SELECT s.day, 1 FROM start s
     WHERE EXISTS (SELECT 1 FROM entities p WHERE p.day = s.day AND p.is_journal AND p.deleted_at IS NULL AND trim(p.body, ' ' || char(9,10,13)) <> '')
    UNION ALL
    SELECT date(r.day, '-1 day'), r.n + 1 FROM run r
     WHERE r.n < 3660
       AND EXISTS (SELECT 1 FROM entities p WHERE p.day = date(r.day, '-1 day') AND p.is_journal AND p.deleted_at IS NULL AND trim(p.body, ' ' || char(9,10,13)) <> ''))
SELECT coalesce(max(n), 0) AS streak,
       EXISTS (SELECT 1 FROM entities p WHERE p.day = :today AND p.is_journal AND p.deleted_at IS NULL AND trim(p.body, ' ' || char(9,10,13)) <> '') AS written_today
  FROM run;
```

- `is_journal` is the stored column of the day page ([D5](../decisions/D05-pages-and-day-pages.md)): the query reads it
  and never works out again what a day page is.
- The query looks up one day page at a time, by its day, going back from today. Its cost follows the streak, not the size of the record. It stops at 3660 days (about ten years), so a longer streak reads as 3660.
- A day page whose body is only spaces, tabs or line breaks is not written. It does not count.
- A tombstoned day page does not count ([D11](../decisions/D11-tombstones.md)).
- A day page of a future day does not count: the query only goes back from `:today`.
- A habit has no streak here. Habits have no periods ([D24](../decisions/D24-habits.md)).
