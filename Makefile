.PHONY: run seed test test-race vet build restart deploy tag tidy clean \
        up down logs psql seed-compose upgrade

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

# Bump the version and create an annotated tag. CHANGELOG.md is the
# human's job — move [Unreleased] entries under a new [X.Y.Z] -
# YYYY-MM-DD heading and commit BEFORE running this so the tag
# points at the commit where master's CHANGELOG.md announces the
# version (the in-app update checker fetches it from there).
#
#   make tag BUMP=major  # 0.1.0 -> 1.0.0
#   make tag BUMP=minor  # 0.1.0 -> 0.2.0
#   make tag BUMP=patch  # 0.1.0 -> 0.1.1
#
# Tag is local-only; push with the command the target prints.
BUMP ?=
tag:
	@if [ -z "$(BUMP)" ]; then \
		echo "Usage: make tag BUMP=major|minor|patch"; \
		echo "Current version: $$(git describe --tags --abbrev=0 2>/dev/null || echo 0.0.0)"; \
		exit 1; \
	fi
	@VERSION=$$(git describe --tags --abbrev=0 2>/dev/null || echo 0.0.0) && \
	MAJOR=$$(echo "$$VERSION" | cut -d. -f1) && \
	MINOR=$$(echo "$$VERSION" | cut -d. -f2) && \
	PATCH=$$(echo "$$VERSION" | cut -d. -f3) && \
	case "$(BUMP)" in \
		major) MAJOR=$$((MAJOR + 1)); MINOR=0; PATCH=0 ;; \
		minor) MINOR=$$((MINOR + 1)); PATCH=0 ;; \
		patch) PATCH=$$((PATCH + 1)) ;; \
		*) echo "Error: BUMP must be major, minor, or patch"; exit 1 ;; \
	esac && \
	NEW="$$MAJOR.$$MINOR.$$PATCH" && \
	git tag -a "$$NEW" -m "Release $$NEW" && \
	echo "$$VERSION -> $$NEW" && \
	echo "Push with: git push origin master $$NEW"

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

# Upgrade an existing Docker Compose deployment to the latest released
# tag. Take a Postgres backup first — see the -backup recipe in
# SELFHOSTING.md. Bails early if there's no tag to check out yet.
#
# We check out the tag (detached HEAD) just long enough for the build
# to stamp `main.version` from `git describe`, then switch back to
# master so the working tree is in a familiar state for the next
# session. The running container keeps the version it was built with.
upgrade:
	git fetch --tags
	@LATEST=$$(git tag --sort=-v:refname | head -1) && \
	if [ -z "$$LATEST" ]; then \
		echo "No release tags found — nothing to upgrade to."; exit 1; \
	fi && \
	echo "Upgrading to $$LATEST" && \
	git checkout "$$LATEST"
	docker compose up -d --build
	git checkout master
