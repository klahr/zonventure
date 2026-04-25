.PHONY: run seed test test-race vet build tidy clean \
        up down logs psql seed-compose

BIN := zv-server

# Default DSN for `make run` (host-side Go process talking to the
# compose'd Postgres on 127.0.0.1:5432). Override for ad-hoc setups.
DSN      ?= postgres://zv:zv@127.0.0.1:5432/zv?sslmode=disable
ADMIN    ?= admin:admin
ARRANGER ?= boss:s3cret

# ---- Native (no Docker) targets ------------------------------------------------

run:
	go run ./cmd/server -dsn '$(DSN)'

seed:
	go run ./cmd/server -dsn '$(DSN)' -seed-admin $(ADMIN) -seed-arranger $(ARRANGER) -seed-demo-adventure

build:
	go build -o $(BIN) ./cmd/server

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
