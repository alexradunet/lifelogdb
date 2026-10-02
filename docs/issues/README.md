# Issues

An issue records an incident from real use — a failed import, a bug in the writing application, a question the
data could not answer, a rule that is ambiguous or untestable. It is the evidence a schema change needs
([how a change happens](../process.md)). One file per incident, named `NNNN-short-slug.md`, written from the
[template](template.md), numbered on from the last: issues up to 0006 are closed and live in git history
(`git log -- docs/issues`), so the next one is **0008**. A resolved or won't-fix issue is deleted; git is the log.

| id | title | status |
|---|---|---|
| [0001](0001-a-rename-has-no-recipe.md) | A rename has no recipe, and the contract leaves open what moves with it | open |
| [0007](0007-a-lifetime-file-has-no-second-copy.md) | A lifetime file has no second copy, and the freeze checklist asks for a decision | open |

Status values: open | proposed (an RFC exists).
