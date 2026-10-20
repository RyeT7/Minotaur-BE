# Minotaur-BE

Go + Gin backend for the Minotaur inventory app, backed by PostgreSQL (tested on 17).

## Run

```sh
DATABASE_URL=postgres://user:pass@localhost:5432/minotaur go run ./cmd/server
```

Migrations in `internal/database/migrations` run automatically at startup. Each one is applied inside a single transaction, and an advisory lock keeps two instances from migrating at once. Add new migrations as numbered files (`0002_…sql`) and never edit one that's already applied.

| Env var                | Default                 |
| ---------------------- | ----------------------- |
| `DATABASE_URL`         | required                |
| `ADDR`                 | `:8080`                 |
| `CORS_ALLOWED_ORIGINS` | `http://localhost:5173` |
| `GIN_MODE`             | `debug` (use `release`) |

## Test

```sh
TEST_DATABASE_URL=postgres://user:pass@localhost:5432/minotaur_test go test ./...
```

Each test creates its own `test_<random>` schema and drops it afterwards. Without `TEST_DATABASE_URL`, the database tests are skipped locally and fail in CI.

## API

| Method | Path                                | Purpose                                              |
| ------ | ----------------------------------- | ---------------------------------------------------- |
| GET    | `/healthz`                          | Liveness check                                       |
| GET    | `/readyz`                           | Readiness check (pings the database)                 |
| GET    | `/api/v1/items?warehouse=`          | List stock rows (one per SKU per warehouse)          |
| POST   | `/api/v1/items`                     | Create a stock row                                   |
| GET    | `/api/v1/items/:sku/:warehouse`     | Get one stock row                                    |
| POST   | `/api/v1/movements`                 | Record a stock movement (updates stock, logs activity) |
| GET    | `/api/v1/movements?sku=&warehouse=&limit=` | Movement history, newest first                |
| GET    | `/api/v1/movements/daily?days=14`   | Inbound/outbound totals per UTC day                  |
| GET    | `/api/v1/activity?limit=50`         | Activity feed, newest first                          |

Movement body:

```json
{ "kind": "transferred", "sku": "WID-1", "warehouse": "WH-A", "toWarehouse": "WH-B", "quantity": 4, "note": "rebalance" }
```

- `received`, `shipped`, `transferred`: `quantity` must be positive.
- `adjusted`: `quantity` is a signed delta.
- `transferred` requires `toWarehouse`. If that warehouse has no row for the SKU yet, one is created.
- Stock can never go negative. A movement that would make it negative gets `409`.
- Daily totals: `received` and positive adjustments count as inbound; `shipped` and negative adjustments count as outbound. Transfers are excluded because they net to zero.

## Supply-chain policy

- Keep dependencies to a minimum. The only direct runtime dependencies are Gin and `pgx`. Everything else uses the standard library, including the CORS and security-header middleware and the migration runner.
- Pin exact versions in `go.mod` and commit `go.sum`. Checksums are verified against `sum.golang.org`, and CI runs `go mod verify` plus `go mod tidy -diff`.
- Wait for new versions to age. Don't adopt a release until it's at least 7 days old; Dependabot enforces this with `cooldown`. Check a version's date with `go list -m -json <module>@<version>`.
- Security fixes skip the wait. When `govulncheck` flags a vulnerability, upgrade to the fixed version right away, even if it's newer than 7 days.
- CI uses `GOFLAGS=-mod=readonly`, so builds can't silently change dependencies, and `GOTOOLCHAIN=local`, so it never downloads a toolchain.
- CI's Postgres service image is pinned by digest. Dependabot doesn't update it, so bump it by hand: pick a tag at least 7 days old and copy its digest from Docker Hub.
- GitHub Actions are pinned to full commit SHAs. Workflows get read-only `contents` permission, and checkout runs with `persist-credentials: false`.
- CI runs `govulncheck` at a pinned version (`go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...`), with the download checked against `sum.golang.org`. This keeps its dependencies out of this module.
- Before adding a dependency, review it (maintainers, recent activity, transitive deps from `go mod graph`) and justify it with `go mod why`.
