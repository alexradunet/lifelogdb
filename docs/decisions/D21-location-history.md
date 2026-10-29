# D21 — No location: a place has no point, a photo's position is never read, and there is no track.

**Status:** accepted

- **Decision.** Where the owner was on a day is the words of that day's page ([D16](D16-places.md)), and nothing
  else. A place is a plain page with no point and no radius: there is no `places` table. A photo's position, when its metadata holds
  one, is never read and never stored: a kept photo gives its day ([D9](D09-binary-files.md)), and the owner writes
  the words. No `positions` table, no fix per row.
- **Why.** The owner reviewed the writer on 2026-10-15 and kept the core only
  ([RFC 0016](../rfcs/0016-no-location.md)): a place's point served one thing, the matching of a photo's position to
  a place when the photo was kept, and that matching cost a table, a distance query without math functions, a
  radius per place, a `link_days` flag for home and work, and a cookbook recipe of its own. The owner names the
  place of a day by hand, as the day pages always did.
- **Alternatives.**
  - *A place with a point and a radius, a photo matched when kept* (the earlier D21, [proposal 0005](../rfcs/0005-a-place-has-a-point.md)):
    removed by RFC 0016. The owner's points were written into the place pages as text, so no word was lost.
  - *A track*: a `positions` table, one GPS fix per row: rejected, the owner does not want minute-by-minute
    tracking. Reopen with an issue when a question needs the moment: "where was I at 15:00 on that day?".
  - *An online reverse geocoder*: rejected, it would send the owner's positions to a third party.
- **Trade accepted.** "Where was I" is answered by what the owner wrote, never by a photo. The whereabouts of a place is
  prose in its page.
- **Sources.** Executed: the `named` suite (no place type and no place table).
