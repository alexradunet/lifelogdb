# Issues

An issue records an incident from real use — a failed import, a bug in the writing application, a question the
data could not answer, a rule that is ambiguous or untestable. It is the evidence a schema change needs
([how a change happens](../process.md)). One file per incident, named `NNNN-short-slug.md`, written from the
[template](template.md).

| id | title | status | resolved by |
|---|---|---|---|
| [0001](0001-a-rename-has-no-recipe.md) | A rename has no recipe, and the contract leaves open what moves with it | open | — |
| [0002](0002-a-ledger-line-with-an-em-dash-in-the-file-name-is-cut.md) | A ledger line whose file name contains " — " is cut in half | open | — |
| [0003](0003-an-import-cannot-set-a-persons-birth-day.md) | An import cannot set a person's birth_day, and a bodyless person note cannot be promoted | open | — |

Status values: open | proposed (an RFC exists) | resolved | won't fix (with a one-line reason).
