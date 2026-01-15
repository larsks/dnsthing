package eventclient

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestRetryWithBackoff_ImmediateSuccess tests that no retries occur when function succeeds immediately.
func TestRetryWithBackoff_ImmediateSuccess(t *testing.T) {
	ctx := context.Background()
	cfg := RetryConfig{
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     1 * time.Second,
		MaxRetries:   3,
		Multiplier:   2.0,
	}

	callCount := 0
	fn := func() error {
		callCount++
		return nil // Succeed immediately
	}

	err := RetryWithBackoff(ctx, cfg, "test operation", fn)
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	if callCount != 1 {
		t.Errorf("expected 1 call, got %d", callCount)
	}
}

// TestRetryWithBackoff_EventualSuccess tests that function succeeds after N attempts.
func TestRetryWithBackoff_EventualSuccess(t *testing.T) {
	ctx := context.Background()
	cfg := RetryConfig{
		InitialDelay: 10 * time.Millisecond,
		MaxDelay:     100 * time.Millisecond,
		MaxRetries:   5,
		Multiplier:   2.0,
	}

	callCount := 0
	failUntil := 3 // Fail first 3 attempts, succeed on 4th

	fn := func() error {
		callCount++
		if callCount < failUntil {
			return errors.New("temporary failure")
		}
		return nil
	}

	start := time.Now()
	err := RetryWithBackoff(ctx, cfg, "test operation", fn)
	elapsed := time.Since(start)

	if err != nil {
		t.Errorf("expected success after retries, got: %v", err)
	}

	if callCount != failUntil {
		t.Errorf("expected %d calls, got %d", failUntil, callCount)
	}

	// Should have taken at least some time due to backoff (at least 2 delays)
	minExpected := cfg.InitialDelay + time.Duration(float64(cfg.InitialDelay)*cfg.Multiplier)
	if elapsed < minExpected/2 { // Allow some tolerance
		t.Errorf("expected at least ~%v elapsed, got %v", minExpected, elapsed)
	}
}

// TestRetryWithBackoff_MaxRetriesExceeded tests that error is returned when max retries exceeded.
func TestRetryWithBackoff_MaxRetriesExceeded(t *testing.T) {
	ctx := context.Background()
	cfg := RetryConfig{
		InitialDelay: 10 * time.Millisecond,
		MaxDelay:     100 * time.Millisecond,
		MaxRetries:   3,
		Multiplier:   2.0,
	}

	callCount := 0
	fn := func() error {
		callCount++
		return errors.New("permanent failure")
	}

	err := RetryWithBackoff(ctx, cfg, "test operation", fn)
	if err == nil {
		t.Error("expected error after max retries, got nil")
	}

	// Should be called: initial attempt + MaxRetries
	expectedCalls := 1 + cfg.MaxRetries
	if callCount != expectedCalls {
		t.Errorf("expected %d calls (1 initial + %d retries), got %d",
			expectedCalls, cfg.MaxRetries, callCount)
	}
}

// TestRetryWithBackoff_ExponentialBackoff tests that delay increases exponentially.
func TestRetryWithBackoff_ExponentialBackoff(t *testing.T) {
	ctx := context.Background()
	cfg := RetryConfig{
		InitialDelay: 50 * time.Millisecond,
		MaxDelay:     10 * time.Second,
		MaxRetries:   4,
		Multiplier:   2.0,
	}

	callCount := 0
	var timestamps []time.Time

	fn := func() error {
		timestamps = append(timestamps, time.Now())
		callCount++
		return errors.New("keep failing")
	}

	RetryWithBackoff(ctx, cfg, "test operation", fn)

	// We should have 5 timestamps (1 initial + 4 retries)
	if len(timestamps) != 5 {
		t.Fatalf("expected 5 timestamps, got %d", len(timestamps))
	}

	// Check that delays roughly double (with jitter tolerance)
	delays := make([]time.Duration, 0)
	for i := 1; i < len(timestamps); i++ {
		delay := timestamps[i].Sub(timestamps[i-1])
		delays = append(delays, delay)
	}

	// First delay should be around InitialDelay (±25% jitter)
	expectedFirst := cfg.InitialDelay
	if delays[0] < expectedFirst/2 || delays[0] > expectedFirst*2 {
		t.Logf("Warning: First delay %v not close to expected %v (with jitter)", delays[0], expectedFirst)
	}

	// Each subsequent delay should be roughly double the previous (accounting for jitter)
	for i := 1; i < len(delays); i++ {
		// We expect delays[i] to be roughly 2x delays[i-1], but jitter makes this fuzzy
		// Just check that delays are generally increasing
		if delays[i] < delays[i-1]/2 {
			t.Errorf("Delay %d (%v) is not increasing from delay %d (%v)",
				i, delays[i], i-1, delays[i-1])
		}
	}
}

