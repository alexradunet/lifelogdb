# Prose, wikilinks and titles

`entities.body` is CommonMark text. Wiki references are written inline as `[[Page Title]]`; a `#tag`
is read as `[[tag]]`, so tags are just pages ([D5](../decisions/D05-pages-and-day-pages.md)). The app never rewrites the body: `#health` stays
`#health` in the database.

**The save contract ([D19](../decisions/D19-wikilink-save-contract.md)).** Saving a page body is one `BEGIN IMMEDIATE` transaction ([save a body](../cookbook/save-a-body.md)): the
body, then the page's `links(kind='wikilink')` rows, made **equal to the set of pages the body
names** — missing rows added, rows the body no longer supports deleted — so a re-save changes nothing
and every link can be rebuilt from the bodies alone. The `links` table is the source of truth for the
graph; the body text is the source of truth for prose. The rules, all executed against the vectors
below:

- *What is read.* The CommonMark **text** of `entities.body`: the stored body is parsed as it is, and
  each run of text is then NFC-normalised — so a character whose NFC form is CommonMark syntax
  (U+1FEF becomes a backtick) never acts as syntax. Not read: code spans, code blocks, raw HTML,
  link destinations or image alt text. A conformant CommonMark parser
  yields exactly this, so nothing is hand-parsed (the suites in `tests/` read with goldmark
  [R59](../research/references.md#r59)).
- *Wikilink.* `[[title]]` or `[[title|alias]]`, with no `[`, `]` or line break (LF or CR) inside — U+2028 and U+2029 are ordinary characters — and the
  brackets and title in **one** run of plain text (`[[Health *Diet*]]` is not a link, and
  `[[Diet]](url)` is a Markdown link). The title is the text before the first `|`, trimmed of
  spaces (U+0020 only, like the DDL's `trim`); the alias is display text the database ignores.
  There is no `#anchor` form (`#` is an ordinary title character, so `[[C#]]` works) and no
  escape (`\[[x]]` still links; write a literal `[[x]]` in a code span). Nothing else is
  normalised: `[[Health  Diet]]` with two spaces is a different page from `[[Health Diet]]`.
- *Tag.* `#` followed by words of letters, marks, digits and `_` (Unicode categories L, M, N)
  joined by single `-`, where the `#` is **not** glued to a preceding such character, `/` or `#`
  — so `C#`, `a#b`, `http://x/#frag` and `##x` are not tags — and the tag is not made only of decimal
  digits (category Nd): `#12` and `#2024` are not tags; `#2024-review`, `#2026-09-29` (which
  names that day's page) and `#½` are. Wikilinks are read first and their text
  is not re-read for tags (`[[Project #alpha]]` is one title). `# Heading` (with a space) is a
  heading; `#Heading` is not a CommonMark heading, so it is a tag. `#Health` and `#health` are
  one page.
- *No redirect marker.* `#REDIRECT` is ordinary text/tag syntax; no body prefix suppresses extraction.
- *An invalid target makes no link and never blocks a save.* A title `entity_names_title_safe` rejects
  (`[[Health/Diet]]`, `[[Re: plan]]`, the tag `#con`) is skipped. Targets are checked first and then de-duplicated by `title_key`, so the first **valid** spelling of a key is the one linked and created. A writer checks the title rules
  before inserting — a writer's predicate is a subset of the DDL's accepted spellings, checked on more than
  40 000 generated strings; SQLite cannot parse the addressability forms — and creates each target inside its own `SAVEPOINT` ([save a body](../cookbook/save-a-body.md)), so even a
  target the predicate wrongly let through is rolled back alone: the page is saved and no orphan
  `entities` row is left. The UI reports skipped targets; nothing is stored about them.
- *A page never links to itself* (`[[Diet]]` inside the page `Diet` is ignored), and *a
  tombstoned target is revived*, not duplicated: the unique index covers tombstoned pages, so the
  save un-tombstones the page it resolves — any save that names it, an old day page edited years
  later included, so the UI tells the owner.
- *Named pages.* A person's, a place's, a metric's or a file's page is a page like any other, so `[[Bob
  Sample]]` is an ordinary wikilink and nothing in the save contract knows about people ([D20](../decisions/D20-named-pages.md)).
- *Embeds.* `![[Lake.jpg]]` is the wikilink `[[Lake.jpg]]` with a `!` before it: the `!` is ordinary text, so the
  link is the same row. Showing the picture of a file page there ([D9](../decisions/D09-binary-files.md)) is a
  reader's business; the database stores the text as written.
- *Known limits.* A body that also defines a reference (`[Ref]: http://r`) turns `[[Ref]]` into
  a Markdown link; a `#` written as an entity (`&#35;x`) is decoded before the scan and counts as
  a tag; a typo (`[[Sm]]`) makes a ghost page like any other ([ghost pages](../cookbook/ghost-pages.md)).

Test vectors — every writer must reproduce them. A body is shown in a code span; `\n`, `\r` and
`\t` stand for a line feed, a carriage return and a tab, `\uXXXX` for that code point, and `\|` for `|`.

| body | links to (`title`s, in order) |
|---|---|
| `See [[Diet plan]].` | `Diet plan` |
| `[[Diet plan\|my diet]]` | `Diet plan` |
| `[[  Diet plan  ]]` | `Diet plan` |
| `[[C#]] and [[Page#Section]]` | `C#`, `Page#Section` |
| `[[Health/Diet]]` | — |
| `[[Re: plan]]` | — |
| `[[]] [[ ]] [[\|alias]]` | — |
| `[[multi\nline]]` | — |
| `[[Café]] [[CAFÉ]] [[Cafe\u0301]] [[cafe]]` | `Café`, `cafe` |
| `[[Diet]] [[diet]]` | `Diet` |
| `[[nested [[x]] y]]` | `x` |
| `\[[escaped]]` | `escaped` |
| `` text `[[code]]` text `` | — |
| `a\n\n~~~\n[[fence]]\n~~~\n\nb` | — |
| `` a\n\n```\n[[fence]]\n#fencetag\n```\n\nb `` | — |
| `a\n\n    [[indented]]\n\nb` | — |
| `<span>[[html]]</span> <!-- [[cm]] -->` | `html` |
| `![alt [[img]]](u.png)` | — |
| `![[Lake.jpg]] and ![[Lake.jpg\|a lake]]` | `Lake.jpg` |
| `!![[A]] x![[B]]` | `A`, `B` |
| `[see [[Diet]]](http://x)` | `Diet` |
| `[[Diet]](http://y)` | — |
| `[[Health *Diet*]]` | — |
| `[[Ref]]\n\n[Ref]: http://r` | — |
| `[[.hidden]] [[trail.]] [[a.b]]` | `a.b` |
| `[[CON]] [[nul]] [[Com1]] [[CONSOLE]] [[COM10]] [[LPT0]]` | `CONSOLE`, `COM10`, `LPT0` |
| `[[CON.backup]] [[nul.txt]] [[COM¹]] [[LPT².x]] [[a.CON]] [[CONSOLE.txt]]` | `a.CON`, `CONSOLE.txt` |
| `[[tab\there]]` | — |
| `# Plan for [[Diet plan\|it]]` | `Diet plan` |
| `Feeling good #health today` | `health` |
| `#Health and #health and #HEALTH` | `Health` |
| `#日本語 #zürich #a_b-c` | `日本語`, `zürich`, `a_b-c` |
| `#tag. #tag2, (#paren) "#quoted" #end-` | `tag`, `tag2`, `paren`, `quoted`, `end` |
| `line\n#second\n#third` | `second`, `third` |
| `# Heading\n\n## Sub\n\n### Sub sub` | — |
| `#Heading` | `Heading` |
| `I write C# and F# and a#b` | — |
| `[a](http://x/#frag) <http://x/#auto> http://x/#bare` | — |
| `issue #12 and #2024 but #2024-review` | `2024-review` |
| `##tag` | — |
| `# and # alone #` | — |
| `` `#code` and #real `` | `real` |
| `[[Project #alpha]] #beta [[#gamma]]` | `Project #alpha`, `beta`, `#gamma` |
| `#con #nul #console` | `console` |
| `#work/project` | `work` |
| `#cafe\u0301 #zu\u0308rich` | `café`, `zürich` |
| `#हिन्दी and #ひらがな` | `हिन्दी`, `ひらがな` |
| `e\u0301#tag` | — |
| `[[Cafe\u0301 notes]]` | `Café notes` |
| `#REDIRECT [[New Title]]` | `REDIRECT`, `New Title` |
| `  #redirect  [[New Title]]\nmore #tag` | `redirect`, `New Title`, `tag` |
| `see #REDIRECT [[New Title]]` | `REDIRECT`, `New Title` |
| `#redirectors are fun` | `redirectors` |
| `#2026-09-29 and #2024-12` | `2026-09-29`, `2024-12` |
| `#½ #² #Ⅻ` | `½`, `²`, `Ⅻ` |
| `#REDIRECT\n[[x]]` | `REDIRECT`, `x` |
| `#REDIRECT\t[[x]]` | `REDIRECT`, `x` |
| `\u00a0#REDIRECT [[x]]` | `REDIRECT`, `x` |
| `#REDIRECT[[x]]` | `REDIRECT`, `x` |
| `\u1fef[[x]]\u1fef` | `x` |
| `[[a\u2028b]]` | `a\u2028b` |
| `[[a\rb]]` | — |
| `[[Café]] [[CAFÉ]] [[Café]] [[cafe]]` | `Café`, `cafe` |
| `[[Café notes]]` | `Café notes` |
| `#café #zürich` | `café`, `zürich` |

(Also: a 240-byte title is a link, a 241-byte one is not; 80 × `日` = 240 bytes is, 81 is not; `[[ẞ…]] [[ss…]]` with each spelling 81 times links `ss…`: the first is 243 bytes, invalid, and dropped before de-duplication.)

**Renames.** Select a new preferred spelling on the same identity in one transaction
([rename a page](../cookbook/rename-a-page.md)). Previous names remain direct aliases;
body, day, provenance, typed details, readings and every incoming/outgoing link keep their identities.
Names are reserved even for ghosts and tombstones: another owner's key is refused, never merged.
Case-only spelling changes and selecting an already-owned alias are supported; selecting the current
spelling is a no-op. A deleted identity must first be revived. A canonical journal identity has only
its date name and cannot rename; other dated notes/files remain aliasable. No rename creates or scans
a redirect stub, and consumers resolve old names directly, not by traversing links.

**Reference-name addressability.** In addition to the filename and pinned Unicode rules, a writer rejects brackets. For both the supplied spelling and its NFC spelling, parse each of `See [[name]].`, `See ![[name]].` and `See [[name\|display]].` as CommonMark, then use the extraction rules above: there must be exactly one extracted candidate with the supplied spelling's normalized key. This test uses lower-level extraction, not a recursive call to the validity predicate. Safe intraword underscores, bare ampersands and an isolated single backtick remain allowed. Two individually addressable single-backtick references can form one code span together; isolated addressability does not guarantee safety in arbitrary surrounding Markdown. Spelling changes must still reproduce the registry key.

The independent addressability vectors below use JSON strings (Unicode escapes have their JSON meanings). `accepted` is the writer predicate; `key` is the independently expected lookup key for accepted names. Every accepted vector is exercised through actual create/save/re-save/resolve and the three literal forms; rejected names must leave no identity or body target behind.

<!-- reference-name-vectors -->
```json
[
  {"name":"Lab (old)","key":"lab (old)","accepted":true},
  {"name":"C#","key":"c#","accepted":true},
  {"name":"a!b","key":"a!b","accepted":true},
  {"name":"a'b","key":"a'b","accepted":true},
  {"name":"a-b","key":"a-b","accepted":true},
  {"name":"a+b","key":"a+b","accepted":true},
  {"name":"a=b","key":"a=b","accepted":true},
  {"name":"a,b","key":"a,b","accepted":true},
  {"name":"a;b","key":"a;b","accepted":true},
  {"name":"a$b","key":"a$b","accepted":true},
  {"name":"a%b","key":"a%b","accepted":true},
  {"name":"a@b","key":"a@b","accepted":true},
  {"name":"a^b","key":"a^b","accepted":true},
  {"name":"a~b","key":"a~b","accepted":true},
  {"name":"a{b}","key":"a{b}","accepted":true},
  {"name":"Café","key":"café","accepted":true},
  {"name":"Cafe\u0301","key":"café","accepted":true},
  {"name":"日本語","key":"日本語","accepted":true},
  {"name":"👩‍💻","key":"👩‍💻","accepted":true},
  {"name":"a\u2028b","key":"a\u2028b","accepted":true},
  {"name":"a\u2029b","key":"a\u2029b","accepted":true},
  {"name":"a_b","key":"a_b","accepted":true},
  {"name":"R&D","key":"r&d","accepted":true},
  {"name":"a`b","key":"a`b","accepted":true},
  {"name":"a\u1fefb","key":"a`b","accepted":true},
  {"name":"_old_","accepted":false},
  {"name":"a`b`c","accepted":false},
  {"name":"R&amp;D","accepted":false},
  {"name":"Lab [old]","accepted":false},
  {"name":"a\u1fefb\u1fefc","accepted":false}
]
```

**Titles.** The rules are the CHECKs `entity_names_title_len` and `entity_names_title_safe` ([schema](../schema/README.md)): 1–240 bytes,
trimmed, and a valid file name on Linux, macOS and Windows — the strict direction on purpose ([D5](../decisions/D05-pages-and-day-pages.md)).
Every writer must be stricter than the DDL: besides the addressability predicate above it rejects code points that **Unicode 15.0** has not
assigned (category `Cn` there), whose case fold a later Unicode version could define — which would silently change
`title_key` (executed). The version is named, not "the newest", so the titles a writer accepts do not change when its
language or library ships newer Unicode tables: a writer refuses `U+0378` (unassigned), `U+2EBF0` (assigned in
Unicode 15.1) and `U+1FAE9` (16.0) in a title, and accepts `U+1FAE8` (15.0) (executed). Unicode promises a stable
case fold only for assigned characters, and formally only for text in NFKC form [R74](../research/references.md#r74); a title with a compatibility character (full-width `Ｃａｆé`, a
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

The database verifies what it can (`entity_names_key_*`: trimmed, no ASCII capitals, `lower(title)` for a
pure-ASCII title); that a non-ASCII key is the *right* fold is the writing application's duty
(principle 3) — a writer that computes it wrongly gets uniqueness wrong and nothing else. Resolve a
`[[wikilink]]` with `WHERE entity_names.name_key = :key`, a search on its unique registry key (executed).

**Day pages.** The journal is one page per local day, titled with that day: `2026-09-29`. Its `day`
is its title (`entities_day_page`), so a day page is the page whose title equals its day, and its key is
its title (a pure-ASCII title). Capture appends to today's page and creates it on the first write
([capture](../cookbook/capture.md)); `[[2026-09-29]]` reaches it like any other title, and a link that names a day before anything
was written that day creates that day's page, empty ([save a body](../cookbook/save-a-body.md)). A day page is an ordinary page in every
other way: its `[[links]]` say who the day was with and where ([the days that name someone](../cookbook/days-that-name.md)).
