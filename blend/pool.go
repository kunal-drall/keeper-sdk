package blend

import (
	"fmt"
	"math"
	"math/big"

	"github.com/stellar/go/xdr"

	"github.com/Nectar-Network/keeper-sdk/soroban"
)

// PoolState is a point-in-time snapshot of a Blend pool: its reserves and the
// SEP-40 oracle it prices against.
type PoolState struct {
	Reserves   map[string]*Reserve // asset address -> reserve
	OracleAddr string              // pool's oracle contract, "" when unknown
}

// Reserve is one pool reserve's risk configuration and pricing.
//
// BRate/DRate are normalized to 7-decimal scale (1e7 = rate 1.0) regardless of
// the pool's native rate decimals — Blend v1 stores rates with 9 decimals and
// v2 with 12; the magnitude identifies the scale since rates start at 1.0 and
// grow slowly with interest. OraclePrice is USD per whole token; 0 means no
// price is available (valuation helpers then degrade conservatively — see
// Profitability and CalcHealthFactor).
type Reserve struct {
	Asset            string
	Index            uint32
	CollateralFactor float64
	LiabilityFactor  float64
	BRate            float64 // 7-decimal scaled (1e7 = 1.0)
	DRate            float64 // 7-decimal scaled (1e7 = 1.0)
	OraclePrice      float64 // USD per whole token, 0 = unknown
}

const scalar = 1e7

// HasPrices reports whether at least one reserve carries a real oracle price.
// Pools without any price (e.g. mocks with no oracle) are valued at parity by
// the valuation helpers.
func (ps *PoolState) HasPrices() bool {
	if ps == nil {
		return false
	}
	for _, r := range ps.Reserves {
		if r != nil && r.OraclePrice > 0 {
			return true
		}
	}
	return false
}

// effectivePrice is the price used in valuations: the reserve's oracle price
// when one exists; parity (1.0) when the whole pool is unpriced, preserving
// pure amount-ratio behavior on oracle-less test pools; and 0 (cannot value)
// when other reserves are priced but this one is not.
func effectivePrice(ps *PoolState, r *Reserve) float64 {
	if r == nil {
		return 0
	}
	if r.OraclePrice > 0 {
		return r.OraclePrice
	}
	if !ps.HasPrices() {
		return 1.0
	}
	return 0
}

// LoadPool queries a Blend pool contract for reserve configuration and then
// loads oracle prices for every reserve from the pool's SEP-40 oracle (via
// get_config). Price loading is best-effort: a pool without a reachable oracle
// still returns usable reserves, with OraclePrice left 0.
func LoadPool(rpc *soroban.Client, passphrase, poolAddr string) (*PoolState, error) {
	ps := &PoolState{Reserves: make(map[string]*Reserve)}

	// get reserve list
	sim, err := rpc.SimulateRead(passphrase, poolAddr, "get_reserve_list")
	if err != nil {
		return nil, fmt.Errorf("reserve list: %w", err)
	}
	if sim.Error != "" {
		return nil, fmt.Errorf("reserve list sim: %s", sim.Error)
	}
	if len(sim.Results) == 0 {
		return ps, nil
	}

	var listVal xdr.ScVal
	if err := xdr.SafeUnmarshalBase64(sim.Results[0].XDR, &listVal); err != nil {
		return nil, err
	}
	assets := parseVec(listVal)

	for _, assetAddr := range assets {
		addrVal, err := soroban.ScvAddress(assetAddr)
		if err != nil {
			continue
		}
		resSim, err := rpc.SimulateRead(passphrase, poolAddr, "get_reserve", addrVal)
		if err != nil || resSim.Error != "" {
			continue
		}
		if len(resSim.Results) == 0 {
			continue
		}
		var resVal xdr.ScVal
		if err := xdr.SafeUnmarshalBase64(resSim.Results[0].XDR, &resVal); err != nil {
			continue
		}
		res := parseReserve(resVal, assetAddr)
		ps.Reserves[assetAddr] = res
	}

	ps.OracleAddr = loadOracleAddr(rpc, passphrase, poolAddr)
	LoadOraclePrices(rpc, passphrase, ps)
	return ps, nil
}

// LoadOraclePrices fills Reserve.OraclePrice for every reserve from the pool's
// SEP-40 oracle (lastprice per asset, scaled by the oracle's decimals).
// Best-effort: assets the oracle cannot price keep OraclePrice 0, and a
// missing/unreachable oracle leaves the snapshot unchanged. Exported so
// operators with a custom price source can re-price a snapshot themselves.
func LoadOraclePrices(rpc *soroban.Client, passphrase string, ps *PoolState) {
	if ps == nil || ps.OracleAddr == "" {
		return
	}
	dec := oracleDecimals(rpc, passphrase, ps.OracleAddr)
	if dec <= 0 || dec > 38 {
		return // cannot scale prices safely without known decimals
	}
	scale := math.Pow10(dec)
	for assetAddr, r := range ps.Reserves {
		if r == nil {
			continue
		}
		p := oracleLastPrice(rpc, passphrase, ps.OracleAddr, assetAddr)
		if p == nil || p.Sign() <= 0 {
			continue
		}
		f, _ := new(big.Float).SetInt(p).Float64()
		r.OraclePrice = f / scale
	}
}

