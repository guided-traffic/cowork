# Conventions

What every change in this repository follows.

- **Commits:** conventional commits (`feat`, `fix`, `chore`, `docs`, `refactor`, `test`, `ci`),
  a scope where one exists (`backend`, `frontend`, `chart`, `ci`, …); **no apostrophe anywhere
  in a commit message**; a `!` or `BREAKING CHANGE:` footer for a breaking change.
  semantic-release reads them. A pull request is squashed under its title, so the title
  follows the same rules ([ci-and-release.md](ci-and-release.md)).
- **Go:** `gofmt -s`, `goimports` with the module as local prefix, the golangci-lint set in
  [`backend/.golangci.yml`](../../backend/.golangci.yml); errors wrapped with `%w` and a verb ("parse
  database url: …"); `log/slog` with key-value pairs; no global state beyond the linker
  variables and the `init()` in [`api/validate.go`](../../backend/internal/api/validate.go),
  which registers the `text/markdown` body decoder and the `uuid` format in kin-openapi's
  process-wide registries.
- **Generated code** — the bundled API document, `apigen`, `readq`, `writeq`, the problem-code
  enum and README table — is committed and never edited; `make generate` writes it and
  `make generate-check` fails CI on a difference.
- **SQL** that reads or writes data is a named query in `backend/internal/store/queries/read/`
  or `write/`; the one query built at run time is the ticket list builder, and no filter value
  ever enters SQL text. The store's own fixed statements — `set_config`, the advisory locks,
  `pg_notify` and `LISTEN`, the role checks and the schema state — live in its Go files. A query that
  reads a ticket or a project calls the visibility predicate or names its exemption with
  `-- visibility: exempt (<why>)` ([data-access.md](data-access.md#visibility-in-sql)).
- **Acts:** a request's write commits through `store.Mutate`, a job's through `store.RunJob`
  (the writes outside both are bookkeeping, not an act of a person: the token's last-used day, a
  session's idle clock, and the login's attempt count, `store.RecordLoginAttempt`, whose refusals
  are rows of `system:login`), and every act is one `store.Event`
  passed to `Writer.Record` — `Before` and `After` hold the changed fields only. A request that
  changes nothing returns `store.ErrNoChange` from the function and answers the current state; a
  mutation that records no act does not commit. A comment's text never enters an act.
- **A conditional write that finds no row lost a race:** a `:one` query whose `WHERE` holds more
  than the id (`revoked_at IS NULL`, `status = 'open'`, a version) answers `pgx.ErrNoRows` when
  a simultaneous request came first. The handler answers as the later request would — re-read
  and `store.ErrNoChange` when the state it wanted is there, `412` on a moved version, `409` on
  another state — never `500`; the integration tests race such writes with `simultaneously`.
- **Names in acts:** entity types, actions and field names are constants — the ones several
  handlers record in [`internal/api/server.go`](../../backend/internal/api/server.go), one
  entity's beside its handler (`entityTicket` in `tickets.go`, …); golangci-lint's `goconst`
  refuses a string literal repeated three times outside the tests. An action must be a value of
  the `audit_action` enum (migration `000005`) and of the document's `AuditAction` schema.
- **Audit row ids are made in Go** (`uuid.NewV7` in `Writer.writeEvents`), never read back with
  `RETURNING`: an installation-level row of a system actor would not pass the read policy. The ids
  of a person, a tenant and a membership are made in Go too, for the same reason: the person who
  creates the row does not yet pass its read policy.
- **A secret has one way out.** A password, a session cookie, a token and a hash are in no log
  line, no audit row, no error text and no response — except the answer that creates the token,
  which shows its plaintext once. A configuration error names the variable and never the value. A
  new route that takes a secret extends `TestNoPasswordCookieOrTokenIsLoggedOrRecorded`.
- **One clock for the login.** A limit of a session, a lock or the address throttle is judged by
  `api.Options.Now` — the backend's clock — and never by the database's `now()` or a date a client
  sent, so a test moves one clock and ages all of them ([testing.md](testing.md)).
- **Invisible is absent:** a row the caller may not see answers exactly like one that does not
  exist — `404`, an absent list entry, an act without its payload.
- **Errors** are `problem.*` values with a catalogue code; the detail is for a person and never
  carries a secret, SQL or an internal path — the cause goes to the log under the request id.
- **Tests:** `testify` (`require` for preconditions, `assert` for the claim); table tests as
  `map[string]…` with `t.Run`; a test name states the behaviour
  (`TestKnownPathsRejectOtherMethods`).
- **Angular:** standalone components, signals, `inject()`, OnPush by default in Angular 22,
  SCSS, Prettier as generated by the CLI, ESLint through `ng lint`. PrimeNG components imported
  one by one and never a deprecated one (`[pButton]`, not `<p-button>`); colours only as the
  preset's tokens; `class` on a PrimeNG host where older code wrote `styleClass`; a resource read
  through `hasValue()` and reloaded through `refresh()`; vocabulary values shown as the API spells
  them, with their meaning in a tooltip ([frontend.md](frontend.md)). Text is shown by
  interpolation; the one markup the page shows is the server's rendered Markdown, through
  `RenderedText` and Angular's sanitiser, and nothing calls `bypassSecurityTrust…`
  ([rendered-markdown.md](rendered-markdown.md)).
- **Documentation:** every claim verified against the tree; "not verified" is a complete
  sentence; file and line references as relative links; `# default` / `# example` on shown
  values; nothing outside `docs/tickets/` cites a ticket.
- **Security:** a request that weakens auth, secrets, TLS, permissions, isolation, validation
  or exposure is named as such and discussed before it is implemented; an accepted risk is
  written down in the security page it belongs to.
