# How a change happens

The schema changes only when real use asks for it (principles 6 and 7 of the
[goals](architecture/goals-and-principles.md)). This page is the path a change takes, and where each step is written down.

```
incident ──▶ issue ──▶ proposal (RFC) ──▶ decision (ADR) ──▶ schema.sql + pages + suites ──▶ plan (optional)
```

1. **Issue** — [issues/](issues/README.md). Something real went wrong or could not be done: a failed import, a bug in
   the writing application, a question the data could not answer, a rule that is ambiguous or untestable. One file
   per incident, from the [template](issues/template.md). A wish of an application or a hypothetical writer is not
   an issue.
2. **Proposal** — [rfcs/](rfcs/README.md). The change that would fix one or more issues: the options, what each costs
   after the freeze (additive only, [D13](decisions/D13-migrations-and-freeze.md)), and what it makes redundant. One file per
   proposal, from the [template](rfcs/template.md). A small fix that changes no decision may skip this step.
3. **Decision** — [decisions/](decisions/README.md). An accepted proposal becomes a new decision, or rewrites an existing
   one in place. Numbers are stable and never reused.
4. **Change** — [schema.sql](schema/schema.sql) is edited in place (until the freeze), with the contract pages, the
   cookbook, the diagrams and the suites it touches, in one commit; every suite ends green.
5. **Plan** — [plans/](plans/README.md). When a change is large enough to hand to another agent, it is written as a
   self-contained plan with done criteria, and its status is kept in the plans index.

## Current truth and history

| folders | what they are |
|---|---|
| `architecture/`, `schema/`, `contract/`, `decisions/`, `cookbook/`, `guides/`, `research/` | **current truth only**: today's rule and its reasons, never the story of how it got there. When something changes it is rewritten in place; git is the log. |
| `issues/`, `rfcs/`, `plans/` | **records**: dated, written once, closed with a status. They cite the docs as they were at their commit. |