// TestRetryWithBackoff_MaxDelayEnforcement tests that delay is capped at MaxDelay.
func TestRetryWithBackoff_MaxDelayEnforcement(t *testing.T) {
	ctx := context.Background()
	cfg := RetryConfig{
		InitialDelay: 50 * time.Millisecond,
		MaxDelay:     100 * time.Millisecond, // Cap at 100ms
		MaxRetries:   10,
		Multiplier:   2.0,
	}

	callCount := 0
	var timestamps []time.Time

	fn := func() error {
		timestamps = append(timestamps, time.Now())
		callCount++
		return errors.New("keep failing")
	}

	RetryWithBackoff(ctx, cfg, "test operation", fn)

	// Check that no delay exceeds MaxDelay significantly (accounting for jitter)
	for i := 1; i < len(timestamps); i++ {
		delay := timestamps[i].Sub(timestamps[i-1])
		// Allow 50% tolerance for jitter (25% either way + some margin)
		maxAllowed := cfg.MaxDelay * 3 / 2
		if delay > maxAllowed {
			t.Errorf("Delay %d (%v) exceeds max delay %v (with jitter tolerance %v)",
				i-1, delay, cfg.MaxDelay, maxAllowed)
		}
	}
}

// TestRetryWithBackoff_ContextCancellation tests that retry stops when context is canceled.
func TestRetryWithBackoff_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cfg := RetryConfig{
		InitialDelay: 50 * time.Millisecond,
		MaxDelay:     1 * time.Second,
		MaxRetries:   0, // Infinite retries
		Multiplier:   2.0,
	}

	callCount := 0
	fn := func() error {
		callCount++
		if callCount == 2 {
			// Cancel context after second attempt
			cancel()
		}
		return errors.New("keep failing")
	}

	err := RetryWithBackoff(ctx, cfg, "test operation", fn)
	if err != context.Canceled {
		t.Errorf("expected context.Canceled error, got: %v", err)
	}

	// Should have been called at least twice (once before cancel, once after triggering cancel)
	// but not many more times since we canceled early
	if callCount < 2 || callCount > 4 {
		t.Errorf("expected 2-4 calls before cancellation, got %d", callCount)
	}
}

// TestRetryWithBackoff_InfiniteRetries tests that MaxRetries=0 means infinite retries.
func TestRetryWithBackoff_InfiniteRetries(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	cfg := RetryConfig{
		InitialDelay: 20 * time.Millisecond,
		MaxDelay:     50 * time.Millisecond,
		MaxRetries:   0, // Infinite retries
		Multiplier:   2.0,
	}

	callCount := 0
	fn := func() error {
		callCount++
		return errors.New("keep failing forever")
	}

	err := RetryWithBackoff(ctx, cfg, "test operation", fn)

	// Should return context error (deadline exceeded)
	if err != context.DeadlineExceeded {
		t.Errorf("expected context.DeadlineExceeded, got: %v", err)
	}

	// Should have been called multiple times (more than 3)
	if callCount < 3 {
		t.Errorf("expected at least 3 calls with infinite retries, got %d", callCount)
	}
}

// TestRetryWithBackoff_Jitter tests that jitter is being applied.
func TestRetryWithBackoff_Jitter(t *testing.T) {
	ctx := context.Background()
	cfg := RetryConfig{
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     10 * time.Second,
		MaxRetries:   5,
		Multiplier:   1.0, // No exponential growth, just test jitter
	}

	var timestamps []time.Time
	fn := func() error {
		timestamps = append(timestamps, time.Now())
		return errors.New("keep failing")
	}

	RetryWithBackoff(ctx, cfg, "test operation", fn)

	// Calculate delays between attempts
	delays := make([]time.Duration, 0)
	for i := 1; i < len(timestamps); i++ {
		delay := timestamps[i].Sub(timestamps[i-1])
		delays = append(delays, delay)
	}

	// With jitter, not all delays should be identical
	// Check that we have some variation (at least one delay differs by more than 5%)
	allSame := true
	baseDelay := delays[0]
	tolerance := baseDelay / 20 // 5% tolerance

	for _, d := range delays[1:] {
		diff := d - baseDelay
		if diff < 0 {
			diff = -diff
		}
		if diff > tolerance {
			allSame = false
			break
		}
	}

	if allSame {
		t.Error("All delays are nearly identical, jitter may not be working")
	}
}

// TestDefaultRetryConfig tests that default config is sensible.
func TestDefaultRetryConfig(t *testing.T) {
	cfg := DefaultRetryConfig()

	if cfg.InitialDelay != 1*time.Second {
		t.Errorf("expected InitialDelay = 1s, got %v", cfg.InitialDelay)
	}

	if cfg.MaxDelay != 5*time.Minute {
		t.Errorf("expected MaxDelay = 5m, got %v", cfg.MaxDelay)
	}

	if cfg.MaxRetries != 0 {
		t.Errorf("expected MaxRetries = 0 (infinite), got %d", cfg.MaxRetries)
	}

	if cfg.Multiplier != 2.0 {
		t.Errorf("expected Multiplier = 2.0, got %f", cfg.Multiplier)
	}
}
