# Implementation Plans

A plan is a dated record ([how a change happens](../process.md)): a change large enough to hand to another agent,
written as a self-contained brief with done criteria. It cites the docs as they were at the commit it names. One file
per plan, `NNN-short-slug.md`, numbered on from the last.

Closed plans live in git history (`git log -- docs/plans`). The next plan number is **073**.

| Plan | Title | Priority | Effort | Depends on | Status |
|------|-------|----------|--------|------------|--------|
| [071](071-audit-followups.md) | Integrate the 22 residual audit follow-ups | P1/P2 | L aggregate | Existing import/surface behavior retained | IN REVIEW (owner) |
| [072](072-stable-names-candidate.md) | Stable names and life periods implementation candidate | P1 | L aggregate | RFC 0006 candidate approvals; identity-core first | BLOCKED (partial proof checkpoint; implementation pending continuation) |

Status values: TODO | IN PROGRESS | IN REVIEW (owner) | DONE | BLOCKED (with one-line reason) |
REJECTED (with one-line rationale). A plan that is DONE or REJECTED is deleted; git is the log.
