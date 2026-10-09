---
name: lifelog-import-notes
description: Put the owner's notes (a folder of Markdown notes, a journal of daily notes, an Obsidian vault) into life.db, one note at a time, with the owner approving each person, place and link. Use when the owner asks to import notes or a journal into lifelog.
---

# Import notes into life.db

First ask the owner for a snapshot (`lifelog snapshot`) and for the folder to import. Then take one note at a time,
in the order of their names, and wait after each one.

## A daily note

A daily note is a `.md` file whose name is a day: `YYYY-MM-DD`, `YYYY-MM-DD-Weekday`, `YYYY_MM_DD`, or `YYYY-MM-DD 1`
(a second note of that day). Its day is the date in its name. Every other note is a topic note. A name that looks
like a date but is none of these: ask the owner.

1. Read the file.
2. `get_day` the day. If the day page already holds the note's text, say "imported before" and stop for this note.
3. `capture` with `day` = the day and `text` = the note's whole text, exactly.
4. `get_day` again, and compare the page's text with the file. If they differ, say where. Fix it with `save_body`
   only when the owner says so.
5. The answer of `capture` lists `created` (empty pages that the note's `[[links]]` made) and `skipped` (links that
   are not valid titles). Tell the owner both.
6. People and places. List each person the note names, and each place where the owner was. `find` each one, then
   propose one line each:
   - an existing person or place: "link about NAME?" or "link at PLACE?"
   - a new one: "new person NAME, and link about?" or "new place PLACE, and link at?"

   Write only what the owner approves: `create_person` or `create_place`, then `link` from the day page (`about` for
   a person, `at` for a place). Never a person for a role ("the dentist"), never a place for a common noun.

## A topic note

1. Read the file. Its title is the file name without `.md`.
2. `find` the title.
   - A page with text: tell the owner, and stop for this note.
   - An empty page (a link made it): `save_body` with the note's whole text.
   - No page: `create_page` with the title, and as body the note's whole text.
3. Compare the page with the file, as for a daily note.

## The text of a note

- Keep the frontmatter, the headings and every word as the file writes them.
- A link that is not a valid title (`[[Folder/Note]]`, `[[Note#Heading]]`) is skipped by the save, and the answer
  lists it. Ask the owner: keep it as written, or write `[[Note|Folder/Note]]`.
- An embedded attachment (`![[scan.pdf]]`) makes an empty page of that name. When the owner later keeps the file
  (skill `lifelog-keep-photos`), the file becomes that page.
- A table of dated values (bloodwork): import the note first, then use the skill `lifelog-readings-from-table`.
