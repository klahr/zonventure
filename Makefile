.PHONY: help up down rebuild logs psql reset seed run \
        test test-race vet tidy build major minor patch _bump latest upgrade \
        web web-dev web-clean theme-cover illustration

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
	@echo "    make theme-cover ID=pirate SRC=~/Downloads/pic.jpg"
	@echo "                    A starter theme's cover picture, for web and print"
	@echo "    make illustration ID=no-events SRC=~/Downloads/pic.jpg [WIDE=1] [EMAIL=1]"
	@echo "                    A picture for the product's screens"
	@echo ""
	@echo "  Build & release"
	@echo "    make build      Production binary (trimpath, stripped, static)"
	@echo "    make patch|minor|major"
	@echo "                    Release: promote CHANGELOG [Unreleased], commit, tag"
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
	cp web/node_modules/maplibre-gl/dist/maplibre-gl.mjs web/node_modules/maplibre-gl/dist/maplibre-gl-shared.mjs \
		web/node_modules/maplibre-gl/dist/maplibre-gl-worker.mjs web/node_modules/maplibre-gl/dist/maplibre-gl.css \
		internal/server/web/assets/maplibre/

web-dev:
	cd web && npm run dev

# Leaves internal/server/web/dist/index.html in place. Removing that
# stub breaks `go build` outright, because //go:embed is a compile-time
# error when its pattern matches nothing.
web-clean:
	find internal/server/web/dist -mindepth 1 -maxdepth 1 \
	  ! -name index.html -exec rm -rf {} +

# theme-cover makes a starter theme's cover picture from a 16:9 source
# (subject on the right, left side calm): web sizes for play-ui and a
# JPEG for print. The theme then needs its line in play-ui's covers.ts.
theme-cover:
	@test -n "$(ID)" -a -f "$(SRC)" || { echo "usage: make theme-cover ID=<theme id> SRC=<image>"; exit 1; }
	magick "$(SRC)" -resize 800x -quality 78 web/packages/play-ui/src/covers/$(ID)-800.webp
	magick "$(SRC)" -resize 1600x -quality 80 web/packages/play-ui/src/covers/$(ID)-1600.webp
	magick "$(SRC)" -resize 1800x -strip -quality 80 internal/trail/covers/$(ID).jpg
	magick "$(SRC)" -gravity center -crop '%[fx:w]x%[fx:w/2]+0+0' +repage -resize 1200x600 -strip -quality 78 internal/server/web/assets/email/covers/$(ID).jpg
	@echo "Now add $(ID) to THEME_COVERS in web/packages/play-ui/src/covers.ts and to CoverThemes in internal/emailtpl/pictures.go."

# A spot is square, 480 px; WIDE=1 makes a banner or scene in two sizes,
# 800 px and the source's own width up to 1600 px.
illustration:
	@test -n "$(ID)" -a -f "$(SRC)" || { echo "usage: make illustration ID=<id> SRC=<image> [WIDE=1]"; exit 1; }
ifeq ($(WIDE),1)
	magick "$(SRC)" -resize 800x -strip -quality 78 web/packages/ui/src/illustrations/$(ID)-800.webp
	magick "$(SRC)" -resize '1600x>' -strip -quality 80 web/packages/ui/src/illustrations/$(ID)-wide.webp
	@echo "Now add $(ID) to WIDE in web/packages/ui/src/illustrations.ts, with the wide file's width."
else
	magick "$(SRC)" -resize 480x480 -strip -quality 80 web/packages/ui/src/illustrations/$(ID).webp
	@echo "Now add $(ID) to SPOTS in web/packages/ui/src/illustrations.ts."
endif
ifeq ($(EMAIL),1)
	magick "$(SRC)" $(if $(filter 1,$(WIDE)),-gravity center -crop '%[fx:w]x%[fx:w/3]+0+0' +repage -resize '1200x400!',-resize 280x280) -strip -quality 80 internal/server/web/assets/email/$(ID).jpg
	@echo "Now add $(ID) to housePictures in internal/emailtpl/pictures.go."
endif

# --- Release -----------------------------------------------------------------

# major/minor/patch cut an annotated release tag, bumping from the latest
# release tag (0.0.0 if none). CHANGELOG.md's [Unreleased] entries move
# under a new [X.Y.Z] - date heading, an empty [Unreleased] is left
# behind, and the change is committed so the tag points at the commit
# where main's CHANGELOG.md announces the version (the in-app update
# checker fetches it from there). An EMPTY [Unreleased] aborts: nobody
# wrote down what ships.
#
#   make patch   0.1.0 -> 0.1.1
#   make minor   0.1.0 -> 0.2.0
#   make major   0.1.0 -> 1.0.0
#
# The tag is local; the recipe prints the push command.
major:
	@$(MAKE) --no-print-directory _bump BUMP=major
