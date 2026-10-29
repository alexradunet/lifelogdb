# The days that name someone or somewhere

"When did I mention Ana or Lakeside?": the day pages that name the person, the place or another page,
newest first — by a `[[wikilink]]` in their text. A day page is the page whose title is its day ([titles and wikilinks](../contract/titles-and-wikilinks.md)).
Mentioning someone or somewhere does not establish an encounter or physical presence.
The words of a day page say where the owner was: `I was at [[Lakeside]]` ([D16](../decisions/D16-places.md)). The days at a place are the days that name it, read by the words.
Owned aliases resolve directly to the same entity id ([titles and wikilinks](../contract/titles-and-wikilinks.md)); a rename does not change these links.

```sql
SELECT DISTINCT d.day, substr(d.body, 1, 60) AS start
  FROM links l
  JOIN entities d ON d.id = l.from_id AND d.preferred_name_key = d.day AND d.deleted_at IS NULL
 WHERE l.to_id = :entity_id
   AND l.kind = 'wikilink'
 ORDER BY d.day DESC;
```
