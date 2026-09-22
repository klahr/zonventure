# Frontend stage. The React apps under web/ compile to
# internal/server/web/dist/<app>/, which the Go build then embeds. Split
# into its own stage so Node never reaches the runtime image and so the
# dependency install caches on the lockfile alone.
FROM node:22-alpine AS web
WORKDIR /src
# The whole workspace in one copy. A manifests-first layer would cache
# `npm ci` more tightly, but `COPY web/apps/*/package.json` flattens
# under Docker's wildcard rules and collides the moment there is a
# second app — not worth the sharper cache on a tree this size.
#
# Copying everything also guarantees tsconfig.base.json is present. Each
# app's tsconfig extends it; without it tsc falls back to the default
# moduleResolution, under which Vite's own "#types/*" subpath imports do
# not resolve, and the build dies in a wall of TS2307s that point
# nowhere near the actual cause. node_modules is excluded by
# .dockerignore, so the host's install never leaks in.
COPY web ./web
RUN cd web && npm ci && npm run build --workspaces --if-present

# Build stage. Pure-Go build (CGO off) so the runtime image can be the
# tiny distroless static one. Migrations and web/ assets are embedded via
# go:embed so the resulting binary is fully self-contained.
FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Overwrite the committed placeholder with the real build output. This
# COPY is what makes the single-binary story hold: without it the image
# would ship the "frontend not built" stub.
COPY --from=web /src/internal/server/web/dist ./internal/server/web/dist
# Version stamping. The caller supplies VERSION — the Makefile and
# compose pass `git describe --tags --always --dirty` from the host,
# and CI passes the tag it is building. That is the accurate source,
# because only the host can tell whether the working tree is dirty.
#
# The in-container fallback exists for a bare `docker build .` with no
# arg. It deliberately omits --dirty: .dockerignore strips a handful of
# tracked files from the build context, so git inside the container sees
# them as deleted and --dirty would mark every build dirty regardless of
# the real tree state. A previous revision worked around that by running
# `git checkout` on an explicit file list that had to stay in sync with
# .dockerignore by hand; passing the version in removes the need.
#
# `|| echo dev` covers a context with no .git at all (e.g. a tarball).
ARG VERSION=""
RUN VERSION="${VERSION:-$(git describe --tags --always 2>/dev/null || echo dev)}" && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=$VERSION" -o /out/server ./cmd/server

# Runtime image. distroless/static is ~2 MB; no shell, no package
# manager. The canonical self-host path is `docker compose up -d` —
# see compose.yaml + SELFHOSTING.md. For one-off runs against an
# external Postgres:
#
#   docker run --rm -p 8080:8080 \
#     -e DATABASE_URL='postgres://zv:zv@host.example/zv?sslmode=disable' \
#     -e ZV_SEED_ADMIN='admin:hunter2' \
#     zv:latest
#
# Backups: the CLI flow is the only supported path —
#   docker compose exec -T app /server -backup > zv.sql
# (or against the postgres container directly with pg_dump). No
# in-app endpoint and intentionally no postgres-client tools in
# this image; that keeps the runtime tiny + no-shell.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/server /server
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/server"]
