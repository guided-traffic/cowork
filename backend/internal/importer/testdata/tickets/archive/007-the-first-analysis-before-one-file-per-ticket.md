# The first analysis, before one file per ticket

A record from before the rule of one file per ticket: several items in one
file, and no frontmatter.

1. The pool is exported, and any package can reach it.
2. No query sets its tenant before it runs.
3. Nothing checks the SQL against the schema.

The data-access layer of T4 settles all three.
