# The days that name someone or somewhere

"When did I see Ana?", "when was I at Lakeside?": the day pages that name the person, place or page,
newest first — by a `[[wikilink]]` in their text, or by an `about` link a writer added where the text
names them without brackets (an imported note). A day page is the page whose title is its day ([titles and wikilinks](../contract/titles-and-wikilinks.md)).
The days the owner was *at* a place are its `at` links ([where was I](where-was-i.md)); for the days anywhere inside a place
(Tokyo in Japan), walk `located-in` first ([inside a place](inside-a-place.md)).
Owned aliases resolve directly to the same entity id ([titles and wikilinks](../contract/titles-and-wikilinks.md)); a rename does not change these links.

```sql
SELECT DISTINCT d.day, substr(d.body, 1, 60) AS start
  FROM links l
  JOIN entities d ON d.id = l.from_id AND d.preferred_name_key = d.day AND d.deleted_at IS NULL
 WHERE l.to_id = :entity_id
   AND l.kind IN ('wikilink', 'about')
 ORDER BY d.day DESC;
```
