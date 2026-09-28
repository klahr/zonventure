.PHONY: help up down rebuild logs psql reset seed run \
        test test-race vet tidy build tag upgrade \
        web web-dev web-clean

# `make` alone prints the menu so a new contributor sees what's
# available without grepping the file.
.DEFAULT_GOAL := help

# --- Knobs (override on the command line where useful) -----------------------

BIN      := zonventure
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
# Exported so `docker compose build` picks it up as a build arg — only
# the host can tell whether the working tree is dirty. See Dockerfile.
export VERSION

# Native `make run` talks to the compose'd Postgres on the host.
DSN      ?= postgres://zv:zv@127.0.0.1:5432/zv?sslmode=disable

# `make seed` credentials. Idempotent — the server skips when the
# user already exists, so re-running on an active stack is harmless.
ADMIN    ?= admin:admin
ARRANGER ?= boss:s3cret
PUBLISHER ?= publisher:publisher

# --- help --------------------------------------------------------------------

help:
	@echo "Zonventure — common targets"
	@echo ""
	@echo "  Local development (Docker Compose)"
	@echo "    make up         Start the stack (builds the app image)"
	@echo "    make down       Stop the stack"
	@echo "    make rebuild    Force a clean rebuild + restart the app"
	@echo "    make reset      Wipe the Postgres volume and start fresh (DESTRUCTIVE)"
	@echo "    make logs       Tail compose logs"
	@echo "    make psql       Open a psql shell against the dev database"
	@echo "    make seed       Seed admin / arranger / demo adventure (idempotent)"
	@echo ""
	@echo "  Native Go (uses the compose'd Postgres on 127.0.0.1:5432)"
	@echo "    make run        go run ./cmd/server"
	@echo ""
	@echo "  Tests"
	@echo "    make test       go test ./..."
	@echo "    make test-race  go test -race ./..."
	@echo "    make vet        go vet ./..."
	@echo "    make tidy       go mod tidy"
	@echo ""
	@echo "  Frontend (React apps under web/, embedded into the binary)"
	@echo "    make web        Build the React apps into internal/server/web/dist"
	@echo "    make web-dev    Vite dev server, proxying /api to :8080"
	@echo "    make web-clean  Remove build output, keeping the committed stub"
	@echo ""
	@echo "  Build & release"
	@echo "    make build      Production binary (trimpath, stripped, static)"
	@echo "    make tag BUMP=patch|minor|major"
	@echo "                    Cut an annotated release tag"
	@echo "    make upgrade    Check out the latest tag and rebuild the stack"
	@echo ""
	@echo "  After 'make up': http://localhost:8080  (admin / admin)"

# --- Local development (Docker Compose) --------------------------------------

# `--build` is cheap when nothing changed (layer cache hits) and the
# right thing to do when Go source changed since the last run. The
# compose.override.yaml mounts internal/server/web read-only into the
# container, so HTML/CSS/JS edits show up on a plain browser refresh
# without going through this target at all.
up:
	docker compose up -d --build
	@echo "Up at http://localhost:8080  (admin / admin)"

down:
	docker compose down

# Force a clean app rebuild without wiping the database. Useful when
# the layer cache is stale or you're testing a build flag change.
rebuild:
	docker compose build --no-cache app
	docker compose up -d
	@echo "Rebuilt; app at http://localhost:8080"

logs:
	docker compose logs -f --tail=200

psql:
	docker compose exec db psql -U zv -d zv

# DESTRUCTIVE — drops the Postgres volume so you start from migrations
# on the next boot. Useful for testing first-run flows.
reset:
	@printf "This deletes the dev Postgres volume. Continue? [y/N] " && read ans && [ "$$ans" = "y" ]
	docker compose down -v
	docker compose up -d --build
	@echo "Fresh stack at http://localhost:8080"

# Seed users + demo adventure. Idempotent: each row checks first. The
# publisher login authors the catalog (the demo adventure lives in its
# org); the admin only manages orgs.
seed:
	docker compose run --rm app \
	  -seed-admin $(ADMIN) \
	  -seed-arranger $(ARRANGER) \
	  -seed-publisher $(PUBLISHER) \
	  -seed-demo-adventure

# --- Native Go run (no Docker for the app) -----------------------------------

# Useful for fast Go iteration: edit, save, Ctrl-C, `make run`. Talks
# to the compose'd Postgres on the host port. Bring it up first with
# `make up` (or run `docker compose up -d db` for just the database).
run:
	go run ./cmd/server -dsn '$(DSN)'

# --- Tests -------------------------------------------------------------------

# Most DB-bound tests skip when no Postgres is reachable. Set
# ZV_TEST_DSN to point at the compose'd Postgres to actually run them:
#   ZV_TEST_DSN=postgres://zv:zv@127.0.0.1:5432/postgres?sslmode=disable make test
test:
	go test ./...

test-race:
	go test -race ./...

vet:
	go vet ./...

tidy:
	go mod tidy

# --- Build -------------------------------------------------------------------

# Production-grade build flags:
#   -trimpath     strip absolute paths from the binary (reproducible)
#   -s -w         strip symbol + DWARF tables (~30 % smaller)
#   CGO_ENABLED=0 fully static — no glibc dependency at runtime
# Depends on `web` so a release binary can never ship a stale bundle.
# `test` deliberately does NOT, so `go test ./...` stays fast and needs
# no Node — the SPA tests skip when the apps aren't built.
build: web
	CGO_ENABLED=0 go build -trimpath \
	  -ldflags="-s -w -X main.version=$(VERSION)" \
	  -o $(BIN) ./cmd/server
	@ls -lh $(BIN)

# --- Frontend ----------------------------------------------------------------

# The React apps build into internal/server/web/dist/<app>/, which
# internal/server/spa.go embeds. One npm workspace, no extra monorepo
# orchestration tool: the graph is a handful of apps and shared
# packages, so Turborepo/Nx would be more configuration than code.
#
# `npm ci` only when node_modules is absent — it deletes and reinstalls
# the tree every run, which is right for CI and needlessly slow here.
web:
	@cd web && [ -d node_modules ] || npm ci
	cd web && npm run build --workspaces --if-present

web-dev:
	cd web && npm run dev

# Leaves internal/server/web/dist/index.html in place. Removing that
# stub breaks `go build` outright, because //go:embed is a compile-time
# error when its pattern matches nothing.
web-clean:
	find internal/server/web/dist -mindepth 1 -maxdepth 1 \
	  ! -name index.html -exec rm -rf {} +

# --- Release -----------------------------------------------------------------

# Cut a new annotated tag. CHANGELOG.md is the human's job — move
# [Unreleased] entries under a new [X.Y.Z] - YYYY-MM-DD heading and
# commit BEFORE running this so the tag points at the commit where
# main's CHANGELOG.md announces the version (the in-app update
# checker fetches it from there).
#
#   make tag BUMP=major   0.1.0 -> 1.0.0
#   make tag BUMP=minor   0.1.0 -> 0.2.0
#   make tag BUMP=patch   0.1.0 -> 0.1.1
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
	echo "Push with: git push origin main $$NEW"

# Upgrade an existing Docker Compose deployment to the latest released
# tag. Take a Postgres backup first — see SELFHOSTING.md.
#
# Checks out the tag (detached HEAD) just long enough for the build
# to stamp `main.version` from `git describe`, then switches back to
# main so the working tree is in a familiar state for the next
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
	git checkout main
