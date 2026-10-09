---
name: lifelog-readings-from-table
description: Turn a table of measurements in a life.db page (bloodwork results, weight, blood pressure) into readings of a registered metric, so they get charts and history. Use when a note with a table of dated values is in life.db and the owner wants its values as readings.
---

# Readings from a table

1. The table must be in a page of `life.db`: import the note first (skill `lifelog-import-notes`).
2. Ask the owner which metric the table holds, and its unit as the table writes it (for example `ng/mL`).
3. A metric that does not exist is the owner's to register: `lifelog do register-metric name=NAME unit=UNIT`. You
   cannot do it.
4. `readings_from_table` with `page` and `metric`, and `column` when the table has more than one column of values.
   (In a shell: `lifelog readings PAGE METRIC [--column NAME]`.)
5. Tell the owner how many readings were written and how many existed, and each reported row with its reason. A
   reported row stays text in the page: a value such as `<5`, a word, a comma decimal, another unit, a second value
   for one day. Never change the page to make a row pass: ask the owner.
6. A wrong value is corrected with `correct` (a new row that supersedes it), never by an edit of the page alone.
