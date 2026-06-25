# duckmigrate

Database migrations for [DuckDB](https://duckdb.org), as a Go library and a Cobra CLI.

`golang-migrate` doesn't support DuckDB. `duckmigrate` is a clean-room, DuckDB-native
migration tool: plain `.up.sql` / `.down.sql` files, each migration applied atomically inside
a transaction (DuckDB has transactional DDL), with checksum validation and `embed.FS` support.

## Install

Requires **CGO** (the [`duckdb-go`](https://github.com/duckdb/duckdb-go) driver bundles DuckDB).
Prebuilt static libraries ship for macOS and Linux (amd64/arm64) and Windows (amd64); other
platforms build DuckDB from source. You need a C compiler and `CGO_ENABLED=1`.

```sh
go install github.com/mrfoh/duckmigrate/cmd/duckmigrate@latest
```

## Migration files

```
migrations/
  20240101120000_create_users.up.sql
  20240101120000_create_users.down.sql
```

`{version}_{title}.up.sql` is required; the matching `.down.sql` is optional. A migration with
no down file is **irreversible** — any rollback that would cross it fails up front and changes
nothing.

A file may contain multiple statements. To opt a migration out of transaction wrapping (e.g. for
`INSTALL`/`LOAD`, some `PRAGMA`s, or `ATTACH`), add a directive comment anywhere in it:

```sql
-- duckmigrate:no-transaction
INSTALL httpfs; LOAD httpfs;
```

## CLI

```sh
duckmigrate -d app.duckdb -p migrations create add_users   # write up/down files (--seq for 000001 style)
duckmigrate -d app.duckdb -p migrations up                 # apply all pending (up N for the next N)
duckmigrate -d app.duckdb -p migrations status             # table of applied / pending / checksum state
duckmigrate -d app.duckdb -p migrations version
duckmigrate -d app.duckdb -p migrations down 1             # roll back the last migration (down -f for all)
duckmigrate -d app.duckdb -p migrations goto 20240101120000
duckmigrate -d app.duckdb -p migrations force 20240101120000   # recovery: set version, clear dirty
duckmigrate -d app.duckdb -p migrations drop -f
```

`-d` accepts a file path, `:memory:`, or a `duckdb://` URL. Flags also read from
`DUCKMIGRATE_DATABASE`, `DUCKMIGRATE_PATH`, and `DUCKMIGRATE_TABLE`.

| Flag | Default | Purpose |
| --- | --- | --- |
| `--table` | `schema_migrations` | tracking table name |
| `--checksum` | `strict` | `strict` \| `warn` \| `off` |
| `--no-transaction` | `false` | never wrap migrations in a transaction |
| `--lock-timeout` | `0` | wait this long for a lock held by another process |
| `-v, --verbose` | `false` | log each applied migration |

## Library

```go
import "github.com/mrfoh/duckmigrate"

//go:embed migrations/*.sql
var migrations embed.FS

m, err := duckmigrate.New(
	duckmigrate.WithDSN("app.duckdb"),
	duckmigrate.WithFS(migrations, "migrations"),
)
if err != nil { log.Fatal(err) }
defer m.Close()

if err := m.Up(context.Background()); err != nil && !errors.Is(err, duckmigrate.ErrNoChange) {
	log.Fatal(err)
}
```

Bring your own `*sql.DB` with `duckmigrate.WithDB(db)` instead of `WithDSN`. Methods:
`Up`, `Down`, `Steps(±n)`, `Goto(version)`, `Force(version)`, `Drop`, `Version`, `Status`.
See [`examples/embed`](examples/embed).

## How it tracks state

Two tables (named from `--table`):

- `schema_migrations` — a single row holding the current `version` and a `dirty` flag (the
  authoritative current state).
- `schema_migrations_history` — an append-only audit log (`version, title, direction, checksum,
  applied_at, execution_ms`). `force` writes a `force` row here, never a synthetic `up` row, so
  `status` and checksum validation stay honest.

On the transactional path the migration SQL and its bookkeeping commit together, so a failure
rolls everything back and `dirty` is never set. Only `--no-transaction` migrations can leave the
database dirty; recover with `force <version>` after fixing it by hand.

`status` recomputes the SHA-256 of each applied migration's up SQL and compares it to the stored
checksum, flagging files that changed after they were applied (`mismatch`). `strict` mode (the
default) turns a mismatch into an error on the next `up`.

## Scope & limitations

- **Concurrency**: DuckDB allows a single read-write process per database file and enforces it
  with a file lock. Concurrent migration runs are not supported; a second run waits up to
  `--lock-timeout` and then errors. For multi-writer setups use DuckLake or the Quack protocol.
- **`drop`** resets the **`main` schema of the primary database only** — every non-internal table,
  view, and sequence (including the tracking tables). Attached databases and other schemas are
  left untouched.
