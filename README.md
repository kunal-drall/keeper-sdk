# Nectar Keeper SDK

A small Go framework for building **Soroban liquidation / automation keepers** on
Stellar. Implement the `ProtocolAdapter` interface (or use the bundled Blend
adapter), register it, and call `Run` — the keeper polls each adapter for
actionable tasks every cycle and executes them with shared vault capital. It is
the same engine that powers [Nectar Network](https://nectarnetwork.fun)'s pooled
liquidation protocol, extracted for third-party operators.

## Install

```sh
go get github.com/Nectar-Network/keeper-sdk
```

Requires Go 1.24+. The only external dependency is the Stellar Go SDK.

## Quickstart

Set the environment, then run ~10 lines:

```sh
export KEEPER_SECRET=S...            # keeper signing key
export REGISTRY_CONTRACT=C...        # KeeperRegistry
export VAULT_CONTRACT=C...           # NectarVault
export BLEND_POOL=C...               # Blend pool to monitor
export USDC_CONTRACT=C...            # USDC token (collateral is swapped into this)
export SOROSWAP_ROUTER=C...          # optional: enables collateral -> USDC swaps
```

```go
package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"syscall"

	sdk "github.com/Nectar-Network/keeper-sdk"
	"github.com/Nectar-Network/keeper-sdk/adapters/blend"
)

func main() {
	cfg := sdk.LoadConfig()
	k, err := sdk.NewKeeper(cfg)
	if err != nil {
		log.Fatal(err)
	}
	k.AddAdapter(blend.NewAdapter(blend.Config{
		PoolAddr:   cfg.BlendPool,
		MinProfit:  cfg.MinProfit,
		HorizonURL: cfg.HorizonURL,
		Passphrase: cfg.Passphrase,
		UsdcAddr:   cfg.UsdcAddr,
	}, nil))

	// Graceful shutdown: the in-flight cycle finishes before the keeper stops.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := k.RunContext(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatal(err)
	}
}
```

```sh
go run .
```

## What's inside

| Package | What it does |
|---|---|
| (root) | `Keeper`, `NewKeeper`, `AddAdapter`, `Run`/`RunContext`, `EnsureRegistered`, `Config`/`LoadConfig`/`LoadConfigFromEnv` |
| `adapters` | `ProtocolAdapter` interface + `Task`/`Result`/`VaultClient` |
| `adapters/blend` | Reference Blend liquidation adapter |
| `dex` | Soroswap (+ Phoenix fallback) collateral → USDC conversion |
| `soroban` | Thin Soroban JSON-RPC client + ScVal builders |
| `vault`, `registry` | NectarVault / KeeperRegistry clients |

## Build your own adapter

Implement four methods (`Name`, `GetTasks`, `Execute`, `EstimateCapital`) — see
[`examples/custom`](examples/custom/main.go) and the
[adapter guide](docs/adapters.md). Reads use `SimulateRead`; state-changing calls
use `rpc.Invoke` and are never auto-retried.

## Examples

- [`examples/basic`](examples/basic/main.go) — minimal Blend keeper
- [`examples/multi-pool`](examples/multi-pool/main.go) — several pools + DEX conversion
- [`examples/custom`](examples/custom/main.go) — a custom `ProtocolAdapter`

## Documentation

- [Setup](docs/setup.md) — run a keeper in under 10 minutes
- [Configuration](docs/configuration.md) — every environment variable
- [Strategies](docs/strategies.md) — conservative / balanced / aggressive
- [Troubleshooting](docs/troubleshooting.md)
- [Writing an adapter](docs/adapters.md)

Full guides will also live on the Nectar docs site (Tranche 3).

## License

See [LICENSE](LICENSE).
