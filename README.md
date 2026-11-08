# Fever v2

A rewrite of this personal Fever RSS reader. There is one user. It works on desktop
and mobile, and read state is stored in PostgreSQL.

- **Server:** Go (`cmd/fever`, `internal/…`). It is a single binary that serves the
  JSON API and the front end and applies database migrations. It will also refresh
  feeds once step 2 is built.
- **Front end:** SvelteKit 3 (`web/`), built as static files and embedded in the
  binary.
- **Database:** PostgreSQL. v2 tables live in their own `reader` schema, so they can
  sit next to the old `fever_*` tables in the same database.

Status: step 1 (skeleton) is done: schema, login, the importer for the old data, and
the page layout. Feed fetching and the item list come next.

## Configuration

Everything is set with environment variables:

| Variable        | Default  | Meaning |
|-----------------|----------|---------|
| `DATABASE_URL`  | required | PostgreSQL URL, e.g. `postgres://rss:…@postgres:5432/rss` |
| `LISTEN_ADDR`   | `:8080`  | HTTP listen address |
| `PUBLIC_ORIGIN` | empty    | Public URL, e.g. `https://rss.example.com`. Only this origin may make changes, and `https` turns on Secure cookies. |
| `TRUST_PROXY`   | `false`  | Read the client IP and scheme from `X-Forwarded-*`. Set it behind nginx only. |
| `DB_MAX_CONNS`  | `4`      | Maximum number of PostgreSQL connections |
| `GOMEMLIMIT`    | `48MiB` in the image | Go runtime memory target |

## Commands

```
fever serve          run the web server (applies migrations first)
fever migrate        apply migrations and exit
fever set-password   set the login password (signs every device out)
fever import-fever   copy the old Fever data into v2 [--schema public] [--prefix fever_] [--replace]
fever healthcheck    used by the container health check
```

`import-fever` copies feeds, groups, items (with their read and saved state) and
favicons from the `fever_*` tables. It keeps Fever's ids and runs in a single
transaction. With `--replace` it wipes v2's feeds, groups, items and favicons first,
so it can be run again while you keep using the old app. Your password and sessions
are not touched.

What the import changes:
- Item HTML is cleaned: scripts, styles, event handlers and `javascript:` links are
  removed, and relative links and images are made absolute.
- Fever's "unfiltered" display option becomes "text with images".
- Hot, Sparks and the links table are not imported, because v2 does not have them.

## Development

Go runs on your machine. Node only runs in a container: `web/npm` is a drop-in `npm`
that runs inside `node:26-slim`, with `node_modules` kept in a podman volume.

```bash
cp .env.example .env
make dev-db          # PostgreSQL 18 in a container on 127.0.0.1:55432
make dev-sample      # optional: a small fake Fever dataset, in schema fever_sample
make dev-import-sample   # copy the fake dataset into v2 (or dev-import for real tables)
make dev-password    # choose a login password
make web-install     # npm install, in the container
make dev             # Go server on 127.0.0.1:8080 (keep it running)
make web-dev         # in another terminal: Vite on http://localhost:5173
```

Vite forwards `/api` to the Go server through `host.containers.internal`. On macOS the
dev server polls for file changes, because file events don't reach the podman VM.

To test with your real data, load a dump of the old tables into the dev database:

```bash
# on the server (replace "postgres" with your Postgres container's name)
podman exec postgres pg_dump -U rss --table='fever_*' --no-owner --no-privileges rss > fever.sql
# on your machine
podman exec -i fever-dev-db psql -U rss -d rss < fever.sql
make dev-import
```

Other tasks: `make test` (Go vet and tests), `make web-check` (svelte-check and
service worker types), `make generate` (regenerates `internal/store` from the SQL in
`internal/db/queries` with sqlc, and `web/src/lib/api/types.ts` from
`internal/apitypes` with tygo). sqlc and tygo are pinned in `tools/go.mod`.

## Deployment (podman quadlet behind nginx)

1. Build the image on the server, or build it elsewhere for the server's
   architecture:
   ```bash
   podman build -t localhost/fever-v2 .
   podman build --platform linux/amd64 -t localhost/fever-v2 .
   ```
2. Store the database URL as a podman secret. The host is the Postgres container's
   name on your quadlet network. Quadlet names containers `systemd-<unit>` unless the
   unit sets `ContainerName=`; `podman ps` shows the actual name.
   ```bash
   printf '%s' 'postgres://rss:PASSWORD@postgres:5432/rss' | podman secret create fever-v2-database-url -
   ```
3. Copy `deploy/fever-v2.container` to `~/.config/containers/systemd/`. Set
   `Network=` to your Postgres network and `PUBLIC_ORIGIN` to your domain. Then:
   ```bash
   systemctl --user daemon-reload
   systemctl --user start fever-v2
   ```
4. Run the first import and set the password:
   ```bash
   podman exec fever-v2 /fever import-fever
   podman exec -it fever-v2 /fever set-password
   ```
5. Add the server block from `deploy/nginx.conf.example` to nginx.

To update: rebuild the image, then run `podman auto-update` (or restart the unit).
Migrations run automatically at startup.

The container runs as a non-root user with a read-only filesystem, no capabilities
and a 128 MB memory limit. It stores nothing locally: all state is in PostgreSQL.
