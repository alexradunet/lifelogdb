# Who may link what

An arrow is a `link_kinds` row. A node that names several types stands for each of them; `any entity` is an
endpoint with no restriction (`from_types` NULL). Three kinds exist: a `wikilink` is made by the body
([D19](../decisions/D19-wikilink-save-contract.md)), an `at` says where a day was ([D16](../decisions/D16-places.md)),
and `part-of` files a thing in a category ([D26](../decisions/D26-metric-categories.md)).

```mermaid
%% diagram: link-map
flowchart LR
    any(["any entity"])
    place["place"]
    page["page"]
    titled["page, person, place, metric, file"]

    page -->|"at"| place
    any -->|"part-of"| page
    titled -->|"wikilink"| titled
```
