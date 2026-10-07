# Entity model

The diagrams draw tables, keys and relationships only; the other columns are in [schema](../schema/README.md). They are part of
the contract: `tests/` checks every table, key column and foreign key they draw against [schema](../schema/README.md), so a
diagram cannot drift from the DDL without a test failing. (`entities_fts`, the FTS5 index over `entity_search_content`,
is derived and rebuildable and is not drawn.)

## Entities and the graph

One named identity and prose body per linkable thing (`entities`), with authoritative spellings in
`entity_names` and a preferred selection checked by a deferred owner FK. A person, place, metric, file or
period is a page concept, not a separate prose row. Typed extensions reference `(id, entity_type)`
directly on `entities`; a place's point remains optional. The polymorphic `links` graph references
the same stable id and the closed `link_kinds` registry.

```mermaid
%% diagram: er-core
erDiagram
    entities ||--o{ entity_names : "entity_id"
    entity_names ||--o| entities : "id"
    entities ||--o| periods : "id"
    entities ||--o| people : "id"
    entities ||--o| files : "id"
    entities ||--o| places : "id"
    entities ||--o{ links    : "from_id"
    entities ||--o{ links    : "to_id"
    link_kinds ||--o{ links  : "kind"

    entities {
        INTEGER id PK, FK
        TEXT preferred_name_key FK
    }
    entity_names {
        INTEGER id PK
        INTEGER entity_id FK
    }
    periods {
        INTEGER id PK, FK
        TEXT entity_type FK
    }
    people {
        INTEGER id PK, FK
        TEXT entity_type FK
    }
    files {
        INTEGER id PK, FK
        TEXT entity_type FK
    }
    places {
        INTEGER id PK, FK
        TEXT entity_type FK
    }
    links {
        INTEGER id PK
        INTEGER from_id FK
        INTEGER to_id FK
        TEXT kind FK
    }
    link_kinds {
        TEXT kind PK
    }
```

## Facts and registries

A reading is a fact, not an entity: `measurements` is append-only ([D7](../decisions/D07-measurements.md)). A correction is a new row, and
`measurements.supersedes_id` points back at the row it corrects; `measurements.captured_with_id` records
provenance (a mood reading points at its day page). `habit_periods` says when a metric is a habit
([D24](../decisions/D24-habits.md)). A metric is a page with a `metrics` row, as a person is a page with a `people` row ([D27](../decisions/D27-a-metric-is-a-page.md));
it is filed in a category, which is a page, by a `part-of` link ([D26](../decisions/D26-metric-categories.md)). `lifelog_meta` stands alone: the rules that span
tables ([D17](../decisions/D17-contract-as-data.md)).

```mermaid
%% diagram: er-facts
erDiagram
    entities    ||--o| metrics : "id"
    entities ||--o{ sessions : "kind_id"
    sessions |o--o{ measurements : "session_id"
    metrics     ||--o{ measurements : "metric_id"
    entities    |o--o{ measurements : "captured_with_id"
    measurements |o--o| measurements : "supersedes_id"
    metrics     ||--o{ habit_periods : "metric_id"

    metrics {
        INTEGER id PK, FK
        TEXT entity_type FK
    }
    sessions {
        INTEGER id PK
        INTEGER kind_id FK
        TEXT kind_entity_type FK
    }
    measurements {
        INTEGER id PK
        INTEGER session_id FK
        INTEGER metric_id FK
        INTEGER captured_with_id FK
        INTEGER supersedes_id FK
    }
    entities {
        INTEGER id PK, FK
        TEXT preferred_name_key FK
    }
    habit_periods {
        INTEGER id PK
        INTEGER metric_id FK
    }
    lifelog_meta {
        TEXT key PK
    }
```
