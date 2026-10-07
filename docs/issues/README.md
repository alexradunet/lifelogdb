# Issues

An issue records an incident from real use — a failed import, a bug in the writing application, a question the
data could not answer, a rule that is ambiguous or untestable. It is the evidence a schema change needs
([how a change happens](../process.md)). One file per incident, named `NNNN-short-slug.md`, written from the
[template](template.md), numbered on from the last: issues up to 0010 are closed and live in git history
(`git log -- docs/issues`). Issues 0011–0038 are listed below; the next one is **0039**. Close an issue with its resolution and status, retaining the dated incident as
[the process](../process.md) requires. Earlier closed records remain available in git history.

| id | title | status |
|---|---|---|
| [0033](0033-containment-starts-from-deleted-root.md) | Recursive active reads traverse deleted roots or intermediates | resolved |
| [0034](0034-scoped-import-retry-rejected-after-tombstone.md) | Scoped import retry is rejected after a session tombstone | resolved |
| [0035](0035-test-fixtures-can-open-the-wrong-file.md) | Validation fixtures open the wrong file or credit an unrelated error | resolved |
| [0036](0036-writer-opens-unsupported-schema-versions.md) | Writer opens unsupported schema versions | resolved |
| [0037](0037-replace-changes-a-used-link-kind.md) | REPLACE changes a used link kind's fixed structure | resolved |
| [0038](0038-habit-readers-hide-invalid-check-ins.md) | Habit readers hide invalid check-ins | resolved |
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
| [0022](0022-nul-suffixes-bypass-text-checks.md) | NUL suffixes bypass constrained text checks | resolved |
| [0023](0023-place-point-ownership-bypasses-revisions.md) | Moving a place point bypasses both owners' edit revisions | resolved |
| [0024](0024-ghost-cleanup-includes-referenced-pages.md) | Ghost cleanup includes pages referenced by sessions and measurements | resolved |
| [0025](0025-personal-planning-has-no-durable-task-model.md) | Personal planning has no durable task and reminder model | resolved |
| [0026](0026-detail-identities-can-be-reassigned.md) | Typed detail identities can be reassigned | resolved |
| [0027](0027-entity-creation-time-can-be-rewritten.md) | Entity creation time can be rewritten | resolved |
| [0028](0028-semantic-integrity-misses-damaged-relationships.md) | Semantic integrity misses damaged relationships | resolved |
| [0029](0029-read-recipes-disagree-with-lifecycle-and-ties.md) | Read recipes disagree with lifecycle and deterministic selection | resolved |
| [0030](0030-retained-reference-lookups-scan-facts.md) | Retained reference lookups scan full fact tables | resolved |
| [0031](0031-compound-diacritics-escape-search-folding.md) | Compound diacritics escape search folding | resolved |
| [0032](0032-history-and-preservation-promises-exceed-storage.md) | History and preservation promises exceed stored evidence | resolved |

Status values: open | proposed (an RFC exists) | resolved | won't-fix.
