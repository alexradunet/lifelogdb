# Issues

An issue records an incident from real use — a failed import, a bug in the writing application, a question the
data could not answer, a rule that is ambiguous or untestable. It is the evidence a schema change needs
([how a change happens](../process.md)). One file per incident, named `NNNN-short-slug.md`, written from the
[template](template.md).

| id | title | status | resolved by |
|---|---|---|---|
| [0001](0001-a-rename-has-no-recipe.md) | A rename has no recipe, and the contract leaves open what moves with it | open | — |
| [0002](0002-a-ledger-line-with-an-em-dash-in-the-file-name-is-cut.md) | A ledger line whose file name contains " — " is cut in half | resolved | `cad659b` |
| [0003](0003-an-import-cannot-set-a-persons-birth-day.md) | An import cannot set a person's birth_day, and a bodyless person note cannot be promoted | open | — |
| [0004](0004-an-about-link-applied-before-the-persons-own-note-fails.md) | An `about` link applied before the person's own note fails, and the replay aborts on it | open | — |
| [0005](0005-a-replay-stops-in-the-middle-and-leaves-the-target-half-written.md) | A replay stops in the middle and leaves the target half-written; there is no dry run | open | — |
| [0006](0006-one-edited-word-in-a-stamped-file-stops-the-whole-import.md) | One edited word in a stamped file stops the whole import, and status does not say what changed | open | — |

Status values: open | proposed (an RFC exists) | resolved | won't fix (with a one-line reason).
