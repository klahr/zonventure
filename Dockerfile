# Build stage. Pure-Go build (CGO off) so the runtime image can be the
# tiny distroless static one. Migrations and web/ assets are embedded via
# go:embed so the resulting binary is fully self-contained.
FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

# Runtime image. distroless/static is ~2 MB; no shell, no package manager.
# Pass the Postgres DSN at start time via $DATABASE_URL. The canonical
# deployment is `docker compose up -d` — see compose.yaml. For one-off
# runs against an external Postgres:
#
#   docker run --rm -p 8080:8080 \
#     -e DATABASE_URL='postgres://zv:zv@host.example/zv?sslmode=disable' \
#     zv:latest
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/server /server
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/server"]
