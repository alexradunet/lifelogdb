# Issues

An issue records an incident from real use — a failed import, a bug in the writing application, a question the
data could not answer, a rule that is ambiguous or untestable. It is the evidence a schema change needs
([how a change happens](../process.md)). One file per incident, named `NNNN-short-slug.md`, written from the
[template](template.md), numbered on from the last: issues up to 0010 are closed and live in git history
(`git log -- docs/issues`). Issues 0011–0021 are listed below; the next one is **0022**. A resolved or won't-fix issue
is deleted; git is the log.

| id | title | status |
|---|---|---|
| [0011](0011-renames-lose-identity-and-backlinks.md) | Renames change identity and repeated renames lose backlinks | proposed |
| [0012](0012-timestamps-allow-stale-overwrites.md) | A timestamp used as an edit version permits a stale overwrite | proposed |
| [0013](0013-promotion-strands-typed-links.md) | Promotion can strand a typed link while integrity reports success | proposed |
| [0014](0014-date-checks-disagree-with-writers.md) | Date round trips admit values that the writer rejects | proposed |
| [0015](0015-mood-can-become-a-habit.md) | Starting Mood as a habit disables normal mood capture | proposed |
| [0016](0016-accepted-handle-cannot-be-linked.md) | An accepted handle cannot be expressed by its literal wikilink | proposed |
| [0017](0017-link-identity-and-time-are-mutable.md) | Link identity and creation time escape the stated immutability rule | proposed |
| [0018](0018-symmetric-link-note-semantics-are-unclear.md) | Symmetric-link note semantics are unclear | proposed |
| [0019](0019-life-period-questions-have-no-structured-boundaries.md) | Jobs, study and trips cannot define honest query windows | proposed |
| [0020](0020-imported-sessions-need-time-and-measurement-scope.md) | Session exports need time and measurement scope | proposed |
| [0021](0021-selected-photo-sidecars-have-unhandled-time-evidence.md) | Selected photo sidecars have unhandled time evidence | proposed |

Status values: open | proposed (an RFC exists).
