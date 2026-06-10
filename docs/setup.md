# Setup — run a keeper in under 10 minutes

## 1. Prerequisites
- Go 1.24+
- A funded Stellar testnet account (the keeper's signing key). Fund it via
  [Friendbot](https://laboratory.stellar.org/#account-creator?network=test).
- The contract IDs you'll target: `KeeperRegistry`, `NectarVault`, USDC token,
  and a Blend pool. (For Nectar testnet, see the project README.)

## 2. Create a project
```sh
mkdir my-keeper && cd my-keeper
go mod init example.com/my-keeper
go get github.com/Nectar-Network/keeper-sdk
```

## 3. main.go
Copy [`examples/basic`](../examples/basic/main.go): load config, register the
Blend adapter, run.

## 4. Configure
Export the required environment (see [configuration.md](configuration.md)):
```sh
export KEEPER_SECRET=S...
export REGISTRY_CONTRACT=C...
export VAULT_CONTRACT=C...
export BLEND_POOL=C...
export USDC_CONTRACT=C...
# Optional — enables collateral -> USDC conversion:
export SOROSWAP_ROUTER=CCJUD55AG6W5HAI5LRVNKAE5WDP5XGZBUDS5WNTIVDU7O264UZZE7BRD
```

## 5. Register on-chain (once)
A keeper must be registered (and staked) in the `KeeperRegistry` before the vault
will let it draw capital. Register from your operator wallet (the registry pulls
`min_stake` USDC), use the Nectar frontend's keeper page, or call
`k.EnsureRegistered()` once at boot — it is idempotent, but note it stakes USDC
on first registration, so the SDK never does it implicitly. An unregistered
keeper logs a clear warning at startup instead of failing cryptically on its
first draw.

## 6. Run
```sh
go run .
```
You should see `keeper starting` and, each cycle, either task activity or a quiet
loop when there's nothing actionable. Logs are structured (`log/slog`). Ctrl-C /
SIGTERM stops the keeper cleanly after the in-flight cycle (see the examples'
`RunContext` + `signal.NotifyContext` pattern).

## 7. Deploy (optional)
The keeper is a single static binary. `go build -o keeper .` and run it under any
supervisor (systemd, Docker, Railway, Fly). It is stateless — it reads all state
from chain each cycle and restarts safely.

Next: [configuration.md](configuration.md) · [strategies.md](strategies.md) ·
[troubleshooting.md](troubleshooting.md)
