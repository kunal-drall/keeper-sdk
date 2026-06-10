package soroban

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestIsRetryable_Transient(t *testing.T) {
	cases := []string{
		"tx_too_late",
		"sequence number conflict",
		"resource_exhaust",
		"timeout waiting for tx",
		"timed out",
		"connection reset by peer",
		"EOF",
		"tx_insufficient_fee",
	}
	for _, msg := range cases {
		err := errors.New(msg)
		if !IsRetryable(err) {
			t.Errorf("%q should be retryable", msg)
		}
	}
}

func TestIsRetryable_Deterministic(t *testing.T) {
	cases := []string{
		"insufficient_balance",
		"contract error: NotRegistered",
		"already filled",
		"AuctionNotFound",
		"unauthorized",
	}
	for _, msg := range cases {
		err := errors.New(msg)
		if IsRetryable(err) {
			t.Errorf("%q should NOT be retryable", msg)
		}
	}
}

func TestIsRetryable_Nil(t *testing.T) {
	if IsRetryable(nil) {
		t.Fatal("nil error should not be retryable")
	}
}

// A transaction with an unknown fate was already broadcast; retrying could
// double-execute (double draw / double fill). This is the SDK's core
// transaction-safety invariant.
func TestIsRetryable_StatusUnknownNeverRetries(t *testing.T) {
	wrapped := fmt.Errorf("vault draw: tx cafebabe: %w after 45s", ErrTxStatusUnknown)
	if IsRetryable(wrapped) {
		t.Fatal("ErrTxStatusUnknown (even wrapped) must not be retryable")
	}
}

// Base64 XDR blobs in error text can contain "eof" by coincidence; that must
// not classify a deterministic failure as transient.
func TestIsRetryable_NoEofFalsePositiveInBase64(t *testing.T) {
	err := errors.New("tx failed: AAAHeoF8cmFuZG9tYmFzZTY0") // contains "eof" lowercased
	if IsRetryable(err) {
		t.Fatal("base64 containing 'eof' must not be treated as a network EOF")
	}
	// Real EOFs still classify correctly.
	for _, msg := range []string{"EOF", `Post "http://x": EOF`, "unexpected EOF"} {
		if !IsRetryable(errors.New(msg)) {
			t.Errorf("%q should be retryable", msg)
		}
	}
}

// Decoded XDR result-code names (what Send/AwaitTx now emit) classify too.
func TestIsRetryable_DecodedResultCodes(t *testing.T) {
	for _, msg := range []string{
		"send tx rejected: TransactionResultCodeTxBadSeq",
		"send tx rejected: TransactionResultCodeTxTooLate",
		"send tx rejected: TransactionResultCodeTxInsufficientFee",
		"send tx: try_again_later (queue full, not accepted)",
	} {
		if !IsRetryable(errors.New(msg)) {
			t.Errorf("%q should be retryable", msg)
		}
	}
}

func TestRetryWith_Success_FirstAttempt(t *testing.T) {
	calls := 0
	cfg := RetryConfig{MaxAttempts: 3, InitialDelay: time.Millisecond, BackoffFactor: 2.0}
	err := RetryWith(cfg, func() error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 call, got %d", calls)
	}
}

func TestRetryWith_Success_AfterTransient(t *testing.T) {
	calls := 0
	cfg := RetryConfig{MaxAttempts: 3, InitialDelay: time.Millisecond, BackoffFactor: 2.0}
	err := RetryWith(cfg, func() error {
		calls++
		if calls < 3 {
			return errors.New("tx_too_late")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if calls != 3 {
		t.Errorf("expected 3 calls, got %d", calls)
	}
}

func TestRetryWith_StopsOnNonRetryable(t *testing.T) {
	calls := 0
	cfg := RetryConfig{MaxAttempts: 5, InitialDelay: time.Millisecond, BackoffFactor: 2.0}
	err := RetryWith(cfg, func() error {
		calls++
		return errors.New("insufficient_balance")
	})
	if err == nil {
		t.Fatal("expected error to propagate")
	}
	if calls != 1 {
		t.Errorf("non-retryable should fail fast; got %d calls", calls)
	}
}

func TestRetryWith_ExhaustsBudget(t *testing.T) {
	calls := 0
	cfg := RetryConfig{MaxAttempts: 3, InitialDelay: time.Millisecond, BackoffFactor: 2.0}
	err := RetryWith(cfg, func() error {
		calls++
		return errors.New("timeout: rpc unavailable")
	})
	if err == nil {
		t.Fatal("expected exhaustion error")
	}
	if calls != cfg.MaxAttempts {
		t.Errorf("expected %d attempts, got %d", cfg.MaxAttempts, calls)
	}
}

func TestRetryWith_BackoffGrows(t *testing.T) {
	cfg := RetryConfig{MaxAttempts: 4, InitialDelay: 5 * time.Millisecond, BackoffFactor: 2.0}
	start := time.Now()
	calls := 0
	_ = RetryWith(cfg, func() error {
		calls++
		return errors.New("timeout")
	})
	elapsed := time.Since(start)
	// Three sleeps between four attempts: 5 + 10 + 20 = 35ms minimum.
	min := 30 * time.Millisecond
	if elapsed < min {
		t.Errorf("expected at least %v elapsed, got %v", min, elapsed)
	}
}

func TestRetryWith_ZeroAttemptsCoercesToOne(t *testing.T) {
	calls := 0
	cfg := RetryConfig{MaxAttempts: 0}
	err := RetryWith(cfg, func() error {
		calls++
		return errors.New("timeout")
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Errorf("expected 1 call (coerced), got %d", calls)
	}
}

func TestDefaultRetry_HasReasonableValues(t *testing.T) {
	c := DefaultRetry()
	if c.MaxAttempts < 2 {
		t.Errorf("default MaxAttempts too low: %d", c.MaxAttempts)
	}
	if c.InitialDelay <= 0 {
		t.Error("default InitialDelay must be positive")
	}
	if c.BackoffFactor <= 1 {
		t.Errorf("default BackoffFactor should be >1, got %f", c.BackoffFactor)
	}
}
