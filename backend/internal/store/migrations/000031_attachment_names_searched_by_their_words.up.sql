-- An attachment's file name is part of its ticket's searchable text
-- (docs/adr/0025 D5). The parser reads a name such as shot.png or
-- release-notes_v2.txt as one word, so a search for shot found nothing: the
-- vector now holds the whole name and its words split at dots, hyphens and
-- underscores. The rewrite recomputes every row, past row-level security,
-- which governs queries and not the rewrite of a table; the index on the
-- column is rebuilt with it. Nothing the previous release reads changes
-- (docs/adr/0028 D3): no route of it searched the column.
ALTER TABLE attachments ALTER COLUMN search SET EXPRESSION AS (
    to_tsvector('cowork_simple', file_name || ' ' || translate(file_name, '._-', '   ')));
