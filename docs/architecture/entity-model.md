# Entity model

The diagrams draw tables, keys and relationships only; the other columns are in [schema](../schema/README.md). They are part of
the contract: `tests/` checks every table, key column and foreign key they draw against [schema](../schema/README.md), so a
diagram cannot drift from the DDL without a test failing. (`pages_fts`, the FTS5 index over `pages`,
is derived and rebuildable and is not drawn.)

## Entities and the graph

One supertype row per linkable thing (`entities`), one domain row per entity with the *same* id — the
composite foreign key `(id, entity_type)` makes the type and the table agree — and one polymorphic graph
(`links`) over the supertype, whose `kind` is a foreign key to the closed registry `link_kinds`. A
person or a place is also a page ([D20](../decisions/D20-named-pages.md)): a person's `people` row hangs off its `pages` row, which hangs
off its `entities` row — one id, three rows; a place is its `entities` and `pages` rows alone ([D16](../decisions/D16-places.md)).

```mermaid
%% diagram: er-core
erDiagram
    entities ||--o| pages    : "id"
    pages    ||--o| people   : "id"
    entities ||--o{ links    : "from_id"
    entities ||--o{ links    : "to_id"
    link_kinds ||--o{ links  : "kind"

    entities {
        INTEGER id PK
    }
    pages {
        INTEGER id PK, FK
        TEXT entity_type FK
    }
    people {
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
([D24](../decisions/D24-habits.md)). `metric_categories` files metrics in a tree of categories whose parents
never change ([D26](../decisions/D26-metric-categories.md)). `lifelog_meta` stands alone: the rules that span
tables ([D17](../decisions/D17-contract-as-data.md)).

```mermaid
%% diagram: er-facts
erDiagram
    metric_categories |o--o{ metric_categories : "parent_id"
    metric_categories |o--o{ metrics : "category_id"
    metrics     ||--o{ measurements : "metric_id"
    entities    |o--o{ measurements : "captured_with_id"
    measurements |o--o| measurements : "supersedes_id"
    metrics     ||--o{ habit_periods : "metric_id"

    metric_categories {
        INTEGER id PK
        INTEGER parent_id FK
    }
    metrics {
        INTEGER id PK
        INTEGER category_id FK
    }
    measurements {
        INTEGER id PK
        INTEGER metric_id FK
        INTEGER captured_with_id FK
        INTEGER supersedes_id FK
    }
    entities {
        INTEGER id PK
    }
    habit_periods {
        INTEGER id PK
        INTEGER metric_id FK
    }
    lifelog_meta {
        TEXT key PK
    }
```
