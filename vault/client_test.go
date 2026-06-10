package vault

import (
	"math"
	"strings"
	"testing"

	"github.com/stellar/go/keypair"
	"github.com/stellar/go/xdr"

	"github.com/Nectar-Network/keeper-sdk/soroban"
)

func mustKP(t *testing.T) *keypair.Full {
	t.Helper()
	kp, err := keypair.Random()
	if err != nil {
		t.Fatalf("keypair: %v", err)
	}
	return kp
}

func TestDraw_RejectsNonPositiveAmount(t *testing.T) {
	rpc := soroban.NewClient("http://invalid.local")
	kp := mustKP(t)

	for _, amt := range []int64{0, -1, -100} {
		err := Draw(rpc, "http://invalid.local", kp, "Test SDF Network", "CAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAD2KM", amt)
		if err == nil {
			t.Errorf("draw(%d): expected error", amt)
			continue
		}
		if !strings.Contains(err.Error(), "amount must be > 0") {
			t.Errorf("draw(%d): unexpected error %v", amt, err)
		}
	}
}

func TestReturnProceeds_RejectsNonPositiveAmount(t *testing.T) {
	rpc := soroban.NewClient("http://invalid.local")
	kp := mustKP(t)

	for _, amt := range []int64{0, -1, -100} {
		err := ReturnProceeds(rpc, "http://invalid.local", kp, "Test SDF Network", "CAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAD2KM", amt, 100)
		if err == nil {
			t.Errorf("return_proceeds(%d): expected error", amt)
			continue
		}
		if !strings.Contains(err.Error(), "amount must be > 0") {
			t.Errorf("return_proceeds(%d): unexpected error %v", amt, err)
		}
	}
}

func TestVaultState_StructDefaults(t *testing.T) {
	// Sanity: zero-value VaultState has all fields at zero.
	var s VaultState
	if s.TotalUSDC != 0 || s.TotalShares != 0 || s.TotalProfit != 0 || s.ActiveLiq != 0 {
		t.Errorf("zero VaultState should be all zeros, got %+v", s)
	}
}

func TestBalanceResult_StructDefaults(t *testing.T) {
	var b BalanceResult
	if b.Shares != 0 || b.USDCValue != 0 {
		t.Errorf("zero BalanceResult should be all zeros, got %+v", b)
	}
}

// i128 values outside int64 must saturate, not truncate to the low 64 bits —
// truncation can silently flip the sign of a huge (or adversarial) value.
func TestScI128_SaturatesInsteadOfTruncating(t *testing.T) {
	i128 := func(hi int64, lo uint64) xdr.ScVal {
		return xdr.ScVal{Type: xdr.ScValTypeScvI128, I128: &xdr.Int128Parts{Hi: xdr.Int64(hi), Lo: xdr.Uint64(lo)}}
	}
	cases := []struct {
		name string
		val  xdr.ScVal
		want int64
	}{
		{"small positive", i128(0, 42), 42},
		{"max int64", i128(0, math.MaxInt64), math.MaxInt64},
		{"negative one", i128(-1, math.MaxUint64), -1},
		{"hi=1 overflows -> saturate max", i128(1, 0), math.MaxInt64},
		{"lo above int63 with hi=0 -> saturate max", i128(0, math.MaxInt64+1), math.MaxInt64},
		{"very negative -> saturate min", i128(-2, 0), math.MinInt64},
		{"not an i128", soroban.ScvU64(7), 0},
	}
	for _, c := range cases {
		if got := scI128(c.val); got != c.want {
			t.Errorf("%s: got %d want %d", c.name, got, c.want)
		}
	}
}
