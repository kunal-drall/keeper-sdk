package keeper

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stellar/go/keypair"

	"github.com/Nectar-Network/keeper-sdk/adapters"
	"github.com/Nectar-Network/keeper-sdk/soroban"
)

// stubAdapter counts GetTasks calls and optionally panics, exercising the run
// loop without any network access (no RegistryContract / UsdcAddr configured).
type stubAdapter struct {
	calls atomic.Int64
	panic bool
}

func (s *stubAdapter) Name() string { return "stub" }
func (s *stubAdapter) GetTasks(*soroban.Client) ([]adapters.Task, error) {
	s.calls.Add(1)
	if s.panic {
		panic("adapter bug")
	}
	return nil, nil
}
func (s *stubAdapter) Execute(*soroban.Client, *keypair.Full, adapters.Task, adapters.VaultClient) (*adapters.Result, error) {
	return nil, nil
}
func (s *stubAdapter) EstimateCapital(adapters.Task) (int64, error) { return 0, nil }

func newTestKeeper(t *testing.T) *Keeper {
	t.Helper()
	k, err := NewKeeper(validConfig(t))
	if err != nil {
		t.Fatalf("NewKeeper: %v", err)
	}
	return k
}

func TestRunContext_NoAdapters(t *testing.T) {
	k := newTestKeeper(t)
	if err := k.RunContext(context.Background()); err == nil {
		t.Fatal("expected error with no adapters")
	}
}

func TestRunContext_RunsFirstCycleImmediatelyAndStops(t *testing.T) {
	k := newTestKeeper(t)
	ad := &stubAdapter{}
	k.AddAdapter(ad)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- k.RunContext(ctx) }()

	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected ctx error, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("RunContext did not stop on context cancellation")
	}
	// First cycle runs immediately (PollInterval is 10s — far longer than the
	// context timeout — so any recorded call proves the immediate first cycle).
	if ad.calls.Load() < 1 {
		t.Fatal("expected an immediate first cycle before the first tick")
	}
}

func TestRunContext_SurvivesPanickingAdapter(t *testing.T) {
	k := newTestKeeper(t)
	bad := &stubAdapter{panic: true}
	good := &stubAdapter{}
	k.AddAdapter(bad)
	k.AddAdapter(good)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- k.RunContext(ctx) }()

	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected clean ctx stop despite panicking adapter, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("RunContext hung")
	}
	if good.calls.Load() < 1 {
		t.Fatal("a panic in one adapter must not prevent other adapters from running")
	}
}
