# Lifelog

The design of `life.db`: a lifetime-scale, single-user SQLite database — a life log and its backup. The product is
the schema and its documentation; there is no application in this repo.

- **[docs/](docs/README.md)** — the database architecture: goals, the canonical [schema.sql](docs/schema/schema.sql),
  the storage contract, the decision log, the query cookbook, research, and the process (issues, proposals, plans).
- **[tests/](tests/README.md)** — the validation suites: every executed claim of the docs. `python3 tests/run_all.py`.
- **[AGENTS.md](AGENTS.md)** — the rules for anyone, human or agent, working in this repo.
