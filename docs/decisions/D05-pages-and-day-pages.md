# D5 — One prose body per identity; the journal is a page per day; names are owned aliases.

**Status:** accepted

- **Decision.** A page is a named, linkable identity with its prose in `entities` and its spellings in `entity_names`; people, places, metrics and files use the same prose home. See [titles and wikilinks](../contract/titles-and-wikilinks.md) for the shared predicate and save grammar.
  - The journal is one canonical identity per local day (`entities_day_page`, `entities_day_page_plain`, `entities_day_identity`, `entity_names_day_insert`, `entity_names_day_update`). Capture appends to that identity ([capture](../cookbook/capture.md)); dated ordinary pages remain distinct.
  - Dated is a property, not a type: a link target normally has no day; a page deliberately written has the day its owner gives it ([day view](../cookbook/day-view.md)).
  - Tags and explicit references are read from prose and resolve owned names to ids; tags are never expanded into rewritten body text.
  - Stable-id rename selects an owned preferred spelling and retains old names ([rename a page](../cookbook/rename-a-page.md)). `entity_names_fixed` and `entity_names_no_delete` reserve ownership; `entity_names_title_safe` supports portable, reference-addressable handles. A rename never merges another owner's ghost or tombstone.
- **Why.** The first vault import exposed two homes for a day: an untitled journal entry and the empty date page made by a reference. One canonical journal identity removes that ambiguity. Later name corrections exposed the cost of permanent titles and redirect identities: moving prose and incident links changes the identity of the thing named. Owned aliases preserve old references without rewriting decades of prose.
- **Alternatives.**
  - Untitled memos or separate journal/note/wiki prose stores: rejected — a day must be linkable and prose has one home.
  - A journal page with a free title and unique day: rejected — it introduces a second resolution rule for date references.
  - Redirect identities and chains: rejected — names belong directly to the same identity; ordinary `#REDIRECT` text has no special state.
  - Rename into another owner's name, even an empty ghost: rejected — rename is not merge.
  - ASCII-only uniqueness or an application collation: rejected — Unicode names need the shared normalization, and a stored index must remain usable without a private collation (executed).
  - Id-named handles without filename checks: rejected — relaxing a restriction later is possible; tightening it after freeze can meet existing incompatible names ([D13](D13-migrations-and-freeze.md)).
- **Costs accepted.** A day-page entry has no separate timestamp; a time worth keeping stays in prose. There is no inbox. Retained names consume the global namespace, including after tombstoning. Empty unreferenced pages appear in `ghost_pages` for the owner's review.
- **Sources.** Kaydet [R41](../research/references.md#r41); FxLifeSheet [R9](../research/references.md#r9)[R42](../research/references.md#r42); Windows reserved names [R58](../research/references.md#r58); Unicode security [R63](../research/references.md#r63).
