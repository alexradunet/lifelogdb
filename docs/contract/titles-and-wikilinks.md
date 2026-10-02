# Prose, wikilinks and titles

`pages.body` is CommonMark text. Wiki references are written inline as `[[Page Title]]`; a `#tag`
is read as `[[tag]]`, so tags are just pages ([D5](../decisions/D05-pages-and-day-pages.md)). The app never rewrites the body: `#health` stays
`#health` in the database.

**The save contract ([D19](../decisions/D19-wikilink-save-contract.md)).** Saving a page body is one `BEGIN IMMEDIATE` transaction ([save a body](../cookbook/save-a-body.md)): the
body, then the page's `links(kind='wikilink')` rows, made **equal to the set of pages the body
names** — missing rows added, rows the body no longer supports deleted — so a re-save changes nothing
and every link can be rebuilt from the bodies alone. The `links` table is the source of truth for the
graph; the body text is the source of truth for prose. The rules, all executed against the vectors
below:

- *What is read.* The CommonMark **text** of `pages.body`, after NFC normalisation — not code
  spans, code blocks, raw HTML, link destinations or image alt text. A conformant CommonMark parser
  yields exactly this, so nothing is hand-parsed (the reference implementation in `tests/` uses
  markdown-it-py [R59](../research/references.md#r59), which loses a code span that follows an unclosed `[`, where the CommonMark
  reference implementation keeps it).
- *Wikilink.* `[[title]]` or `[[title|alias]]`, with no `[`, `]` or line break inside, and the
  brackets and title in **one** run of plain text (`[[Health *Diet*]]` is not a link, and
  `[[Diet]](url)` is a Markdown link). The title is the text before the first `|`, trimmed of
  spaces (U+0020 only, like the DDL's `trim`); the alias is display text the database ignores.
  There is no `#anchor` form (`#` is an ordinary title character, so `[[C#]]` works) and no
  escape (`\[[x]]` still links; write a literal `[[x]]` in a code span). Nothing else is
  normalised: `[[Health  Diet]]` with two spaces is a different page from `[[Health Diet]]`.
- *Tag.* `#` followed by words of letters, marks, digits and `_` (Unicode categories L, M, N)
  joined by single `-`, where the `#` is **not** glued to a preceding such character, `/` or `#`
  — so `C#`, `a#b`, `http://x/#frag` and `##x` are not tags — and the word is not all digits
  (`#12` and `#2024` are not tags, `#2024-review` is). Wikilinks are read first and their text
  is not re-read for tags (`[[Project #alpha]]` is one title). `# Heading` (with a space) is a
  heading; `#Heading` is not a CommonMark heading, so it is a tag. `#Health` and `#health` are
  one page.
- *Stub pages.* A body that starts with `#REDIRECT [[` (any case, leading whitespace allowed) is
  a rename stub (below): it gets no wikilinks and no tags — its one edge is the `redirect` link
  the app writes — so a stub never shows up as a backlink. Elsewhere the word `#redirect` alone
  is never a tag, so nothing can create a page called `redirect`.
- *An invalid target makes no link and never blocks a save.* A title `pages_title_safe` rejects
  (`[[Health/Diet]]`, `[[Re: plan]]`, the tag `#con`) is skipped. A writer checks the title rules
  before inserting — the reference predicate in `tests/` agrees with the DDL's CHECKs on more than
  40 000 generated strings — and creates each target inside its own `SAVEPOINT` ([save a body](../cookbook/save-a-body.md)), so even a
  target the predicate wrongly let through is rolled back alone: the page is saved and no orphan
  `entities` row is left. The UI reports skipped targets; nothing is stored about them.
- *A page never links to itself* (`[[Diet]]` inside the page `Diet` is ignored), and *a
  tombstoned target is revived*, not duplicated: the unique index covers tombstoned pages, so the
  save un-tombstones the page it resolves — any save that names it, an old day page edited years
  later included, so the UI tells the owner.
- *Named pages.* A person's or a place's page is a page like any other, so `[[Bob
  Sample]]` is an ordinary wikilink and nothing in the save contract knows about people ([D20](../decisions/D20-named-pages.md)).
- *Known limits.* A body that also defines a reference (`[Ref]: http://r`) turns `[[Ref]]` into
  a Markdown link; a `#` written as an entity (`&#35;x`) is decoded before the scan and counts as
  a tag; a typo (`[[Sm]]`) makes a ghost page like any other ([ghost pages](../cookbook/ghost-pages.md)).

Test vectors — every writer must reproduce them (`\n`, `́`, `̈` stand for a line
break and combining marks; a body is shown in a code span):

| body | links to (`title`s, in order) |
|---|---|
| `See [[Diet plan]].` | `Diet plan` |
| `[[Diet plan\|my diet]]` | `Diet plan` |
| `[[  Diet plan  ]]` | `Diet plan` |
| `[[C#]] and [[Page#Section]]` | `C#`, `Page#Section` |
| `[[Health/Diet]]` | — |
| `[[Re: plan]]` | — |
| `[[]] [[ ]] [[\|alias]]` | — |
| `[[Café]] [[CAFÉ]] [[Café]] [[cafe]]` | `Café`, `cafe` |
| `[[Café notes]]` | `Café notes` |
| `\[[escaped]]` | `escaped` |
| `` text `[[code]]` text `` | — |
| `a\n\n~~~\n[[fence]]\n~~~\n\nb` | — |
| `[[Diet]](http://y)` | — |
| `[[Health *Diet*]]` | — |
| `[[Ref]]\n\n[Ref]: http://r` | — |
| `[[CON]] [[nul]] [[Com1]] [[CONSOLE]] [[COM10]] [[LPT0]]` | `CONSOLE`, `COM10`, `LPT0` |
| `[[CON.backup]] [[nul.txt]] [[COM¹]] [[LPT².x]] [[a.CON]] [[CONSOLE.txt]]` | `a.CON`, `CONSOLE.txt` |
| `Feeling good #health today` | `health` |
| `#Health and #health and #HEALTH` | `Health` |
| `#tag. #tag2, (#paren) "#quoted" #end-` | `tag`, `tag2`, `paren`, `quoted`, `end` |
| `#café #zürich` | `café`, `zürich` |
| `# Heading\n\n## Sub\n\n### Sub sub` | — |
| `#Heading` | `Heading` |
| `I write C# and F# and a#b` | — |
| `[a](http://x/#frag) <http://x/#auto> http://x/#bare` | — |
| `issue #12 and #2024 but #2024-review` | `2024-review` |
| `[[Project #alpha]] #beta [[#gamma]]` | `Project #alpha`, `beta`, `#gamma` |
| `#con #nul #console` | `console` |
| `#REDIRECT [[New Title]]` | — |
| `see #REDIRECT [[New Title]]` | `New Title` |

(Also: a 240-byte title is a link, a 241-byte one is not; 80 × `日` = 240 bytes is, 81 is not.)

**Renames.** A title never changes (`pages_title_fixed`, [D5](../decisions/D05-pages-and-day-pages.md)). To fix one: create the new page, make
the old page a one-line stub (`#REDIRECT [[New Title]]`) and add `links(kind='redirect', from=old,
to=new)`. The stub is a plain page; the replacement may be a page, a person or a place, so a ghost
made by a misspelt `[[Name]]` can point at the person, and a replacement promoted later ([a person or a place](../cookbook/person-or-place.md)) keeps
its redirect. A person's or a place's own title is its permanent handle and is not renamed (a person's
display name is `people.name`, [D20](../decisions/D20-named-pages.md)). Consumers follow one hop — the days that name someone and backlinks count a stub's mentions as its replacement's
([the days that name someone](../cookbook/days-that-name.md), [backlinks](../cookbook/backlinks.md)); the `redirect` row itself is never a backlink.

**Titles.** The rules are the CHECKs `pages_title_len` and `pages_title_safe` ([schema](../schema/README.md)): 1–240 bytes,
trimmed, and a valid file name on Linux, macOS and Windows — the strict direction on purpose ([D5](../decisions/D05-pages-and-day-pages.md)).
Every writer must be stricter than the DDL in one way: it also rejects code points Unicode has not
assigned yet (category `Cn`), whose case fold a later Unicode version could define — which would
silently change `title_key` (executed). Unicode promises a stable case fold only for assigned characters, and formally
only for text in NFKC form [R74](../research/references.md#r74); a title with a compatibility character (full-width `Ｃａｆé`, a
ligature, `x²`) is outside that promise, a known limit ([non-goals](../architecture/non-goals.md)).

**`title_key`.** Uniqueness is on the key, not on the title. The function is fixed:
`title_key = NFC(casefold(NFC(title)))` — in Python
`unicodedata.normalize('NFC', unicodedata.normalize('NFC', t).casefold())`. A writer in another
language must reproduce these vectors exactly:

| title | `title_key` |
|---|---|
| `Café notes`, `Café notes` (NFD), `CAFÉ NOTES` | `café notes` |
| `Straße`, `STRASSE` | `strasse` |
| `ΣΑΣ`, `σας` (final sigma) | `σασ` |
| `Ǆ` | `ǆ` |
| `ﬁle` (ligature) | `file` |
| `İstanbul` | `i̇stanbul` (`i` + U+0307) |
| `日本語 ノート`, `Diet` | `日本語 ノート`, `diet` |

The database verifies what it can (`pages_key_*`: trimmed, no ASCII capitals, `lower(title)` for a
pure-ASCII title); that a non-ASCII key is the *right* fold is the writing application's duty
(principle 3) — a writer that computes it wrongly gets uniqueness wrong and nothing else. Resolve a
`[[wikilink]]` with `WHERE title_key = :key`, a search on the unique index `pages_title` (executed).

**Day pages.** The journal is one page per local day, titled with that day: `2026-09-29`. Its `day`
is its title (`pages_day_page`), so a day page is the page whose title equals its day, and its key is
its title (a pure-ASCII title). Capture appends to today's page and creates it on the first write
([capture](../cookbook/capture.md)); `[[2026-09-29]]` reaches it like any other title, and a link that names a day before anything
was written that day creates that day's page, empty ([save a body](../cookbook/save-a-body.md)). A day page is an ordinary page in every
other way: its `[[links]]` say who the day was with and where ([the days that name someone](../cookbook/days-that-name.md)).
