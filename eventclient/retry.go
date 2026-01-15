package eventclient

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"time"
)

// RetryConfig configures retry behavior with exponential backoff.
type RetryConfig struct {
	InitialDelay time.Duration // Initial delay before first retry
	MaxDelay     time.Duration // Maximum delay between retries
	MaxRetries   int           // Maximum number of retries (0 = infinite)
	Multiplier   float64       // Backoff multiplier (e.g., 2.0 for doubling)
}

// DefaultRetryConfig returns a default retry configuration with:
// - 1 second initial delay
// - 5 minute maximum delay
// - 2.0 multiplier (exponential backoff)
// - Infinite retries (0)
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		InitialDelay: 1 * time.Second,
		MaxDelay:     5 * time.Minute,
		MaxRetries:   0, // Infinite retries
		Multiplier:   2.0,
	}
}

// RetryWithBackoff executes the given function with exponential backoff retry logic.
// It retries the function until it succeeds, the context is cancelled, or MaxRetries is reached.
//
// The backoff delay starts at InitialDelay and multiplies by Multiplier after each attempt,
// up to MaxDelay. Jitter (±25% randomization) is added to prevent thundering herd.
//
// Parameters:
//   - ctx: Context for cancellation
//   - cfg: Retry configuration
//   - operation: Human-readable operation name for logging
//   - fn: Function to retry
//
// Returns:
//   - nil if function succeeds
//   - context error if context is cancelled
//   - last error if MaxRetries is exceeded
func RetryWithBackoff(
	ctx context.Context,
	cfg RetryConfig,
	operation string,
	fn func() error,
) error {
	// Try once immediately
	err := fn()
	if err == nil {
		return nil
	}

	// If it failed, start retry loop
	delay := cfg.InitialDelay
	attempt := 1

	for {
		// Check if we've exceeded max retries (0 means infinite)
		if cfg.MaxRetries > 0 && attempt > cfg.MaxRetries {
			return fmt.Errorf("max retries exceeded: %w", err)
		}

		// Calculate next backoff delay with jitter
		nextDelay := calculateBackoffWithJitter(delay, cfg.MaxDelay)

		// Log retry attempt
		log.Printf("INFO: %s failed, retrying in %.1fs (attempt %d): %v",
			operation, nextDelay.Seconds(), attempt, err)

		// Wait for backoff period or context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(nextDelay):
			// Continue to retry
		}

		// Attempt the operation
		err = fn()
		if err == nil {
			// Success! Log recovery if we had to retry
			if attempt > 0 {
				log.Printf("INFO: %s successful after %d attempt(s)", operation, attempt+1)
			}
			return nil
		}

		// Calculate next delay using exponential backoff
		delay = min(cfg.MaxDelay, time.Duration(float64(delay)*cfg.Multiplier))
		attempt++
	}
}

// calculateBackoffWithJitter applies jitter (±25% randomization) to the delay
// to prevent thundering herd when multiple instances retry simultaneously.
// The result is capped at maxDelay.
func calculateBackoffWithJitter(delay, maxDelay time.Duration) time.Duration {
	// Apply ±25% jitter
	jitterPercent := 0.25
	jitterRange := float64(delay) * jitterPercent
	jitter := (rand.Float64() * 2 * jitterRange) - jitterRange

	result := max(0, min(maxDelay, time.Duration(float64(delay)+jitter)))

	return result
}
