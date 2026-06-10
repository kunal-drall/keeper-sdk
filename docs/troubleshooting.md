# Troubleshooting

## `missing required env ...` on startup
`KEEPER_SECRET`, `REGISTRY_CONTRACT`, or `VAULT_CONTRACT` is unset. See
[configuration.md](configuration.md).

## `keeper: no adapters registered`
`Run()` was called before `AddAdapter()`. Register at least one adapter (e.g. the
Blend adapter) first.

## Keeper runs but never acts
- `BLEND_POOL` is empty → the Blend adapter is idle by design. Set it.
- No underwater positions exist in the pool right now (normal — the keeper is
  waiting). Watch for `underwater` / task logs.
- `MIN_PROFIT` is too high for current auction conditions. Lower it (see
  [strategies.md](strategies.md)).

## `execute failed: vault draw: ... Unauthorized (#5)`
The keeper isn't registered in the `KeeperRegistry`, or its registration lapsed.
Register (and stake) the keeper's address first.

## `task skipped` with `zero returnable proceeds — outstanding draw at slash risk`
The auction filled and capital was drawn, but every collateral swap failed (no
router configured, slippage too tight, or the DEX was unreachable), so there was
nothing to return. The keeper holds the collateral; the on-chain draw stays open
and is exposed to the registry's draw-timeout slash. Fix the swap path
(`SOROSWAP_ROUTER`/`USDC_CONTRACT`/`SLIPPAGE_BPS`) and recover the collateral
before the slash window elapses.

## `dex: quote below slippage floor`
The DEX quote was worse than the oracle-implied fair value by more than
`SLIPPAGE_BPS`. This is protection working as intended — the keeper refuses to
sell collateral too cheaply. Raise `SLIPPAGE_BPS` only if you understand the
pool's depth.

## Swaps / draws intermittently fail with timeout
Public testnet RPC can be flaky. Failures **before** a transaction is accepted
(simulation errors, `tx_bad_seq`, `try_again_later`, connection resets) are
retried with backoff. Failures **after** acceptance are not: a
`transaction status unknown` error means the tx was broadcast but unconfirmed —
it may still land, so the keeper refuses to re-broadcast (a second submission
could double-draw or double-fill) and reconciles on the next cycle via the
stale-draw recovery instead. Persistent failures usually mean a bad RPC
endpoint — set `SOROBAN_RPC`/`HORIZON_URL` to a reliable provider.

## `transaction status unknown (sent but unconfirmed)`
Not a config problem — the RPC node couldn't confirm the transaction within the
await window. Check the tx hash on Stellar Expert: if it landed, the next
cycle's stale-draw recovery squares the vault automatically; if it expired, the
keeper simply re-evaluates the opportunity fresh.

## `keeper is not registered in the KeeperRegistry` warning at startup
The keeper checks its registration when it starts (read-only). Register and
stake from your operator wallet, or call `k.EnsureRegistered()` once at boot —
it is idempotent but **stakes USDC** on first registration, which is why `Run`
never does it automatically.

## `can't find crate for core` (only when building contracts, not this SDK)
Unrelated to the SDK — that's a Rust/wasm toolchain issue in the contracts repo.

## Still stuck?
Open an issue with the structured log line (`slog` output) for the failing cycle.
