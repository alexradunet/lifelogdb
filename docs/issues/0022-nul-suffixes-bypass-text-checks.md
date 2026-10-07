# 0022 — NUL suffixes bypass constrained text checks

- **Date:** 2026-10-07
- **Status:** resolved
- **Seen in:** a synthetic review probe through the measurement writer and fresh-file DDL probes on SQLite 3.53.4

## What happened

Recording a Mood reading with the zone `UTC\u0000hidden` succeeded and returned the full invalid zone on read.
The integrity checks passed. SQLite TEXT `length` and `GLOB` stop at NUL, leaving the suffix outside the checked
shape and length. The same DDL gap admitted invalid file hashes, MIME types, provenance, non-ASCII name keys
and link-kind registry text. The application's source and file validators already refused those inputs.

## Reproduce

Build a fresh database from [schema.sql](../schema/schema.sql), then record a reading with a valid metric, day
and value and a zone made from `UTC`, NUL and `hidden`. A direct INSERT into `measurements` gives the same result.
For a file, append NUL and a suffix to an otherwise valid 64-character lowercase hash. Valid controls use the
same inputs without the suffix; no real records are needed.

## Rules involved

The named text CHECKs in [schema.sql](../schema/schema.sql), [time model](../decisions/D10-time-model.md),
[file identity](../decisions/D09-binary-files.md), and [connection setup](../contract/connections.md).

## Resolution

Explicit NUL guards cover the affected constrained fields. Ordinary recording and scope relocation share zone
validation before writing. The schema safeguard suite exercises valid controls and both trailing and embedded
NUL; each new guard has a mutant that removes only that guard. Focused regressions reproduced the failures before
implementation and pass afterward. Generation, vet and `TMPDIR=/var/tmp go test -count=1 ./...` passed on Go 1.27.1 / Linux amd64. No decision changes or RFC are
needed for these enforcement fixes ([process](../process.md)).
