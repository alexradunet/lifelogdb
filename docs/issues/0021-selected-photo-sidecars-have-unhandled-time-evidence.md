# 0021 — Selected photo sidecars contain time evidence the capture policy does not handle

- **Date:** 2026-10-06
- **Status:** won't-fix
- **Seen in:** owner-authorized inspection of a small Google Photos metadata sample.

## What happened

Sampled metadata contained separate `photoTakenTime` and `creationTime` objects, numeric-string timestamp fields,
and a `geoData` object. The current selected-photo path derives its day from the image's metadata, otherwise from
the day it is kept. It has no specified association or precedence rule for an export's accompanying metadata.

Only field names and types were reported. No actual disagreement between image and sidecar values was established,
no image was opened in this inspection, and no date coverage or location history was inferred. This is a missing
interpretation policy for an observed source format, not evidence that a particular photo has a wrong date.

## Reproduce

The policy question can be made into a synthetic import fixture:

1. Select an image with no embedded capture date and a matching export sidecar carrying a capture timestamp.
2. Keep it on a different day.
3. Ask which day the kept file should represent and what evidence determines the local day.

A second fixture should disagree on the embedded and sidecar capture metadata. Matching by similar basename,
choosing creation time instead of capture time, or using the importing machine's timezone must not silently decide
what happened. These fixtures are not yet implemented and no failed production import is claimed.

## Rules involved

- [D9](../decisions/D09-binary-files.md), [D10](../decisions/D10-time-model.md) and [D21](../decisions/D21-location-history.md).
- [Keep a file](../cookbook/keep-a-file.md), [the place of a photo](../cookbook/place-of-a-photo.md) and [imports](../contract/imports.md).
- [Google Photos export guidance](https://support.google.com/photos/answer/3024190): additional metadata may accompany an export separately.

## Resolution

Won't-fix. A kept photo gives its day from its own metadata, else from the day it is kept, and its position is
never read ([D9](../decisions/D09-binary-files.md), [D21](../decisions/D21-location-history.md)); the Google Photos
extension gives the day from the photo's time in the zone the owner names. A sidecar is never read. The
[non-goals](../architecture/non-goals.md) keep a photo archive out. [RFC 0006](../rfcs/0006-stable-names-and-life-periods.md)
is withdrawn.
