# Metrics by category: file a metric, a category's subtree, every metric grouped (D26)

`:category` is a registered category; `:subcategory` is registered under it ([D26](../decisions/D26-metric-categories.md)).

```sql
-- register :subcategory under :category (re-run: nothing; a writer first checks that an existing
-- :subcategory has this parent, since a parent never changes)
INSERT INTO metric_categories(name, parent_id)
SELECT :subcategory, id FROM metric_categories WHERE name = :category
ON CONFLICT(name) DO NOTHING;

-- file :metric in it (re-filing is the same UPDATE; NULL un-files it)
UPDATE metrics SET category_id = (SELECT id FROM metric_categories WHERE name = :subcategory)
 WHERE name = :metric;

-- the metrics of :category and of every category under it, at any depth
WITH RECURSIVE tree(id, path) AS (
  SELECT id, name FROM metric_categories WHERE name = :category
  UNION ALL
  SELECT c.id, t.path || '/' || c.name FROM metric_categories c JOIN tree t ON c.parent_id = t.id
)
SELECT t.path, m.name, m.unit
  FROM tree t JOIN metrics m ON m.category_id = t.id
 ORDER BY t.path, m.name;

-- every metric with the path of its category ('' = not filed) and whether it is a habit:
-- the habits first, then by category, the metrics not filed last
WITH RECURSIVE tree(id, path) AS (
  SELECT id, name FROM metric_categories WHERE parent_id IS NULL
  UNION ALL
  SELECT c.id, t.path || '/' || c.name FROM metric_categories c JOIN tree t ON c.parent_id = t.id
)
SELECT EXISTS (SELECT 1 FROM habit_periods h WHERE h.metric_id = m.id) AS habit,
       coalesce(t.path, '') AS category, m.name, m.unit
  FROM metrics m LEFT JOIN tree t ON t.id = m.category_id
 ORDER BY habit DESC, category = '', category, m.name;
```

A habit is not a category: it is a metric with a period ([habits](habits.md)), so it is found through
`habit_periods`, and it can be filed in a category as well. The path is computed, never stored:
renaming a category renames every path under it.
