# Implementation Plans

A plan is a dated record ([how a change happens](../process.md)): a change large enough to hand to another agent,
written as a self-contained brief with done criteria. It cites the docs as they were at the commit it names. One file
per plan, `NNN-short-slug.md`, numbered on from the last.

Earlier closed plans live in git history (`git log -- docs/plans`). The next plan number is **090**.

| Plan | Title | Priority | Effort | Depends on | Status |
|------|-------|----------|--------|------------|--------|
| [089](089-no-import-process.md) | No import process: the owner imports with an agent, through the catalog | P1 | L | RFC 0011; issue 0058 | PROPOSED |
| [088](088-the-writer-runs-the-facts-pass.md) | The writer runs the facts pass with a local model: `lifelog import run` | P1 | L | RFC 0010; issue 0057 | DONE |
| [087](087-prepared-quantities-rounded-to-whole-units.md) | The owner rounds chosen prepared quantities to whole units | P2 | S | issue 0055 | DONE |
| [086](086-rules-mark-the-ledger-and-status-reads-it-once.md) | The rules mark the ledger, and status reads it once | P2 | M | issue 0054 | DONE |
| [085](085-rejected-names-are-skipped-and-tombstoned.md) | A rejected name is skipped, and its imported row tombstoned | P1 | M | RFC 0009; issue 0053 | DONE |
| [084](084-owner-decides-names.md) | The owner decides each new name in `entities.md` | P1 | L | RFC 0008; issue 0052 | DONE |
| [083](083-check-reports-every-refusal.md) | Check reports every refused write with a class; look-alikes by kind | P1 | M | issues 0050, 0051 | DONE |
| [082](082-snapshot-in-the-catalog.md) | A snapshot from any surface: the owner-only snapshot action | P2 | S | Plan 081; 37767bb | DONE |
| [081](081-planning-on-every-surface.md) | Planning on every surface: tasks, occurrences and deadlines in the catalog | P1 | M | Owner decision 2026-10-08; adea9b8 | DONE |
| [080](080-api-reference.md) | An API reference for client authors, kept true by a test | P2 | S | Plans 078, 079; 68ca8d1 | DONE |
| [079](079-client-contract-hygiene.md) | Client contract hygiene: error codes, list pages, a connect timeout, one parameter | P2 | S | Plan 078; b28ee22 | DONE |
| [078](078-network-server.md) | Serve the API beyond this machine: a token, allowed origins, optional TLS | P1 | M | Owner requirement 2026-10-08; 05790ec | DONE |
| [077](077-ingest-folder-survey-and-passes.md) | An ingest folder: survey without reading, notes folders, added files, a later pass | P1 | M | issues 0043–0046 | DONE |
| [076](076-validation-gaps.md) | Exercise remaining validation gaps | P1/P2 | L | Owner continuation; 6f2bfe8 | DONE |
| [075](075-schema-validation-depth.md) | Schema validation depth | P1/P2 | L | Owner authorization; 90c062e | DONE |
| [074](074-schema-review-findings.md) | Repair schema review findings | P1/P2 | L | Issues 0026–0032; owner authorization | DONE |
| [073](073-review-repairs.md) | Schema, reliability and validation repairs | P1/P2 | L | Reproduced review findings | DONE |
| [071](071-audit-followups.md) | Integrate the 22 residual audit follow-ups | P1/P2 | L aggregate | Existing import/surface behavior retained | IN REVIEW (owner) |
| [072](072-stable-names-candidate.md) | Stable names and life periods implementation candidate | P1 | L aggregate | RFC 0006 candidate approvals; identity-core first | BLOCKED (partial proof checkpoint; implementation pending continuation) |

Status values: TODO | IN PROGRESS | IN REVIEW (owner) | DONE | BLOCKED (with one-line reason) |
REJECTED (with one-line rationale). Close a plan with its result and status, retaining the dated record as
[the process](../process.md) requires.
