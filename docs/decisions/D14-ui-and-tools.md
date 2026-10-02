# D14 — UI: thin custom app for capture/browse; off-the-shelf tools for exploration.

**Status:** accepted

- **Decision.** Build only what the product needs: a capture composer that appends to today's page
  (with mood, [D6](D06-mood-is-a-measurement.md)), a day view, simple metric charts, forms for people/places,
  a search box over `pages_fts`, and a backlinks panel. For ad-hoc exploration: **Datasette** pointed
  at `life.db`, read-only ([connection setup](../contract/connections.md)) [R49](../research/references.md#r49). `sqlite-web` is **not used** [R50](../research/references.md#r50): it can insert, update and
  delete rows — a second writer that bypasses the insert conventions (principle 3).
- **The writing application** is one stack the owner controls — one codebase, in whatever language —
  that carries the insert conventions ([capture](../cookbook/capture.md)) and exposes them as a CLI, a REST API and an
  agent surface, so the owner's UIs, AI agents and importers all write through it (principle 3).
  Its own engineering decisions live outside these docs: it implements the schema, it never
  defines it.
- **Rejected.** Building a generic admin UI — Datasette already is one, maintained by someone else.
- **Sources.** [R49](../research/references.md#r49)[R50](../research/references.md#r50).
