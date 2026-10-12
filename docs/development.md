# Development

## Layout

```
cmd/server/    entry point, flags, backup and restore commands
internal/      server packages (HTTP, auth, database, game engine, …)
web/           React apps and shared packages, embedded into the binary
docs/          operator documentation
```

`make help` lists the common tasks: running the stack, tests, the
frontend build and releases.

## Running locally

```sh
make up        # Postgres and the app in Docker, on http://localhost:8080
make web-dev   # Vite dev server for the React apps, proxying /api
```

## Tests

Run the suite against a real PostgreSQL. The database-bound tests, about
a quarter of the suite, are skipped without one, so a green run without
a database proves little. Point `ZV_TEST_DSN` at a role with `CREATEDB`:

```sh
docker run -d --name zv-test-pg -e POSTGRES_PASSWORD=postgres -p 5432:5432 postgres:16-alpine
export ZV_TEST_DSN='postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable'
go test ./...
```

CI sets `CI=true`, which makes an unreachable database a failure instead
of a skip.

## Git hooks

Enable the repository's hooks once after cloning:

```sh
git config core.hooksPath .githooks
```

The pre-commit hook runs `gitleaks` on staged changes and checks
`gofmt`.

## Contributing

Bug reports, patches and translations are welcome as GitHub issues and
pull requests. Report security issues privately as described in
[SECURITY.md](../SECURITY.md).
