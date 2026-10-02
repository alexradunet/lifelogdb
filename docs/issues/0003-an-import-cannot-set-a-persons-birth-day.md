# 0003 — An import cannot set a person's birth_day, and a bodyless person note cannot be promoted

- **Date:** 2026-10-02
- **Status:** open
- **Seen in:** the import, on notes under `People/` whose only content is frontmatter

## What happened

Two walls, both hit while importing a vault of person notes.

**A birthday in the note never reaches `people.birth_day`.** The facts contract's `person` write carries a
title and a name only (`internal/importer/facts.go`, `named` in `internal/importer/apply.go`), and
`Tx.CreatePerson(title, name, birth, death, importKey)` is called with empty birth and death. `POST /people`
is the only writer that sets them, and it needs the title to be free:

```
$ lifelog do create-person title="Bob Person" name="Bob Person" birth_day=1980-03-29   # before the page exists
$ lifelog do apply-vault
422: the plan has problems (People/Bob Person.md: the database already has a person titled "Bob Person":
     change the title, or ask the owner)
```

So the only order that works — create the person first — is refused by the plan check, and the order the
importer uses leaves `birth_day` NULL. There is no endpoint to set it afterwards.

**A person note with no body cannot be promoted at all.** Every facts write needs a quote from the file, and
`states(title)` requires the quote to name the title (or an alias that maps to it). A note that is only
frontmatter has no body to quote:

```
$ lifelog do check-facts file="People/Bob Person.md"
422: People/Bob Person.md: write 1: the quote does not name "Bob Person"
```

The page stays a plain page, so `[[Bob Person]]` links to it but nothing knows it is a person.

## Reproduce

On a fresh database built from [schema.sql](../schema/schema.sql), a source folder with one file,
`People/Bob Person.md`, containing only:

```
---
type: Person
birthday: 1980-03-29
---
```

1. `lifelog import setup --workspace <source>.lifelog --from <db>`, `lifelog do make-ledger`, approve rules.
2. `lifelog do apply-vault` → the page is created as a plain page.
3. `lifelog do write-facts file="People/Bob Person.md" facts='{"file":"People/Bob Person.md","writes":[{"person":{"title":"Bob Person","name":"Bob Person"}}]}'` then `check-facts` → refused: every write needs a quote that is in the file and names the person, and there is no body to quote.
4. Alternatively `do create-person` first, then `do apply-vault` → the plan problem above (observed verbatim in a real import).

## Rules involved

- [Identity (D20)](../decisions/D20-identity.md) — a person is a page with a `people` row.
- [Importing](../guides/importing.md) — the quote is the evidence a facts write is allowed to make.
- `people.birth_day` in [schema.sql](../schema/schema.sql).

## Resolution

_(open)_
