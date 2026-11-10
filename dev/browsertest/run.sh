#!/bin/sh
# Checks the installed-app behavior in a real (headless) Chromium: the service
# worker (offline start, updates), installability, the install button, the
# phone gestures, reading while the server has problems, adding, changing
# and removing a feed, searching, and the keyboard shortcuts. It builds two
# versions of the app and runs them in
# throwaway podman containers, behind nginx as in production, with the fake
# sample data (dev/fever-sample.sql). Nothing else is touched.
#
#   make browser-test
#
# Set BROWSERTEST_PORT to publish Chromium's DevTools on another port than 9222.
set -eu
cd "$(dirname "$0")/../.."

here=dev/browsertest
work=$PWD/$here/.work # absolute: cleanup runs after the cd below
name=fever-browsertest
port=${BROWSERTEST_PORT:-9222}
chrome_image=docker.io/chromedp/headless-shell:158.0.8127.18
nginx_image=docker.io/library/nginx:1.31.6
postgres_image=docker.io/library/postgres:18
run_image=gcr.io/distroless/static-debian13:nonroot

cleanup() {
	podman rm -f -t 0 $name-chrome $name-proxy $name-app $name-db >/dev/null 2>&1 || true
	podman network rm $name >/dev/null 2>&1 || true
	rm -rf "$work"
}
trap cleanup EXIT
trap 'exit 130' INT TERM
cleanup
mkdir -p "$work/current" "$work/next"

# Fails with the command's output, or stays quiet.
quiet() {
	"$@" >"$work/log" 2>&1 || { cat "$work/log"; exit 1; }
}

arch=$(podman info --format '{{.Host.Arch}}')
build() {
	quiet web/npm run build
	CGO_ENABLED=0 GOOS=linux GOARCH=$arch go build -tags embedweb -trimpath -o "$1/fever" ./cmd/fever
}
echo "Building two versions of the app..."
build "$work/current"
build "$work/next" # each build is a new version: SvelteKit's version is the build time

head -c 18 /dev/urandom | base64 >"$work/password"
cat >"$work/env" <<EOF
DATABASE_URL=postgres://rss:test@$name-db:5432/rss?sslmode=disable
PUBLIC_ORIGIN=http://localhost:8080
REFRESH_INTERVAL=1000h
FETCH_ALLOW_PRIVATE=true
EOF

echo "Starting the containers..."
app_files="$work/current:/app:ro,z"
fever() {
	podman run --rm -i --network $name --env-file "$work/env" -v "$app_files" $run_image /app/fever "$@"
}
quiet podman network create $name
quiet podman run -d --name $name-db --network $name \
	-e POSTGRES_USER=rss -e POSTGRES_PASSWORD=test -e POSTGRES_DB=rss $postgres_image
until podman exec $name-db pg_isready -q -U rss -d rss 2>/dev/null; do sleep 1; done
quiet podman exec -i $name-db psql -q -U rss -d rss -v ON_ERROR_STOP=1 <dev/fever-sample.sql
quiet fever import-fever --replace --schema fever_sample
fever set-password <"$work/password" >/dev/null
quiet podman run -d --name $name-app --network $name --network-alias fever.test \
	--env-file "$work/env" -v "$app_files" $run_image /app/fever serve
quiet podman run -d --name $name-proxy --network $name -e NGINX_ENTRYPOINT_LOCAL_RESOLVERS=1 \
	-v "$PWD/$here/nginx.conf.template:/etc/nginx/templates/default.conf.template:ro,z" \
	-v "$PWD/$here/keys.xml:/srv/keys.xml:ro,z" $nginx_image
quiet podman run -d --name $name-chrome --network $name -p 127.0.0.1:$port:9222 --shm-size=1g \
	$chrome_image --user-data-dir=/tmp/profile
# Over plain HTTP, Chromium allows service workers on localhost only.
quiet podman exec -d $name-chrome socat TCP4-LISTEN:8080,fork,reuseaddr TCP4:$name-proxy:8080
until curl -fs http://127.0.0.1:$port/json/version >/dev/null; do sleep 1; done

cd $here
go run . -devtools ws://127.0.0.1:$port/ -password "$work/password" \
	-current "$work/current" -next "$work/next" -app $name-app -proxy $name-proxy
