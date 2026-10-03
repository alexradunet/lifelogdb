# Metrics by category: file a metric, nest a category, a category's metrics, every metric grouped (D26)

A category is a page, and so is a metric ([D26](../decisions/D26-metric-categories.md), [D27](../decisions/D27-a-metric-is-a-page.md)): anything is
filed in a category by a `part-of` link to the category's page. `:metric_id` is a metric, `:page_id` a category's page,
`:parent_id` the page of the category it belongs to; `:source` is the writer.

```sql
-- file the metric :metric_id in the category :page_id; a re-run inserts nothing (unlink to take it out)
INSERT INTO links(from_id, to_id, kind, created_at, source)
VALUES (:metric_id, :page_id, 'part-of', strftime('%Y-%m-%dT%H:%M:%fZ','now'), :source)
ON CONFLICT(from_id, to_id, kind) DO NOTHING;

-- nest the category :page_id in :parent_id (Lipids part-of Biomarkers), the same link between two pages
INSERT INTO links(from_id, to_id, kind, created_at, source)
VALUES (:page_id, :parent_id, 'part-of', strftime('%Y-%m-%dT%H:%M:%fZ','now'), :source)
ON CONFLICT(from_id, to_id, kind) DO NOTHING;

-- the metrics filed in the category :parent_id and in every category under it, at any depth; UNION, not
-- UNION ALL: part-of links can close a cycle, and a page met twice ends the walk
WITH RECURSIVE tree(id) AS (
  SELECT :parent_id
  UNION
  SELECT l.from_id FROM links l JOIN tree t ON l.to_id = t.id AND l.kind = 'part-of'
)
SELECT c.title AS category, mp.title AS metric, m.unit
  FROM tree t
  JOIN pages c     ON c.id = t.id
  JOIN entities ce ON ce.id = t.id AND ce.deleted_at IS NULL
  JOIN links l     ON l.to_id = t.id AND l.kind = 'part-of'
  JOIN metrics m   ON m.id = l.from_id
  JOIN pages mp    ON mp.id = m.id
  JOIN entities me ON me.id = m.id AND me.deleted_at IS NULL
 ORDER BY c.title_key, mp.title_key;

-- every metric with each category it is filed in ('' = none) and whether it is a habit: the habits first,
-- then by category, the metrics filed nowhere last; a metric in two categories is listed under each
SELECT EXISTS (SELECT 1 FROM habit_periods h WHERE h.metric_id = m.id) AS habit,
       coalesce(c.title, '') AS category, mp.title AS metric, m.unit
  FROM metrics m
  JOIN pages mp    ON mp.id = m.id
  JOIN entities me ON me.id = m.id AND me.deleted_at IS NULL
  LEFT JOIN (SELECT l.from_id, p.title, p.title_key
               FROM links l JOIN pages p ON p.id = l.to_id
               JOIN entities e ON e.id = p.id AND e.deleted_at IS NULL
              WHERE l.kind = 'part-of') c ON c.from_id = m.id
 ORDER BY habit DESC, c.title_key IS NULL, c.title_key, mp.title_key;
```

A category's page is an ordinary page: its body is what the owner writes about the category, and a `[[Lipids]]` in
the journal is one of its backlinks ([backlinks](backlinks.md)). The same link files anything else in a category — a
page, a person, a place. A habit is not a category: it is a metric with a period ([habits](habits.md)), and it may be
filed in a category as well. Renaming a category's page moves the `part-of` links that end at it
([rename a page](rename-a-page.md)). Nothing keeps the `part-of` links a tree: a page may belong to two categories,
and links may close a cycle, as `located-in` may (D8); a writer that draws the tree follows one parent and stops at a
page it has seen.
