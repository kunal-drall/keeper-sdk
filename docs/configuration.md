# Configuration reference

`sdk.LoadConfig()` reads these environment variables. **Required** vars abort the
process with a clear message if missing; the rest have testnet-friendly defaults.
Prefer `sdk.LoadConfigFromEnv()` when embedding the SDK in a larger program — it
returns an error instead of exiting.

| Variable | Required | Default | Meaning |
|---|---|---|---|
| `KEEPER_SECRET` | ✅ | — | Keeper's Stellar secret key (`S...`). Signs all txs. |
| `REGISTRY_CONTRACT` | ✅ | — | KeeperRegistry contract ID. |
| `VAULT_CONTRACT` | ✅ | — | NectarVault contract ID (capital source). |
| `BLEND_POOL` | — | _(empty)_ | Blend pool to monitor. Empty → the Blend adapter is idle. |
| `USDC_CONTRACT` | — | _(empty)_ | USDC token; collateral is swapped into this. |
| `SOROSWAP_ROUTER` | — | _(empty)_ | Soroswap router; empty disables Soroswap swaps. |
| `PHOENIX_ROUTER` | — | _(empty)_ | Phoenix XYK pool (fallback DEX); empty disables. |
| `KEEPER_NAME` | — | `nectar-keeper` | Display name in logs/registry. |
| `SOROBAN_RPC` | — | testnet RPC | Soroban RPC endpoint. |
| `HORIZON_URL` | — | testnet Horizon | Horizon endpoint (account sequence). |
| `NETWORK_PASSPHRASE` | — | testnet passphrase | Network passphrase. |
| `POLL_INTERVAL` | — | `10` | Seconds between cycles (range 3–300). |
| `MIN_PROFIT` | — | `1.02` | Minimum lot/bid ratio to act (must be > 0). |
| `SLIPPAGE_BPS` | — | `100` | Max swap slippage in basis points (0–10000; 100 = 1%). |

## Notes
- **No DEX configured** → the keeper holds non-USDC collateral instead of
  synthesizing proceeds. To close the loop on testnet set `SOROSWAP_ROUTER` and
  `USDC_CONTRACT`.
- All on-chain amounts are i128 in 7-decimal stroops (1 USDC = 10,000,000).
- `MIN_PROFIT` is the lot-value / bid-cost ratio; 1.02 means "only fill when the
  seized collateral is worth ≥ 2% more than the capital spent."
- Constructing `Config` directly (without `LoadConfig`) is fully supported — set
  the struct fields yourself for tests or non-env deployments. `NewKeeper`
  validates the config (`Config.Validate`) and applies production defaults to
  unset tuning fields (`PollInterval` → 10s, `MinProfit` → 1.02), so a partial
  struct fails fast with a clear error instead of misbehaving mid-run.
- **Oracle prices**: when the monitored pool exposes its SEP-40 oracle via
  `get_config`, the SDK prices every reserve from `lastprice` and uses real USD
  values for profitability, health factors, and the swap slippage floor. Pools
  without a reachable oracle are valued at parity (pure amount ratios) and the
  slippage floor falls back to the DEX quote's on-chain min-out.
