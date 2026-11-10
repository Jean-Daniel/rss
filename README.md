# Fever v2

A rewrite of this personal Fever RSS reader. There is one user. It works on desktop
and mobile, and read state is stored in PostgreSQL.

- **Server:** Go (`cmd/fever`, `internal/…`). It is a single binary that serves the
  JSON API and the front end, refreshes feeds in the background and applies database
  migrations.
- **Front end:** SvelteKit 3 (`web/`), built as static files and embedded in the
  binary.
- **Database:** PostgreSQL. v2 tables live in their own `reader` schema, so they can
  sit next to the old `fever_*` tables in the same database.

Status: steps 1 to 6 are done: schema, login, the importer for the old data, the page
layout, feed refreshing, reading (item lists, mark as read on scroll, saving), the
phone and installed-app polish, managing feeds, groups and preferences, search, and
Fever's keyboard shortcuts. Left for later: dark mode.

## Configuration

Everything is set with environment variables:

| Variable        | Default  | Meaning |
|-----------------|----------|---------|
| `DATABASE_URL`  | required | PostgreSQL URL, e.g. `postgres://rss:…@postgres:5432/rss` |
| `LISTEN_ADDR`   | `:8080`  | HTTP listen address |
| `PUBLIC_ORIGIN` | empty    | Public URL, e.g. `https://rss.example.com`. Only this origin may make changes, and `https` turns on Secure cookies. |
| `TRUST_PROXY`   | `false`  | Read the client IP and scheme from `X-Forwarded-*`. Set it behind nginx only. |
| `DB_MAX_CONNS`  | `4`      | Maximum number of PostgreSQL connections |
| `REFRESH_INTERVAL` | `15m` | How often each feed is fetched (at least `5m`) |
| `FETCH_ALLOW_PRIVATE` | `false` | Allow feeds on loopback or private addresses (your LAN, other containers) |
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
are not touched. The wiped tables stay locked until the import is finished: a running
v2 server answers nothing that needs feeds or items meanwhile (pages wait, then show
the new data).

What the import changes:
- Item HTML is cleaned: scripts, styles, event handlers and `javascript:` links are
  removed, and relative links and images are made absolute.
- Fever's "unfiltered" display option becomes "text with images".
- Hot, Sparks and the links table are not imported, because v2 does not have them.

## Feed refreshing

Every minute the server fetches the feeds that are due, four at a time, then
schedules each one again `REFRESH_INTERVAL` later (with a little random spread). It
sends the feed's `ETag`/`Last-Modified` back, so unchanged feeds cost a short "not
modified" answer. The refresh button in the sidebar makes every feed due at once.

- **Failures:** a failing feed is retried after twice the refresh interval, and the
  wait doubles with each failure in a row: 30 minutes, then 1, 2, 4, 8 and 16 hours
  with the default interval. A site's `Retry-After` is respected when it asks for
  longer, and the wait is never more than a day. The message is shown on the feed.
- **Retention:** like Fever, items are kept for 10 weeks unless saved, and expired
  items are deleted every hour. Older entries that a feed still lists are not added
  again: the server remembers the IDs of expired items while their feed lists them,
  which also covers entries without a date. They are forgotten once the feed has
  left them out for 30 days, so a feed that serves an empty or partial document
  for a while does not bring its old entries back as unread.
- **Failed saves:** if new items cannot be stored, the feed is marked as failed and
  keeps its previous `ETag`, so the next attempt downloads it again. A refresh
  whose outcome cannot be recorded is a failure too, retried with the same growing
  waits rather than every minute.
- **Favicons:** for feeds without one, the server looks at the site's home page for
  icon links, then `/favicon.ico`, at most once a week per feed.
- **Safety:** feeds cannot point the server at loopback or private addresses (such as
  the database container) unless `FETCH_ALLOW_PRIVATE=true`. Downloads are size
  limited (8 MB per feed, 256 KB per icon) and time limited.

## Reading

Lists show unread items, oldest first, full articles with images, 20 at a time, as
the old Fever setup did; "All" shows read items too (remembered per device). These
defaults can be changed in Settings, and each group or feed can set its own order,
images and unread count. Saved items are listed newest first.

- **Mark as read on scroll:** an item is marked read once its bottom edge goes above
  the top of the window (below the top bar on phones), after it has been on screen.
  Opening an article also marks it read; an item marked unread by hand stays unread.
  The end of a list leaves a screen of room so the last items can scroll past too.
- **No lost reads:** changes are kept in the browser's storage and sent in batches
  every 2 seconds, and right away when the page is hidden or closed; anything not
  confirmed is sent again later. If the session has ended, the login page opens,
  and they are sent after logging in.
- **Other devices:** read and saved changes made elsewhere are picked up when the app
  comes back to the foreground, and every 5 minutes.
- **Mark all as read** only covers items that had arrived when the list was shown.
  Changes not sent yet are sent first, so an item marked unread a moment before
  ends up read like the others.
- Feed HTML is sanitized on the server, and a Content Security Policy blocks any
  script that is not the app's own.

## Search

Search, under Saved in the sidebar, finds the items whose title, author or text has
every word given, or a word starting with it from three letters on ("swift" finds
"SwiftUI"), whatever the case and accents ("cafe" finds "Café"). Results are listed
newest first, read or not, and are not marked read on scroll. It covers what the
server keeps: the last 10 weeks, and saved items.

It uses PostgreSQL's full-text search: the words of each item (HTML tags and links
left out, no stemming, so it works the same in every language) are kept in an index
of their own, updated when items are added or changed. Only the first 64,000
characters of an article's text count: PostgreSQL limits how many words one item
may index. The first start after the update indexes every stored item, which takes a
moment on a large database.

