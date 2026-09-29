# Runtime behaviour

What the two cowork pods do between being scheduled and serving, and how they behave under
the probes, on shutdown and in their logs. The variables named here are explained one by one
in [README.md, Configuration](../../README.md#configuration).

## The backend

`cowork serve` is the container command. In order:

1. **Configuration.** Every `COWORK_*` variable is read. An invalid value or a missing
   `COWORK_DATABASE_URL` ends the process with exit code 1 and one message listing every
   problem, so one restart fixes all of them.
2. **The migration run**, unless `COWORK_MIGRATE_ON_START=false`. See below.
3. **The connection pool** is opened and pinged. A database that cannot be reached ends the
   process with exit code 1; the pod restarts and tries again, which is the intended behaviour
   while a database is still coming up.
4. **The listener** opens on `COWORK_LISTEN_ADDR` and the log says `listening` with the
   version, the commit and whether a UI is embedded.

## The migration run

The schema lives in the binary as numbered SQL files; the pod applies every file newer than
the version recorded in the `schema_migrations` table, in one transaction per file, before it
listens. Several pods starting at once take a PostgreSQL advisory lock in turn; the first one
applies, the rest log `database schema is current` with `applied=0`.

Two outcomes stop the pod:

- **A migration fails.** The version is recorded as *dirty*, the process exits 1, and every
  further start refuses with `schema version N is dirty: a previous migration failed halfway and
  needs a manual repair`. The repair is manual on purpose: inspect what the failed file did,
  fix the data or the schema, then either clear the dirty flag
  (`UPDATE schema_migrations SET dirty = false`) to retry the same file on the next start, or
  set the version back one to re-run the previous one. Nothing does this automatically.
- **The database is older than PostgreSQL 18.** The first migration uses `uuidv7()`, which
  does not exist before 18; the failure surfaces as a dirty version 1.

`cowork migrate` runs steps 1 and 2 alone and exits; it is what a Job would run if an
installation moves the migration out of the pod start (`COWORK_MIGRATE_ON_START=false`).

### Probes

| Probe | Path | Answers | The chart's default |
|---|---|---|---|
| startup | `/healthz` | 200 as soon as the listener is open, which is after the migration run | every 5 s, up to 36 failures — three minutes for a slow migration |
| liveness | `/healthz` | 200 while the process serves; says nothing about the database | every 10 s |
| readiness | `/readyz` | 200 when a ping on the connection pool succeeds; 503 with the error in the body otherwise | every 10 s |

A pod whose database goes away stays alive and leaves the Service endpoints until the
database is back; it is not restarted for it. While no backend endpoint is ready, the
frontend still serves the UI and answers `/api/` requests with nginx's `502`.

### Shutdown

On `SIGTERM` the server stops accepting connections, finishes in-flight requests for up to
`COWORK_SHUTDOWN_TIMEOUT` (default 15s), closes the listener and the pool, and exits 0. Keep
the timeout below the pod's `terminationGracePeriodSeconds` (chart default 30s); otherwise the
kubelet kills what the server was still draining.

### Log

One line per request (`method`, `path`, `status`, `duration`) and the lifecycle events named
above, at `COWORK_LOG_LEVEL` (default `info`) in the format `COWORK_LOG_FORMAT` (default
`json`; `text` for a terminal). Request bodies, query strings and headers are not logged.
There is no metrics endpoint yet; it is an open question in the planning catalog.

## The frontend

The image is `nginxinc/nginx-unprivileged` with the Angular bundle and one configuration
template. At start the image entrypoint renders the template with `BACKEND_URL` — the chart
sets it to the backend Service, `http://<release>-cowork-backend:8080` — and the cluster
nameservers from `/etc/resolv.conf` into `/etc/nginx/conf.d/default.conf`, and nginx listens
on 8080 as user 101. The backend name is resolved per request (cached 30 s), so the frontend
pod starts and becomes ready whether or not the backend exists yet; `/api/` answers `502`
until it does. Nothing else is configurable; `frontend.extraEnv` exists for the entrypoint's
own switches (`NGINX_ENTRYPOINT_QUIET_LOGS`, for instance).

What nginx does with a request:

| Path | Behaviour |
|---|---|
| `/healthz` | `{"status":"ok"}` from nginx itself — the frontend's liveness and readiness probes; it says nothing about the backend |
| `/api/…` | proxied to `BACKEND_URL` with the path unchanged and `X-Forwarded-For`, `X-Forwarded-Proto`, `X-Real-IP` set; a backend that is down yields `502` |
| hashed bundles (`*.js`, `*.css`, fonts, images) | served with `Cache-Control: public, max-age=31536000, immutable` |
| everything else | `index.html` with `Cache-Control: no-store` — the Angular router resolves the path |

The pod runs with a read-only root filesystem; the chart mounts `emptyDir`s at `/tmp` and
`/etc/nginx/conf.d`, which is all nginx writes, and `fsGroup: 101` is what makes them
writable for the nginx user. If `conf.d` is not writable the entrypoint logs
`/etc/nginx/conf.d is not writable`, skips the template, and nginx serves nothing on 8080 —
the readiness probe then never passes, which is the symptom to look for. Its log is nginx's access and error log on
stdout and stderr. On `SIGTERM` the image's nginx exits within its grace period; there is no
draining beyond nginx's own.
