# Build stage. Pure-Go build (CGO off) so the runtime image can be the
# tiny distroless static one. Migrations and web/ assets are embedded via
# go:embed so the resulting binary is fully self-contained.
FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# git is needed for `git describe`; the golang image already ships it.
# When the build context lacks a .git directory (e.g. `docker build`
# from a tarball), fall back to "dev" so the build still succeeds —
# runtime/debug.ReadBuildInfo gets vcs.* from the Go toolchain.
#
# `git checkout` restores ONLY the tracked files that .dockerignore
# stripped from the build context — without it, git sees them as
# deleted inside the container and `git describe --dirty` falsely
# tags every build "-dirty". Listing the paths explicitly (rather
# than `git checkout -- .`) means uncommitted edits to real source
# files survive the build, so `make rebuild` actually picks up
# work-in-progress instead of silently reverting to HEAD. Keep this
# list in sync with .dockerignore. Errors are swallowed for the
# no-.git tarball case.
RUN git checkout -- .gitignore TODO.md Dockerfile .dockerignore compose.yaml 2>/dev/null; \
    VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo dev) && \
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