Ignoring accents needs PostgreSQL's `unaccent` extension, which the migration
installs. If the database user may not install it, the migration says so in a notice
and search keeps the accents. To add it later, run as a PostgreSQL superuser, in the
database (`reader` is v2's schema; if `unaccent` is already installed in another
schema, name that one in the second statement):

```sql
CREATE EXTENSION IF NOT EXISTS unaccent SCHEMA reader;
ALTER TEXT SEARCH CONFIGURATION reader.fever_search
    ALTER MAPPING FOR word, hword, hword_part, numword, numhword, hword_numpart
    WITH reader.unaccent, simple;
UPDATE reader.item_search s SET search = reader.search_vector(i.title, i.author, i.content)
FROM reader.items i WHERE i.id = s.item_id;
```

## Keyboard shortcuts

Fever's shortcuts, minus those of what v2 does not have. `?` shows them, and so does
Settings.

| Keys | Does |
|------|------|
| `J` / `K` | Next / previous item (the item left behind is read, as when scrolling) |
| `Space` / `Shift Space` | Next / previous item when it is in view, else a page down / up |
| `O` or `→` | Open the current item in a new tab (marks it read) |
| `S` | Save or unsave the current item |
| `V` / `Shift V` | Visit the current item's site / and mark its feed as read |
| `U` | Show or hide read items |
| `A` / `Shift A` | Mark the group or feed shown / everything as read (asks first) |
| `Z` | Mark the items read last (within 15 seconds of the last one, on any device) as unread again |
| `2` / `3` / `5` | All items / Saved / Search |
| `/` | Search |
| `F` | Show or hide the feed list |
| `R` / `Shift R` | Refresh all feeds / the group or feed shown |
| `N` / `Shift N` | Add a feed / a group |
| `I` | Edit the group or feed shown |
| `P` | Settings |
| `Esc` | Close the list of shortcuts, the menu or the feed list; leave the search field |

The current item is the first one at the top of the reading area; once keys are used,
it is outlined. Letters follow the keyboard layout (A is A on AZERTY too) and digits
work without Shift there, as in Fever. Not carried over: the arrow keys moving
between groups, feeds and items, `1` and `4` (Hot, Sparks), `B` (blacklist) and `0`
(excerpts).

## Managing feeds and groups

Settings (the sliders button next to the logo) lists the groups and feeds, with each
feed's state, and holds the reading preferences. The pencil button above a group's
or a feed's list opens its settings.

- **Adding a feed:** give the address of the feed, or of the site: a site's page
  usually links to its feed. If it links to several (posts and comments, say), you
  choose. An address without `http://` is tried with `https://`. The feed's items
  are stored right away, as unread, back to the 10-week retention period.
- **A feed's settings:** its name (empty to use the feed's own title), address,
  groups, display options, and a username and password for feeds behind a login
  (the username may be empty, for feeds that only want a token). A new address or
  login is fetched again right away. A login given with a site's address is not
  kept if the site's feed is on another site.
- **Unsubscribing** deletes the feed's items, saved ones included, as in Fever.
  Deleting a group keeps its feeds.
- **OPML:** export writes every feed, by group, for another reader. Import adds the
  feeds you do not have yet (fetched in the background) and puts feeds in the
  file's groups, creating the groups that do not exist, empty ones included. Files
  in UTF-8, ISO-8859-1, ISO-8859-15 and Windows-1252 are read.
- The password is changed on the server with `fever set-password`.

Fever's blacklist is not carried over: it only kept links out of Hot, which v2 does
not have.

## Phones and the installed app

On narrow screens the groups and feeds are in a drawer: open it with the menu button,
close it with a swipe to the left, a tap outside it, or Escape. Tapping the title
bar goes back to the top of the list.

Fever can be installed as an app (it needs HTTPS):
- **iPhone and iPad (Safari):** Share → Add to Home Screen.
- **Android, and Chrome or Edge on a computer:** the "Install app" button under the
  feed count in the sidebar (in the drawer on phones), or the browser's install menu.
- **Safari on a Mac:** File → Add to Dock.

A service worker keeps the app's own files on the device, so the app starts without
downloading them; a page load waits at most 3 seconds for the server before using
that copy. Articles, read state and counts always come from the server: without a
connection, with the server down (nginx answering "502 Bad Gateway"), or when it
does not answer within 10 seconds, the app opens and says the server cannot be
reached.

**Updates:** when a new version is deployed, the app picks it up in the background
and shows "A new version of Fever is available" with a Reload button, so you are
never interrupted mid-article. Reads made before the reload are kept and sent. The
previous version's files stay cached until the next update, so an app still open
on the old version keeps working.

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

Other tasks: `make test` (Go vet and tests; set `TEST_DATABASE_URL` to a scratch
database to include the database tests, which wipe the v2 tables there), `make
web-check` (svelte-check and service worker types), `make web-test` (the front
end's tests, with Node's own test runner), `make generate` (regenerates
`internal/store` from the SQL in `internal/db/queries` with sqlc, and
`web/src/lib/api/types.ts` from `internal/apitypes` with tygo, formatted with
Prettier). sqlc and tygo are pinned in `tools/go.mod`.

`make browser-test` checks what only a browser can: the service worker (offline
start, server down behind nginx, slow server, updates), installability, the install
button, the phone gestures, reading while things go wrong (counts that cannot be
reloaded, a refresh in progress, "mark all as read" right after a change, a session
that has ended), adding, renaming and removing a feed, searching, and the keyboard
shortcuts. It builds two versions of the app and runs them with
nginx, the fake dataset and a headless Chromium in throwaway podman containers,
removed afterwards. The checks are in `dev/browsertest` (a separate Go module, so
the server does not depend on chromedp).

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
