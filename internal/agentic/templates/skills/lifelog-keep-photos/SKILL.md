---
name: lifelog-keep-photos
description: Keep a chosen photo, PDF or recording in life.db as a file page, linked to its day and to the place a photo was taken. Use when the owner picks a photo for a day, or wants to keep an attachment of a note.
---

# Keep a photo or a file

lifelog never stores the original file: it keeps its hash, its text and a small picture. The original stays where it
is.

1. The owner chooses the file. Never pick photos yourself from a library or a folder.
2. In a shell: `lifelog file PATH --dry-run --human`. Tell the owner what it would do: the day, the place.
3. A photo taken near no known place: ask the owner for the place, then add `--at PLACE`.
4. A PDF or a recording: its text comes first (the owner's transcript, or a text the owner made from it), then
   `--text TEXTFILE`.
5. Without `--dry-run`, it writes. Tell the owner the result. Several photos of one day go in one command:
   `lifelog file PHOTO1 PHOTO2 --human`.
