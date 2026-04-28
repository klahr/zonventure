.PHONY: run seed test test-race vet build restart deploy tidy clean \
        up down logs psql seed-compose

# Default target — `make` alone builds the production binary, ready
# for a systemctl restart.
.DEFAULT_GOAL := build

BIN := zv-server

# Tag-or-commit identifier baked into the binary and surfaced on
# /api/version. Falls back to a short commit hash when no tag points
# at HEAD; appends `-dirty` when the working tree has uncommitted
# changes. Override on the command line for reproducible custom
# tags: `make build VERSION=v1.2.3`.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

# Default DSN for `make run` (host-side Go process talking to the
# compose'd Postgres on 127.0.0.1:5432). Override for ad-hoc setups.
DSN      ?= postgres://zv:zv@127.0.0.1:5432/zv?sslmode=disable
ADMIN    ?= admin:admin
ARRANGER ?= boss:s3cret

# Systemd unit name used by `make restart` / `make deploy`. Override
# per-host if you've named yours differently:
#   make deploy SERVICE=zonventure
SERVICE  ?= zonventure-next

# ---- Native (no Docker) targets ------------------------------------------------

run:
	go run ./cmd/server -dsn '$(DSN)'

seed:
	go run ./cmd/server -dsn '$(DSN)' -seed-admin $(ADMIN) -seed-arranger $(ARRANGER) -seed-demo-adventure

# Production-grade build flags:
#   -trimpath           strip absolute paths from binary (reproducibility)
#   -s -w               strip the symbol + DWARF tables (~30 % smaller)
#   CGO_ENABLED=0       fully static — no glibc dependency at runtime
build:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=$(VERSION)" -o $(BIN) ./cmd/server
	@ls -lh $(BIN)

# Restart the systemd unit. Separated from `build` so the restart only
# fires when the build actually succeeded — `make deploy` chains them.
restart:
	sudo systemctl restart $(SERVICE)
	sudo systemctl status $(SERVICE) --no-pager --lines=5

# One-shot: rebuild + restart. The order matters; if build fails the
# restart never runs and the old binary keeps serving.
deploy:
	docker compose up -d --build

test:
	go test ./...

test-race:
	go test -race ./...

vet:
	go vet ./...

tidy:
	go mod tidy

clean:
	rm -f $(BIN)

# ---- Compose targets ----------------------------------------------------------

# Build (or rebuild) the app image and bring the whole stack up.
up:
	docker compose up -d --build

down:
	docker compose down

logs:
	docker compose logs -f --tail=200

# Drop into a psql shell against the running db service.
psql:
	docker compose exec db psql -U zv -d zv

# Run the seeds inside a one-shot container so the db service is reused.
seed-compose:
	docker compose run --rm app -seed-admin $(ADMIN) -seed-arranger $(ARRANGER) -seed-demo-adventure
