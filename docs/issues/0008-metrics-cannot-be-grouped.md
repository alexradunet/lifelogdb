# 0008 — The metrics cannot be grouped: a hundred lab values, a drink count and mood in one list

- **Date:** 2026-10-03
- **Status:** proposed
- **Seen in:** the writer's metrics list, after the lab-result imports into the canonical `life.db`

## What happened

The lab-result imports registered about a hundred metrics, one per lab value, beside the owner's own
series. The metrics list of the writing application is one alphabetical run of every row of `metrics`:
`albumin`, `alcohol_drinks`, `alkaline_phosphatase`, … `mood`, … `weed_smoke_grams`. The owner asked for the
list in groups — habits, biomarkers, what was consumed — and the data cannot answer "which metrics are
biomarkers?" or "what do I take in?": a metric has a name, a unit and a note, and nothing else sets one apart.
Only habits can be told apart, by their periods ([D24](../decisions/D24-habits.md)). The list grows with every lab
import.

## Reproduce

On a fresh database built from [schema.sql](../schema/schema.sql):

```sql
INSERT INTO metrics(name, unit) VALUES ('ldl_cholesterol', 'mg/dL'), ('ferritin', 'ng/mL'),
  ('alcohol_drinks', 'drinks'), ('coffee_cups', 'cups');
-- which of these are biomarkers, and which are things the owner consumed?
SELECT name, unit, note FROM metrics ORDER BY name;   -- nothing in the row says
```

## Rules involved

- [D7](../decisions/D07-measurements.md): `metrics` is a registry of name, unit and note.
- [D24](../decisions/D24-habits.md): a habit is a metric with periods; a `kind` column was rejected for that.

## Resolution

Proposed in [0001](../rfcs/0001-metric-categories.md).
