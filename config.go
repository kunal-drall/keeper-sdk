package keeper

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config holds everything a keeper needs. Populate it directly, or via
// LoadConfig / LoadConfigFromEnv to read from environment variables.
// NewKeeper applies defaults for unset tuning fields (PollInterval, MinProfit)
// and validates the rest, so a hand-built Config fails fast instead of
// panicking at runtime.
type Config struct {
	RpcURL           string
	HorizonURL       string
	Passphrase       string
	KeeperSecret     string
	KeeperName       string
	RegistryContract string
	VaultContract    string
	BlendPool        string
	UsdcAddr         string
	SoroswapRouter   string
	PhoenixRouter    string
	PollInterval     int     // seconds between cycles (default 10)
	MinProfit        float64 // minimum lot/bid ratio to act (default 1.02)
	SlippageBps      int     // max swap slippage in basis points (0–10000)
}

// LoadConfig reads Config from environment variables with testnet defaults.
// KEEPER_SECRET, REGISTRY_CONTRACT, and VAULT_CONTRACT are required; any
// invalid or missing value exits the process with a clear message. Library
// callers that want an error instead should use LoadConfigFromEnv.
func LoadConfig() Config {
	c, err := LoadConfigFromEnv()
	if err != nil {
		fmt.Fprintf(os.Stderr, "keeper-sdk: %v\n", err)
		os.Exit(1)
	}
	return c
}

// LoadConfigFromEnv reads Config from environment variables with testnet
// defaults, returning an error (instead of exiting) when a required variable
// is missing or a value is out of range.
func LoadConfigFromEnv() (Config, error) {
	c := Config{
		RpcURL:         envOr("SOROBAN_RPC", "https://soroban-testnet.stellar.org:443"),
		HorizonURL:     envOr("HORIZON_URL", "https://horizon-testnet.stellar.org"),
		Passphrase:     envOr("NETWORK_PASSPHRASE", "Test SDF Network ; September 2015"),
		KeeperName:     envOr("KEEPER_NAME", "nectar-keeper"),
		BlendPool:      envOr("BLEND_POOL", ""),
		UsdcAddr:       envOr("USDC_CONTRACT", ""),
		SoroswapRouter: envOr("SOROSWAP_ROUTER", ""),
		PhoenixRouter:  envOr("PHOENIX_ROUTER", ""),
	}

	var errs []error
	required := func(key string) string {
		v := strings.TrimSpace(os.Getenv(key))
		if v == "" {
			errs = append(errs, fmt.Errorf("missing required env %s", key))
		}
		return v
	}
	c.KeeperSecret = required("KEEPER_SECRET")
	c.RegistryContract = required("REGISTRY_CONTRACT")
	c.VaultContract = required("VAULT_CONTRACT")

	var err error
	if c.PollInterval, err = intEnv("POLL_INTERVAL", 10, 3, 300); err != nil {
		errs = append(errs, err)
	}
	if c.MinProfit, err = floatEnv("MIN_PROFIT", 1.02); err != nil {
		errs = append(errs, err)
	}
	if c.SlippageBps, err = intEnv("SLIPPAGE_BPS", 100, 0, 10000); err != nil {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return Config{}, errors.Join(errs...)
	}
	return c, nil
}

// Validate reports configuration problems that would make the keeper
// malfunction at runtime. NewKeeper calls it after applying defaults; callers
// constructing Config by hand can also call it directly.
func (c Config) Validate() error {
	var errs []error
	for _, f := range []struct{ name, v string }{
		{"RpcURL", c.RpcURL},
		{"HorizonURL", c.HorizonURL},
		{"Passphrase", c.Passphrase},
		{"KeeperSecret", c.KeeperSecret},
		{"VaultContract", c.VaultContract},
	} {
		if strings.TrimSpace(f.v) == "" {
			errs = append(errs, fmt.Errorf("%s must be set", f.name))
		}
	}
	if c.PollInterval < 1 {
		errs = append(errs, fmt.Errorf("PollInterval must be >= 1 second, got %d", c.PollInterval))
	}
	if c.MinProfit <= 0 {
		errs = append(errs, fmt.Errorf("MinProfit must be > 0, got %v", c.MinProfit))
	}
	if c.SlippageBps < 0 || c.SlippageBps > 10000 {
		errs = append(errs, fmt.Errorf("SlippageBps must be in [0,10000], got %d", c.SlippageBps))
	}
	return errors.Join(errs...)
}

// withDefaults returns a copy with zero-valued tuning fields set to production
// defaults. Identity fields (secrets, contracts, endpoints) are never invented.
func (c Config) withDefaults() Config {
	if c.PollInterval == 0 {
		c.PollInterval = 10
	}
	if c.MinProfit == 0 {
		c.MinProfit = 1.02
	}
	if c.KeeperName == "" {
		c.KeeperName = "nectar-keeper"
	}
	return c
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func intEnv(key string, def, min, max int) (int, error) {
	v := envOr(key, strconv.Itoa(def))
	n, err := strconv.Atoi(v)
	if err != nil || n < min || n > max {
		return 0, fmt.Errorf("%s=%q invalid (want integer in [%d,%d])", key, v, min, max)
	}
	return n, nil
}

func floatEnv(key string, def float64) (float64, error) {
	v := envOr(key, strconv.FormatFloat(def, 'f', -1, 64))
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || f <= 0 {
		return 0, fmt.Errorf("%s=%q invalid (want float > 0)", key, v)
	}
	return f, nil
}
