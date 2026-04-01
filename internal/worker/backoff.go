package worker

import (
	"math"
	"time"
)

// BackoffStrategy determines how long to wait before retrying a failed job.
// attempts is the number of attempts already made (0-based before the next attempt).
type BackoffStrategy interface {
	NextDelay(attempts int) time.Duration
}

// FixedBackoff waits a constant duration between every retry.
type FixedBackoff struct {
	Delay time.Duration
}

// NextDelay always returns the fixed delay regardless of attempt count.
func (f *FixedBackoff) NextDelay(_ int) time.Duration {
	return f.Delay
}

// ExponentialBackoff doubles (or scales by Factor) the wait time after each failure,
// capped at MaxDelay.
type ExponentialBackoff struct {
	// InitialDelay is the wait time after the first failure.
	InitialDelay time.Duration
	// MaxDelay is the upper bound on the computed delay.
	MaxDelay time.Duration
	// Factor is the multiplier applied per attempt. Defaults to 2.0 if zero.
	Factor float64
}

// NextDelay returns InitialDelay * Factor^(attempts-1), capped at MaxDelay.
func (e *ExponentialBackoff) NextDelay(attempts int) time.Duration {
	factor := e.Factor
	if factor <= 0 {
		factor = 2.0
	}
	if attempts <= 0 {
		return e.InitialDelay
	}
	delay := float64(e.InitialDelay) * math.Pow(factor, float64(attempts-1))
	if delay > float64(e.MaxDelay) {
		return e.MaxDelay
	}
	return time.Duration(delay)
}

// DefaultExponentialBackoff returns a sensible exponential backoff suitable for
// most production workloads: starts at 5 s and caps at 1 h.
func DefaultExponentialBackoff() *ExponentialBackoff {
	return &ExponentialBackoff{
		InitialDelay: 5 * time.Second,
		MaxDelay:     1 * time.Hour,
		Factor:       2.0,
	}
}

// NoBackoff returns a zero-delay backoff — retries are immediate.
// Useful in tests or for jobs that should be retried as fast as possible.
func NoBackoff() *FixedBackoff {
	return &FixedBackoff{Delay: 0}
}
