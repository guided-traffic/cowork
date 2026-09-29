-- The tenant is the isolation unit: every project, ticket and membership
-- belongs to exactly one tenant. uuidv7() is a PostgreSQL 18 built-in, which
-- is also what pins the minimum server version.
CREATE TABLE tenants (
    id         uuid        PRIMARY KEY DEFAULT uuidv7(),
    slug       text        NOT NULL UNIQUE CHECK (slug ~ '^[a-z0-9][a-z0-9-]{1,62}$'),
    name       text        NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
