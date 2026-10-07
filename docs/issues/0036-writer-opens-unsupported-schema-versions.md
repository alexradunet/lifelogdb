# 0036 — Writer opens unsupported schema versions

- **Date:** 2026-10-07
- **Status:** resolved
- **Seen in:** synthetic version-header tests against the production open paths.

## What happened

Files with the correct application id and unsupported `user_version` values were accepted by both ordinary and snapshot open paths. Closing an ordinary connection could optimize the unrecognized file, changing it before compatibility was established.

## Reproduce

Create a disposable current schema file, set `user_version` to 0, -1, 2 or the maximum signed 32-bit value, and call both open paths. All were accepted before the fix.

## Rules involved

[D13 compatibility](../decisions/D13-migrations-and-freeze.md) requires readers and writers to recognize the supported schema version.

## Resolution

Check application identity and supported version through the read-only connection before opening a writer; refusal does not invoke optimizing writer cleanup. Test unsupported versions and unchanged file contents, with supported-version controls. No migration is introduced. See [plan 075](../plans/075-schema-validation-depth.md).
