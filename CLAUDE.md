# CLAUDE.md — Nectar Keeper SDK

Public Go module **github.com/Nectar-Network/keeper-sdk** — a framework for
building Soroban liquidation/automation keepers. Extracted from the Nectar
monorepo's `keeper/` during Tranche 2 (Phase 4). Third-party operators import
this to run their own keepers.

## Layout
- `keeper.go` / `config.go` — public API: `Keeper`, `NewKeeper`, `AddAdapter`, `Run`/`RunContext`, `EnsureRegistered`, `Config`, `LoadConfig`/`LoadConfigFromEnv`, `Config.Validate`
- `adapters/` — `ProtocolAdapter` interface + `Task` / `Result` / `VaultClient`
- `adapters/blend/` — reference Blend liquidation adapter
- `dex/` — Soroswap (+ Phoenix fallback) collateral → USDC conversion
- `soroban/` — thin Soroban JSON-RPC client + ScVal builders
- `vault/`, `registry/` — NectarVault / KeeperRegistry clients
- `examples/` — `basic`, `multi-pool`, `custom`
- `docs/` — operator + adapter documentation

## Conventions
- Go 1.24; `gofmt`; `go vet` clean; no external deps beyond `github.com/stellar/go`.
- Adapters are libraries: **no logging**, return errors/values; the `Keeper` logs (`log/slog`).
- Reads via `SimulateRead`; state-changing calls via `rpc.Invoke`. Retries (`InvokeWithRetry`) only fire on pre-broadcast failures; `soroban.ErrTxStatusUnknown` (sent but unconfirmed) is **never retried** — a re-broadcast could double-execute. Recovery happens next cycle.
- Amounts are i128 in 7-decimal stroops (1 unit = 1e7).
- Every exported symbol has a GoDoc comment (this is a public SDK).

## Build / test
```sh
go build ./... && go vet ./... && go test ./...
```

## Publish
Tag a semver and push; the Go module proxy indexes on first `go get`:
```sh
git tag v0.1.0 && git push origin main --tags
```
