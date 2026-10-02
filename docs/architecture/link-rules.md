# Who may link what

An arrow is a `link_kinds` row; a double-headed arrow is a symmetric kind, mirrored by trigger so that one
direction suffices for backlinks. A node that names several types stands for each of them; `any entity`
is an endpoint with no restriction (`from_types` or `to_types` NULL).

```mermaid
%% diagram: link-map
flowchart LR
    any(["any entity"])
    person["person"]
    place["place"]
    page["page"]
    named["person, place"]
    titled["page, person, place"]

    any -->|"about"| named
    any <-->|"related"| any
    page -->|"at"| place
    person -->|"parent-of"| person
    person <-->|"friend, family"| person
    place -->|"located-in"| place
    page -->|"redirect"| titled
    titled -->|"wikilink"| titled
```