minor:
	@$(MAKE) --no-print-directory _bump BUMP=minor
patch:
	@$(MAKE) --no-print-directory _bump BUMP=patch

# set -e: a failed `git commit` must not fall through to `git tag`, which
# would tag the previous HEAD as the new release.
_bump:
	@set -e; \
	CUR=$$($(LATEST_TAG)); CUR=$${CUR:-0.0.0}; \
	MAJOR=$$(echo "$$CUR" | cut -d. -f1); \
	MINOR=$$(echo "$$CUR" | cut -d. -f2); \
	PATCH=$$(echo "$$CUR" | cut -d. -f3); \
	case "$(BUMP)" in \
		major) MAJOR=$$((MAJOR + 1)); MINOR=0; PATCH=0 ;; \
		minor) MINOR=$$((MINOR + 1)); PATCH=0 ;; \
		patch) PATCH=$$((PATCH + 1)) ;; \
		*) echo "Use: make major|minor|patch"; exit 1 ;; \
	esac; \
	NEW="$$MAJOR.$$MINOR.$$PATCH"; \
	if grep -q "^## \[$$NEW\]" CHANGELOG.md; then \
		echo "CHANGELOG.md already has a [$$NEW] heading — leaving it alone"; \
	else \
		if [ "$$(awk '/^## \[Unreleased\]/{f=1;next} /^## \[/{f=0} f' CHANGELOG.md | grep -c '[^[:space:]]')" -eq 0 ]; then \
			echo "REFUSING: CHANGELOG.md [Unreleased] is empty — nothing to release."; \
			echo "Write what ships under [Unreleased] first, or add a [$$NEW] heading by hand."; \
			exit 1; \
		fi; \
		awk -v h="## [$$NEW] - $$(date +%F)" '{print} /^## \[Unreleased\]$$/ && !d {print ""; print h; d=1}' \
			CHANGELOG.md > CHANGELOG.md.tmp && mv CHANGELOG.md.tmp CHANGELOG.md; \
		echo "CHANGELOG.md: [Unreleased] promoted to [$$NEW]"; \
	fi; \
	git add CHANGELOG.md; \
	git commit -q -m "Release $$NEW" -- CHANGELOG.md; \
	git tag -a "$$NEW" -m "Release $$NEW"; \
	echo "$$CUR -> $$NEW (committed CHANGELOG.md, tagged)"; \
	echo "Push with: git push origin main $$NEW"

# LATEST_TAG is the newest RELEASE tag: only X.Y.Z tags (a `latest` or
# `nightly` tag would sort above every version), no pre-releases (version
# sort puts 1.0.0-rc1 ahead of 1.0.0), ordered numerically (0.10.0 beats
# 0.9.0). Expanded by the recipes, not $(shell), so plain `make` runs no git.
LATEST_TAG = git tag -l '[0-9]*.[0-9]*.[0-9]*' --sort=-v:refname | grep -Ex '[0-9]+\.[0-9]+\.[0-9]+' | head -1

latest:
	@$(LATEST_TAG)

# Upgrade an existing Docker Compose deployment to the latest released
# tag. Take a Postgres backup first — see docs/selfhosting.md.
#
# Checks out the tag (detached HEAD) just long enough for the build
# to stamp `main.version` from `git describe`, then switches back to
# main so the working tree is in a familiar state for the next
# session. The running container keeps the version it was built with.
#
# With compose.prod.yaml enabled (the app image is on ghcr.io) it pulls
# the image CI published for the tag instead of building here.
#
# A pulled tag is then written to .env as VERSION=X.Y.Z, so a later bare
# `docker compose up -d` (after editing .env, say) keeps running it
# rather than whatever :latest this host pulled long ago.
upgrade:
	git fetch --tags
	@LATEST=$$($(LATEST_TAG)) && \
	if [ -z "$$LATEST" ]; then \
		echo "No release tags found — nothing to upgrade to."; exit 1; \
	fi && \
	echo "Upgrading to $$LATEST" && \
	git checkout "$$LATEST" && \
	if docker compose config --images 2>/dev/null | grep -q '^ghcr\.io/'; then \
		echo "Pulling $$LATEST from ghcr.io; nothing is built on this host." && \
		VERSION="$$LATEST" docker compose pull app && \
		VERSION="$$LATEST" docker compose up -d --no-build && \
		touch .env && \
		if grep -q '^VERSION=' .env; then \
			sed -i "s/^VERSION=.*/VERSION=$$LATEST/" .env; \
		else \
			printf '\n# Release running here; set by make upgrade.\nVERSION=%s\n' "$$LATEST" >> .env; \
		fi && \
		echo "Pinned VERSION=$$LATEST in .env"; \
	else \
		VERSION="$$LATEST" docker compose up -d --build; \
	fi
	git checkout main
