package blend

import (
	"math"
	"math/big"
	"testing"

	"github.com/Nectar-Network/keeper-sdk/adapters"
	core "github.com/Nectar-Network/keeper-sdk/blend"
	"github.com/Nectar-Network/keeper-sdk/soroban"
)

func TestAdapter_Name(t *testing.T) {
	a := NewAdapter(Config{}, nil)
	if a.Name() != "blend" {
		t.Fatalf("expected blend, got %s", a.Name())
	}
}

// GetTasks with an empty pool returns no work without touching the network.
func TestAdapter_GetTasks_NoPool(t *testing.T) {
	a := NewAdapter(Config{}, nil)
	tasks, err := a.GetTasks(soroban.NewClient("http://invalid.local"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("expected no tasks, got %d", len(tasks))
	}
}

func TestAdapter_EstimateCapital(t *testing.T) {
	a := NewAdapter(Config{}, nil)
	got, err := a.EstimateCapital(adapters.Task{})
	if err != nil || got != 0 {
		t.Fatalf("expected (0,nil), got (%d,%v)", got, err)
	}
}

func TestPriorityFromHF(t *testing.T) {
	cases := []struct {
		hf   float64
		want int
	}{
		{0.4, 10}, {0.7, 7}, {0.9, 4}, {0.99, 1},
	}
	for _, c := range cases {
		if got := priorityFromHF(c.hf); got != c.want {
			t.Errorf("priorityFromHF(%.2f)=%d want %d", c.hf, got, c.want)
		}
	}
}

func TestOracleValueUSDC(t *testing.T) {
	pool := &core.PoolState{Reserves: map[string]*core.Reserve{
		"CTKN": {OraclePrice: 0.5},
	}}
	if got := oracleValueUSDC(pool, "CTKN", 100); got != 50 {
		t.Fatalf("expected 50 (100 * 0.5), got %d", got)
	}
	if got := oracleValueUSDC(pool, "UNKNOWN", 100); got != 0 {
		t.Fatalf("expected 0 for unknown asset, got %d", got)
	}
	if got := oracleValueUSDC(nil, "CTKN", 100); got != 0 {
		t.Fatalf("expected 0 for nil pool, got %d", got)
	}
}

// With oracle prices, the draw is sized by the bid's USD value at the current
// decay (+5% buffer) — not by summing raw token amounts across assets, which
// is only meaningful when the bid is USDC.
func TestDrawAmount_UsesOracleValueWhenPriced(t *testing.T) {
	pool := &core.PoolState{Reserves: map[string]*core.Reserve{
		"CXLM": {OraclePrice: 0.5},
	}}
	auction := &core.Auction{
		StartBlock: 1000,
		Bid:        map[string]*big.Int{"CXLM": big.NewInt(100_0000000)}, // 100 XLM @ $0.50
	}
	// Fair-price block (bidPct=1.0): 50 USD * 1.05 buffer = 52.5 USDC.
	got := drawAmount(auction, pool, 1200)
	want := int64(52_5000000)
	if got != want {
		t.Fatalf("drawAmount=%d want %d", got, want)
	}
}

// Without prices (mock pools), the raw bid sum remains the fallback.
func TestDrawAmount_RawSumFallbackWhenUnpriced(t *testing.T) {
	pool := &core.PoolState{Reserves: map[string]*core.Reserve{"CUSDC": {}}}
	auction := &core.Auction{
		StartBlock: 1000,
		Bid:        map[string]*big.Int{"CUSDC": big.NewInt(75_0000000)},
	}
	if got := drawAmount(auction, pool, 1200); got != 75_0000000 {
		t.Fatalf("drawAmount=%d want raw sum 75_0000000", got)
	}
}

func TestBigToInt64_Saturates(t *testing.T) {
	huge := new(big.Int).Lsh(big.NewInt(1), 100)
	if got := bigToInt64(huge); got != math.MaxInt64 {
		t.Fatalf("expected MaxInt64, got %d", got)
	}
	if got := bigToInt64(new(big.Int).Neg(huge)); got != math.MinInt64 {
		t.Fatalf("expected MinInt64, got %d", got)
	}
	if got := bigToInt64(nil); got != 0 {
		t.Fatalf("nil should be 0, got %d", got)
	}
	if got := bigToInt64(big.NewInt(-5)); got != -5 {
		t.Fatalf("expected -5, got %d", got)
	}
}

// Adapter must satisfy the ProtocolAdapter interface.
var _ adapters.ProtocolAdapter = (*Adapter)(nil)
