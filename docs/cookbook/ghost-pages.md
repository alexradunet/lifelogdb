# Ghost pages (the wikilink sweep, D5)

```sql
SELECT id, title, created_at FROM ghost_pages ORDER BY created_at;
```

The view ([schema](../schema/README.md)) lists empty plain pages without retained references, 30 days old; the UI surfaces it as a cleanup
list. Graph links, session-kind and task-project references, and measurement capture provenance all keep a page out, including
tombstoned sessions/tasks and corrected or retracted measurements. The sources are capture-time typos and a mention later edited out of a body (step 4 of [save a body](save-a-body.md)
drops its link, the empty page stays).
