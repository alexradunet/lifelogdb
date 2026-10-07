# Full-text search

The derived FTS index uses `unicode61 remove_diacritics 2`: searches ignore Latin diacritics, including
letters bearing more than one diacritic. This search tokenization differs from exact
[name-key resolution](../contract/titles-and-wikilinks.md), which uses the pinned Unicode normalization and
full case-folding contract. For example, the name `Straße` resolves through the key `strasse`, but an FTS
query for `strasse` does not match the token `Straße`; query `Straße` itself. A missing search hit therefore
does not establish that an exact name is absent. The FTS tokenizer does not change identity or merge names.

```sql
SELECT e.id, n.title,
       snippet(entities_fts, 2, '<b>', '</b>', '…', 24) AS ctx
  FROM entities_fts
  JOIN entities e ON e.id = entities_fts.rowid AND e.deleted_at IS NULL
  JOIN entity_names n ON n.entity_id = e.id AND n.name_key = e.preferred_name_key
 WHERE entities_fts MATCH :query
 ORDER BY rank;
```
