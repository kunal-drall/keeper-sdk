# Writing a ProtocolAdapter

A keeper drives any Soroban protocol through one interface. Implement it and
register your adapter with `k.AddAdapter(...)`.

```go
type ProtocolAdapter interface {
	Name() string
	GetTasks(rpc *soroban.Client) ([]Task, error)
	Execute(rpc *soroban.Client, kp *keypair.Full, task Task, vault VaultClient) (*Result, error)
	EstimateCapital(task Task) (int64, error)
}
```

The interface and its types are in the `adapters` package, and re-exported at the
SDK root, so you can use either `adapters.ProtocolAdapter` or `keeper.ProtocolAdapter`
(`sdk.ProtocolAdapter` if you alias the import).

- **`Name()`** — stable identifier used in logs (`"blend"`, `"defindex"`, …).
- **`GetTasks(rpc)`** — scan the protocol this cycle and return actionable
  `Task`s. Reads only (`SimulateRead`). Return `nil` (not an error) when there's
  nothing to do or the adapter is unconfigured.
- **`Execute(rpc, kp, task, vault)`** — perform one task. Draw/return capital via
  `vault` only when the task needs it. Return a `Result`; never log.
- **`EstimateCapital(task)`** — USDC the task needs (0 if none).

### Task / Result

```go
type Task struct {
	Protocol  string  // your Name()
	Type      string  // "liquidation", "rebalance", …
	Target    string  // address / vault id
	Priority  int     // 0..10; higher runs first
	EstProfit float64
	Health    float64 // optional (health factor / drift)
	Data      any     // your payload, threaded back to Execute
}

type Result struct {
	Success        bool
	TxHash         string
	Drew, Proceeds int64
	Profit         int64 // realized, max(0, proceeds-drew)
	ResponseTimeMs int64
	Note           string // status when not Success
}
```

Stash whatever `Execute` needs in `Task.Data` (a snapshot, a precomputed plan)
and type-assert it back, tolerating a failed assertion.

### VaultClient

```go
type VaultClient interface {
	Draw(amount int64) error
	ReturnProceeds(amount, responseTimeMs int64) error
}
```

Use it only when your task consumes Nectar capital. **Only return proceeds when
you actually drew** — otherwise the vault books the return as cost-free profit.

## Conventions

1. **No logging, no global state.** Adapters are libraries; the `Keeper` logs from
   the `Result`.
2. **Reads via `SimulateRead`, writes via `rpc.Invoke`.** Never re-broadcast a
   transaction whose fate is unknown: `soroban.ErrTxStatusUnknown` (returned when
   a sent transaction can't be confirmed in time) means it may still land —
   `InvokeWithRetry` already refuses to retry it; treat it the same way in your
   own code and let the next cycle re-evaluate instead.
3. **Panics are contained, not free.** The keeper isolates adapter panics so one
   buggy adapter can't kill the process, but a panicking cycle does no work —
   return errors instead.
4. **Measured, never synthesized.** Report real on-chain outcomes (balance
   deltas, returned amounts).
5. **Encode with the `soroban.Scv*` builders.** `ScvAddress`, `ScvI128`,
   `ScvU64`, `ScvSymbol`, `ScvVec`, `ScvVoid`. Soroban structs → `ScMap` keyed by
   `Symbol`; enums-with-fields → `Vec[Symbol(variant), field0, …]`; `Option::None`
   → `ScvVoid()`.
6. **Decode** with `xdr.SafeUnmarshalBase64(sim.Results[0].XDR, &val)` then walk
   the `ScVal` (`val.Vec`/`val.Map` are double pointers: `**val.Vec`).
7. **Fail fast on auth.** If the action is role-gated, check it in `Execute` and
   return a `Result{Note: …}` instead of submitting a doomed tx.

## Skeleton

```go
type Adapter struct{ cfg Config }

func (a *Adapter) Name() string { return "myproto" }

func (a *Adapter) GetTasks(rpc *soroban.Client) ([]adapters.Task, error) {
	// SimulateRead -> decode -> detect work -> return []adapters.Task
}

func (a *Adapter) Execute(rpc *soroban.Client, kp *keypair.Full, t adapters.Task, vc adapters.VaultClient) (*adapters.Result, error) {
	// optionally vc.Draw(...); encode args; rpc.Invoke(...); build Result
}

func (a *Adapter) EstimateCapital(adapters.Task) (int64, error) { return 0, nil }

var _ adapters.ProtocolAdapter = (*Adapter)(nil) // compile-time check
```

## Reference

- [`adapters/blend`](../adapters/blend/adapter.go) — draws capital, fills a Blend
  auction, swaps collateral to USDC, returns real proceeds.
- [`examples/custom`](../examples/custom/main.go) — a minimal custom adapter.

## Testing

Unit-test the pure logic (planning, math, decoders) and no-RPC guards
(`GetTasks` with an empty contract, validation errors); full on-chain execution
is verified on testnet. Add `var _ adapters.ProtocolAdapter = (*Adapter)(nil)` so
the interface is enforced at compile time.
