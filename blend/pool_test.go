package blend

import (
	"math"
	"math/big"
	"testing"

	"github.com/stellar/go/xdr"
)

func TestNormalizeRate_DetectsScale(t *testing.T) {
	cases := []struct {
		name string
		raw  int64
		want float64 // 1e7-scaled
	}{
		{"7-decimal rate 1.0", 1_0000000, 1_0000000},
		{"7-decimal rate 1.5", 1_5000000, 1_5000000},
		{"9-decimal rate 1.0 (Blend v1)", 1_000_000_000, 1_0000000},
		{"9-decimal rate 1.2 (Blend v1)", 1_200_000_000, 1_2000000},
		{"12-decimal rate 1.0 (Blend v2)", 1_000_000_000_000, 1_0000000},
		{"12-decimal rate 1.1 (Blend v2)", 1_100_000_000_000, 1_1000000},
	}
	for _, c := range cases {
		got := normalizeRate(big.NewInt(c.raw))
		if math.Abs(got-c.want) > 1 {
			t.Errorf("%s: normalizeRate(%d)=%f want %f", c.name, c.raw, got, c.want)
		}
	}
}

// A reserve that fails to parse must carry honest neutral values — no
// fabricated oracle price (the old 0.30) and no invented collateral factor.
func TestParseReserve_NoFabricatedDefaults(t *testing.T) {
	res := parseReserve(xdr.ScVal{Type: xdr.ScValTypeScvVoid}, "CASSET")
	if res.OraclePrice != 0 {
		t.Errorf("OraclePrice must default to 0 (unknown), got %f", res.OraclePrice)
	}
	if res.CollateralFactor != 0 {
		t.Errorf("CollateralFactor must default to 0 (no fabricated borrowing power), got %f", res.CollateralFactor)
	}
	if res.LiabilityFactor != 1.0 {
		t.Errorf("LiabilityFactor must default to 1.0 (face value), got %f", res.LiabilityFactor)
	}
	if res.BRate != scalar || res.DRate != scalar {
		t.Errorf("rates must default to 1.0 (1e7-scaled), got %f/%f", res.BRate, res.DRate)
	}
}

func TestHasPrices(t *testing.T) {
	unpriced := &PoolState{Reserves: map[string]*Reserve{"A": {}, "B": {}}}
	if unpriced.HasPrices() {
		t.Error("pool with no prices should report false")
	}
	priced := &PoolState{Reserves: map[string]*Reserve{"A": {OraclePrice: 0.5}, "B": {}}}
	if !priced.HasPrices() {
		t.Error("pool with one price should report true")
	}
	var nilPool *PoolState
	if nilPool.HasPrices() {
		t.Error("nil pool should report false")
	}
}

func TestEffectivePrice(t *testing.T) {
	unpriced := &PoolState{Reserves: map[string]*Reserve{"A": {}, "B": {}}}
	if p := effectivePrice(unpriced, unpriced.Reserves["A"]); p != 1.0 {
		t.Errorf("fully unpriced pool should value at parity, got %f", p)
	}
	mixed := &PoolState{Reserves: map[string]*Reserve{"A": {OraclePrice: 2.5}, "B": {}}}
	if p := effectivePrice(mixed, mixed.Reserves["A"]); p != 2.5 {
		t.Errorf("priced reserve should use its oracle price, got %f", p)
	}
	if p := effectivePrice(mixed, mixed.Reserves["B"]); p != 0 {
		t.Errorf("unpriced reserve in a priced pool cannot be valued, got %f", p)
	}
}
