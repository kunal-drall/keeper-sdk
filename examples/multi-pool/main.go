// Command multi-pool runs a keeper that monitors several Blend pools at once and
// converts seized collateral to USDC via Soroswap (Phoenix fallback).
//
// Set BLEND_POOLS to a comma-separated list of pool contract IDs (plus the
// standard env from LoadConfig), then:
//
//	go run ./examples/multi-pool
package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	sdk "github.com/Nectar-Network/keeper-sdk"
	"github.com/Nectar-Network/keeper-sdk/adapters/blend"
	"github.com/Nectar-Network/keeper-sdk/dex"
)

func main() {
	cfg := sdk.LoadConfig()
	k, err := sdk.NewKeeper(cfg)
	if err != nil {
		log.Fatal(err)
	}

	// One DEX client shared across every pool adapter.
	dexc := dex.NewSwapClient(k.RPC(), dex.Config{
		HorizonURL:     cfg.HorizonURL,
		Passphrase:     cfg.Passphrase,
		UsdcAddr:       cfg.UsdcAddr,
		SoroswapRouter: cfg.SoroswapRouter,
		PhoenixRouter:  cfg.PhoenixRouter,
		SlippageBps:    cfg.SlippageBps,
	})

	pools := strings.Split(os.Getenv("BLEND_POOLS"), ",")
	for _, p := range pools {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		k.AddAdapter(blend.NewAdapter(blend.Config{
			PoolAddr:   p,
			MinProfit:  cfg.MinProfit,
			HorizonURL: cfg.HorizonURL,
			Passphrase: cfg.Passphrase,
			UsdcAddr:   cfg.UsdcAddr,
		}, dexc))
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := k.RunContext(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatal(err)
	}
}
