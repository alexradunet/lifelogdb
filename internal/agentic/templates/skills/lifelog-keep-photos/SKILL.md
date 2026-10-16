---
name: lifelog-keep-photos
description: Keep a chosen photo, PDF or recording in life.db as a file page with its day. Use when the owner picks a photo for a day, or wants to keep an attachment of a note.
---

# Keep a photo or a file

lifelog never stores the original file: it keeps its hash, its text and a small picture. The original stays where it
is. A photo's day is the day the owner gives, else the date in its EXIF; the page of that day shows its picture.
lifelog does not read where a photo was taken.

1. The owner chooses the file. Never pick photos yourself from a library or a folder.
2. In a shell: `lifelog --source agent:NAME file PATH --dry-run --human` (the program and its `--db` as AGENTS.md names them, so the rows show you as their writer). Tell the owner what it would do: the title, the day, the picture.
3. A photo with no date in its EXIF gets no day, and no day page shows it: ask the owner for its day, then add
   `--day YYYY-MM-DD`.
4. A PDF or a recording: its text comes first (the owner's transcript, or a text the owner made from it), then
   `--text TEXTFILE`.
5. Without `--dry-run`, it writes. Tell the owner the result. One command keeps one file: for several photos, run it
   once for each photo.
