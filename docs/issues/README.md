# Issues

An issue records an incident from real use — a failed import, a bug in the writing application, a question the
data could not answer, a rule that is ambiguous or untestable. It is the evidence a schema change needs
([how a change happens](../process.md)). One file per incident, named `NNNN-short-slug.md`, written from the
[template](template.md), numbered on from the last: issues up to 0009 are closed and live in git history
(`git log -- docs/issues`), so the next one is **0011**. A resolved or won't-fix issue is deleted; git is the log.

| id | title | status |
|---|---|---|
| [0010](0010-a-photos-place-has-no-home.md) | A photo's place cannot become an `at` link: a place has no point, and a file's day is the day it was kept | proposed |

Status values: open | proposed (an RFC exists).
