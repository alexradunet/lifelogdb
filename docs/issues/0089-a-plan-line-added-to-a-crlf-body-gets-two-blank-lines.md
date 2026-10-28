# 0089 — A plan line added to a body with CRLF line ends gets two blank lines before it

- **Date:** 2026-10-22
- **Status:** resolved
- **Seen in:** the counter-review of [plan 142](../plans/142-the-fall-back-pass.md), on a synthetic page

## What happened

The writer adds a plan line to the end of a body as its own paragraph: a blank line, then the line
([plan lines](../contract/plan-lines.md)). It writes `\n\n` and the line, and does not look at the line end that the
body uses. A body that ends in `\r\n` then ends in `\r\n\n\n- [ ] c`: a reader of CRLF text sees two blank lines, not
one. The lines read back, and every other write keeps the body byte for byte, so nothing is lost. The body is only
not what the contract draws.

## Reproduce

On a fresh database, make a page whose body is `Week\r\n\r\n- [ ] a\r\n` and add a plan line `c` to it with the
`plan` action. The body is `Week\r\n\r\n- [ ] a\r\n\n\n- [ ] c`. The test `TestPlanWritesKeepTheLineEndsOfABody`
(`internal/core/plans_test.go`) pins this body as the writer gives it today, with the second blank line.

## Rules involved

- [Plan lines](../contract/plan-lines.md): the line that a plan, a tick or a carry adds goes at the end of the body as
  its own paragraph.
- [D29](../decisions/D29-plan-lines.md).

## Resolution

Resolved on 2026-10-28 by the commit that changes `appendLine` (`internal/core/plans.go`): the paragraph break is
written in the line end the body uses, and a body that ends in a line break gets one more, not two. The body of the
example is now `Week\r\n\r\n- [ ] a\r\n\r\n- [ ] c`. [Plan lines](../contract/plan-lines.md) says so, the test above
expects it, and `TestAPlanLineIsOneParagraphAfterAnyBody` holds the other line ends.
