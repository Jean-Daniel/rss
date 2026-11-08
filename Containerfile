# Fever v2: one static Go binary with the SvelteKit front end embedded.
#
#   podman build -t localhost/fever-v2 .
#   podman build --platform linux/amd64 -t localhost/fever-v2 .   # for an x86 server
#
# The build stages run on the build machine's own architecture; Go
# cross-compiles to the target, so no emulation is needed.

FROM --platform=$BUILDPLATFORM docker.io/library/node:26-slim AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json web/.npmrc ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM docker.io/library/golang:1.27 AS server
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY web/*.go ./web/
COPY --from=web /src/web/build ./web/build
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -tags embedweb -trimpath -ldflags="-s -w" -o /out/fever ./cmd/fever

# distroless/static: CA certificates and time zones, no shell, non-root user.
FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=server /out/fever /fever
ENV LISTEN_ADDR=:8080 \
    GOMEMLIMIT=48MiB
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/fever"]
CMD ["serve"]
