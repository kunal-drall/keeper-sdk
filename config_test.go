package keeper

import (
	"strings"
	"testing"

	"github.com/stellar/go/keypair"
)

func testSecret(t *testing.T) string {
	t.Helper()
	kp, err := keypair.Random()
	if err != nil {
		t.Fatalf("keypair: %v", err)
	}
	return kp.Seed()
}

func validConfig(t *testing.T) Config {
	return Config{
		RpcURL:        "http://invalid.local",
		HorizonURL:    "http://invalid.local",
		Passphrase:    "Test SDF Network ; September 2015",
		KeeperSecret:  testSecret(t),
		VaultContract: "CAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAD2KM",
		PollInterval:  10,
		MinProfit:     1.02,
	}
}

func TestLoadConfigFromEnv_MissingRequired(t *testing.T) {
	for _, k := range []string{"KEEPER_SECRET", "REGISTRY_CONTRACT", "VAULT_CONTRACT"} {
		t.Setenv(k, "")
	}
	_, err := LoadConfigFromEnv()
	if err == nil {
		t.Fatal("expected error when required env is missing")
	}
	for _, k := range []string{"KEEPER_SECRET", "REGISTRY_CONTRACT", "VAULT_CONTRACT"} {
		if !strings.Contains(err.Error(), k) {
			t.Errorf("error should name %s, got: %v", k, err)
		}
	}
}

func TestLoadConfigFromEnv_InvalidNumbers(t *testing.T) {
	t.Setenv("KEEPER_SECRET", "S...")
	t.Setenv("REGISTRY_CONTRACT", "C...")
	t.Setenv("VAULT_CONTRACT", "C...")
	t.Setenv("POLL_INTERVAL", "999999")
	if _, err := LoadConfigFromEnv(); err == nil || !strings.Contains(err.Error(), "POLL_INTERVAL") {
		t.Fatalf("expected POLL_INTERVAL range error, got %v", err)
	}

	t.Setenv("POLL_INTERVAL", "10")
	t.Setenv("MIN_PROFIT", "-1")
	if _, err := LoadConfigFromEnv(); err == nil || !strings.Contains(err.Error(), "MIN_PROFIT") {
		t.Fatalf("expected MIN_PROFIT error, got %v", err)
	}
}

func TestLoadConfigFromEnv_Defaults(t *testing.T) {
	t.Setenv("KEEPER_SECRET", "S...")
	t.Setenv("REGISTRY_CONTRACT", "C...")
	t.Setenv("VAULT_CONTRACT", "C...")
	for _, k := range []string{"POLL_INTERVAL", "MIN_PROFIT", "SLIPPAGE_BPS", "SOROBAN_RPC", "KEEPER_NAME"} {
		t.Setenv(k, "")
	}
	c, err := LoadConfigFromEnv()
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if c.PollInterval != 10 || c.MinProfit != 1.02 || c.SlippageBps != 100 {
		t.Errorf("unexpected defaults: %+v", c)
	}
	if c.RpcURL == "" || c.KeeperName != "nectar-keeper" {
		t.Errorf("unexpected endpoint defaults: %+v", c)
	}
}

func TestValidate_CatchesProblems(t *testing.T) {
	c := validConfig(t)
	c.VaultContract = ""
	c.PollInterval = 0
	if err := c.Validate(); err == nil {
		t.Fatal("expected validation error")
	} else {
		if !strings.Contains(err.Error(), "VaultContract") || !strings.Contains(err.Error(), "PollInterval") {
			t.Errorf("error should name every failing field, got: %v", err)
		}
	}
}

// PollInterval 0 in a hand-built Config used to panic time.NewTicker at Run
// time; NewKeeper must default it instead.
func TestNewKeeper_DefaultsZeroTuning(t *testing.T) {
	c := validConfig(t)
	c.PollInterval = 0
	c.MinProfit = 0
	k, err := NewKeeper(c)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	got := k.Config()
	if got.PollInterval != 10 {
		t.Errorf("PollInterval should default to 10, got %d", got.PollInterval)
	}
	if got.MinProfit != 1.02 {
		t.Errorf("MinProfit should default to 1.02, got %v", got.MinProfit)
	}
}

func TestNewKeeper_RejectsEmptyEndpoints(t *testing.T) {
	c := validConfig(t)
	c.HorizonURL = ""
	if _, err := NewKeeper(c); err == nil {
		t.Fatal("expected error for missing HorizonURL")
	}
}

func TestNewKeeper_RejectsBadSecret(t *testing.T) {
	c := validConfig(t)
	c.KeeperSecret = "not-a-secret"
	if _, err := NewKeeper(c); err == nil {
		t.Fatal("expected error for malformed secret")
	}
}