// loadOracleAddr reads the pool's oracle address from get_config (best-effort).
func loadOracleAddr(rpc *soroban.Client, passphrase, poolAddr string) string {
	sim, err := rpc.SimulateRead(passphrase, poolAddr, "get_config")
	if err != nil || sim.Error != "" || len(sim.Results) == 0 {
		return ""
	}
	var val xdr.ScVal
	if err := xdr.SafeUnmarshalBase64(sim.Results[0].XDR, &val); err != nil {
		return ""
	}
	if val.Type != xdr.ScValTypeScvMap || val.Map == nil || *val.Map == nil {
		return ""
	}
	for _, e := range **val.Map {
		if scSymbol(e.Key) != "oracle" {
			continue
		}
		if e.Val.Type == xdr.ScValTypeScvAddress && e.Val.Address != nil {
			addr, err := soroban.ParseAddress(*e.Val.Address)
			if err == nil {
				return addr
			}
		}
	}
	return ""
}

// oracleDecimals reads the SEP-40 oracle's price decimals (0 on failure).
func oracleDecimals(rpc *soroban.Client, passphrase, oracleAddr string) int {
	sim, err := rpc.SimulateRead(passphrase, oracleAddr, "decimals")
	if err != nil || sim.Error != "" || len(sim.Results) == 0 {
		return 0
	}
	var val xdr.ScVal
	if err := xdr.SafeUnmarshalBase64(sim.Results[0].XDR, &val); err != nil {
		return 0
	}
	return int(scU32(val))
}

// oracleLastPrice fetches lastprice(Asset::Stellar(token)) from a SEP-40
// oracle and returns the raw scaled price, or nil when unavailable.
func oracleLastPrice(rpc *soroban.Client, passphrase, oracleAddr, tokenAddr string) *big.Int {
	addrVal, err := soroban.ScvAddress(tokenAddr)
	if err != nil {
		return nil
	}
	// SEP-40 Asset enum: Stellar(Address) encodes as Vec[Symbol("Stellar"), Address].
	assetVal := soroban.ScvVec(soroban.ScvSymbol("Stellar"), addrVal)
	sim, err := rpc.SimulateRead(passphrase, oracleAddr, "lastprice", assetVal)
	if err != nil || sim.Error != "" || len(sim.Results) == 0 {
		return nil
	}
	var val xdr.ScVal
	if err := xdr.SafeUnmarshalBase64(sim.Results[0].XDR, &val); err != nil {
		return nil
	}
	// Option::None → void; Some(PriceData{price, timestamp}) → map.
	if val.Type != xdr.ScValTypeScvMap || val.Map == nil || *val.Map == nil {
		return nil
	}
	for _, e := range **val.Map {
		if scSymbol(e.Key) == "price" {
			return scI128(e.Val)
		}
	}
	return nil
}

// parseReserve decodes a get_reserve result. Fields that fail to parse keep
// honest neutral values — collateral factor 0 (no fabricated borrowing power),
// liability factor 1.0 (face value), rate 1.0 — rather than invented risk
// parameters that could mis-trigger liquidations.
func parseReserve(val xdr.ScVal, asset string) *Reserve {
	res := &Reserve{
		Asset:           asset,
		LiabilityFactor: 1.0,
		BRate:           scalar,
		DRate:           scalar,
	}
	if val.Type != xdr.ScValTypeScvMap || val.Map == nil || *val.Map == nil {
		return res
	}
	for _, e := range **val.Map {
		k := scSymbol(e.Key)
		switch k {
		case "index":
			res.Index = scU32(e.Val)
		case "c_factor":
			if e.Val.Type == xdr.ScValTypeScvU32 && e.Val.U32 != nil {
				res.CollateralFactor = float64(*e.Val.U32) / scalar
			}
		case "l_factor":
			if e.Val.Type == xdr.ScValTypeScvU32 && e.Val.U32 != nil && *e.Val.U32 > 0 {
				res.LiabilityFactor = float64(*e.Val.U32) / scalar
			}
		case "b_rate":
			if v := scI128(e.Val); v != nil {
				res.BRate = normalizeRate(v)
			}
		case "d_rate":
			if v := scI128(e.Val); v != nil {
				res.DRate = normalizeRate(v)
			}
		}
	}
	return res
}

// normalizeRate converts a raw on-chain b_rate/d_rate to the SDK's 7-decimal
// scale. Blend v1 stores rates with 9 decimals and v2 with 12; since a rate
// starts at 1.0 and grows slowly with accrued interest, its magnitude
// identifies the scale unambiguously.
func normalizeRate(raw *big.Int) float64 {
	f, _ := new(big.Float).SetInt(raw).Float64()
	if f <= 0 {
		return scalar
	}
	switch {
	case f >= 1e10: // 12-decimal (Blend v2)
		return f / 1e5
	case f >= 1e8: // 9-decimal (Blend v1)
		return f / 1e2
	default: // already 7-decimal
		return f
	}
}

func parseVec(val xdr.ScVal) []string {
	if val.Type != xdr.ScValTypeScvVec || val.Vec == nil || *val.Vec == nil {
		return nil
	}
	out := make([]string, 0)
	for _, item := range **val.Vec {
		if item.Type == xdr.ScValTypeScvAddress && item.Address != nil {
			addr, err := soroban.ParseAddress(*item.Address)
			if err == nil {
				out = append(out, addr)
			}
		}
	}
	return out
}

func scSymbol(val xdr.ScVal) string {
	if val.Type == xdr.ScValTypeScvSymbol && val.Sym != nil {
		return string(*val.Sym)
	}
	return ""
}

func scU32(val xdr.ScVal) uint32 {
	if val.Type == xdr.ScValTypeScvU32 && val.U32 != nil {
		return uint32(*val.U32)
	}
	return 0
}

func scI128(val xdr.ScVal) *big.Int {
	if val.Type != xdr.ScValTypeScvI128 || val.I128 == nil {
		return nil
	}
	hi := new(big.Int).SetInt64(int64(val.I128.Hi))
	lo := new(big.Int).SetUint64(uint64(val.I128.Lo))
	result := new(big.Int).Lsh(hi, 64)
	result.Add(result, lo)
	return result
}
