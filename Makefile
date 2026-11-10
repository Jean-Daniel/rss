# Fever v2 development tasks. Node never runs on this machine: every npm
# command goes through web/npm, which runs it in a podman container.

-include .env
export

DEV_DB      := fever-dev-db
DEV_DB_PORT := 55432
IMAGE       := localhost/fever-v2:latest
GO_TOOL     := go tool -modfile=tools/go.mod

.PHONY: help dev web-dev web-install web-check web-test web-build generate test browser-test build build-bin \
        dev-db dev-db-stop dev-sample dev-import dev-import-sample dev-password

help:
	@echo "make dev-db        start a local PostgreSQL container (port $(DEV_DB_PORT))"
	@echo "make dev-sample    load a small fake Fever dataset (schema fever_sample)"
	@echo "make dev-import    copy the Fever tables (public.fever_*) into v2, replacing v2 data"
	@echo "make dev-import-sample  same, from the fake dataset"
	@echo "make dev-password  set the login password"
	@echo "make dev           run the Go server (API on LISTEN_ADDR)"
	@echo "make web-dev       run the Vite dev server in a container on http://localhost:5173"
	@echo "make web-check     type-check the front end"
	@echo "make web-test      run the front end's tests (Node's test runner)"
	@echo "make generate      regenerate sqlc queries and TypeScript API types"
	@echo "make test          run the Go tests"
	@echo "make browser-test  check the installed app in a headless Chromium (containers)"
	@echo "make build         build the container image $(IMAGE)"

# --- Go server ---------------------------------------------------------------

dev:
	go run ./cmd/fever serve

test:
	go vet ./...
	go test -p 1 ./...  # one package at a time: the database tests share TEST_DATABASE_URL

# Service worker, install and phone gestures, in throwaway containers.
browser-test:
	dev/browsertest/run.sh

generate:
	$(GO_TOOL) sqlc generate
	$(GO_TOOL) tygo generate
	web/npm exec -- prettier --log-level warn --write src/lib/api/types.ts

# A local binary with the front end embedded (needs web-build first).
build-bin: web-build
	go build -tags embedweb -trimpath -ldflags="-s -w" -o bin/fever ./cmd/fever

# --- Front end (in a Node container) ------------------------------------------

web-install:
	web/npm install

web-dev:
	NPM_PUBLISH_PORT=127.0.0.1:5173:5173 web/npm run dev -- --host 0.0.0.0

web-check:
	web/npm run check

web-test:
	web/npm test

web-build:
	web/npm run build

# --- Container image -----------------------------------------------------------

build:
	podman build -t $(IMAGE) .

# --- Local database ------------------------------------------------------------

dev-db:
	@podman container exists $(DEV_DB) || podman run -d --name $(DEV_DB) \
		-e POSTGRES_USER=rss -e POSTGRES_PASSWORD=dev -e POSTGRES_DB=rss \
		-p 127.0.0.1:$(DEV_DB_PORT):5432 -v fever-dev-db:/var/lib/postgresql \
		docker.io/library/postgres:18
	@podman start $(DEV_DB) >/dev/null
	@until podman exec $(DEV_DB) pg_isready -q -U rss -d rss; do sleep 1; done
	@echo "PostgreSQL on 127.0.0.1:$(DEV_DB_PORT) (user rss, password dev, database rss)"

dev-db-stop:
	podman stop $(DEV_DB)

dev-sample: dev-db
	podman exec -i $(DEV_DB) psql -q -U rss -d rss -v ON_ERROR_STOP=1 < dev/fever-sample.sql

dev-import:
	go run ./cmd/fever import-fever --replace

dev-import-sample:
	go run ./cmd/fever import-fever --replace --schema fever_sample

dev-password:
	go run ./cmd/fever set-password
