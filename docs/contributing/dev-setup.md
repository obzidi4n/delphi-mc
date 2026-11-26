# Dev setup

**Status:** current at M0.

## Requirements

- Go 1.27+ (module `delphi.dev/delphi`; the path is a vanity placeholder — change `go.mod` and
  imports in one pass if you publish elsewhere).
- `make`.
- Postgres 16+ only for readiness and migrations; the page and `/healthz` need nothing.

## Build and run

```sh
make build
./bin/delphi panel --config panel.example.toml
```

Open `http://127.0.0.1:8080/`. With no database the page works and `/readyz` returns 503 —
that is the intended degraded state, not a failure.

```sh
./bin/delphi agent --panel http://127.0.0.1:8080 --once   # one readiness probe
```

## Check

```sh
make check   # vet + test + build
```

## Database

```sh
delphi init --config ./panel.toml --database-url 'postgres://delphi:delphi@127.0.0.1:5432/delphi?sslmode=disable'
```

Migrations are embedded and forward-only; running `init` again is a no-op. There is no
`down` migration and no separate migration tool to install.

## Where things live

[`conventions.md`](conventions.md) has the layout and code rules,
[`testing.md`](testing.md) the test layers. Docs change in the same PR as the behavior they
describe.
