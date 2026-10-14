# Implementation Plans

A plan is a dated record ([how a change happens](../process.md)): a change large enough to hand to another agent,
written as a self-contained brief with done criteria. It cites the docs as they were at the commit it names. One file
per plan, `NNN-short-slug.md`, numbered on from the last.

Earlier closed plans live in git history (`git log -- docs/plans`). The next plan number is **107**.

| Plan | Title | Priority | Effort | Depends on | Status |
|------|-------|----------|--------|------------|--------|
| [106](106-one-continuous-conversation.md) | One continuous conversation: a page per day, compacted into the daily note | P1 | M | plans 102, 103; owner decision 2026-10-14 | DONE |
| [105](105-the-owners-shape-in-the-synthetic-fixture.md) | The owner's shape in the synthetic fixture: a calibrated workload for tests | P1 | L | owner request 2026-10-14; `bd16083` | TODO |
| [104](104-the-chat-in-basm.md) | The chat in Basm: the conversation drawn as the Chat of the owner's design system | P2 | S | plan 103; owner decision 2026-10-14 | DONE |
| [103](103-the-conversation-on-the-screen.md) | The conversation on the screen: a chat page, and a sidebar that follows the owner | P1 | M | issue 0072; owner decision 2026-10-14 | IN PROGRESS |
| [102](102-a-conversation-with-lifelog.md) | A conversation with lifelog: the action `ask`, an agent inside the writer | P1 | M | RFC 0012; issue 0071; owner decisions 2026-10-14 | IN PROGRESS |
| [101](101-the-calendar-holds-every-dated-thing.md) | The calendar holds every dated thing | P1 | M | issue 0070; owner decision 2026-10-13 | DONE |
| [100](100-one-page-a-metric.md) | One page a metric: Record, every reading, and its own page | P1 | M | issue 0069; owner decision 2026-10-13 | DONE |
| [099](099-rules-into-the-schema.md) | Rules into the schema: a Go copy deleted, a writer-held rule made a trigger | P1 | M | issue 0068 | DONE |
| [098](098-the-codebase-made-cohesive.md) | The codebase made cohesive: dead code out, one helper a rule, one convention a concept | P1 | L | issue 0067; owner decisions 2026-10-12 | DONE |
| [097](097-the-almanac-reflects.md) | The Almanac reflects: the past on the page, a refusal on its form, the week and the year | P1 | L | issue 0066; owner decision 2026-10-12 | DONE |
| [096](096-bound-in-hearthwood.md) | Bound in Hearthwood: the Almanac takes Balaur's materials | P1 | L | issue 0065; owner decisions 2026-10-12 | DONE |
| [095](095-the-leaf-and-its-margin.md) | The leaf and its margin: a calm Almanac | P1 | L | issue 0064; owner decisions 2026-10-12 | DONE |
| [094](094-the-command-list.md) | The command list of Ctrl K | P2 | M | issue 0063; owner decision 2026-10-12 | DONE |
| [093](093-words-phone-and-today.md) | Words, not data; the phone first; a richer Today | P1 | L | issue 0062; owner decisions 2026-10-12 | DONE |
| [092](092-the-app-shell.md) | The app shell: the Almanac design system in the browser | P1 | L | issue 0061; owner decisions 2026-10-12 | DONE |
| [091](091-the-browser-for-daily-use.md) | The browser for daily use: planning, the journal, navigation and editing | P1 | L | issue 0060; owner decisions 2026-10-12 | DONE |
| [090](090-agentic-init.md) | `lifelog agentic-init`: the lifelog folder ready for any agent | P1 | M | issue 0059 | DONE |
| [089](089-no-import-process.md) | No import process: the owner imports with an agent, through the catalog | P1 | L | RFC 0011; issue 0058 | DONE |
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
