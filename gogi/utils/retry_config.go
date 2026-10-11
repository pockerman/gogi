package utils

import "time"

// RetryConfig says how a failed call is retried: up to MaxRetries times, waiting
// InitialDelay before the first retry and doubling the delay, up to MaxDelay, on every retry
type RetryConfig struct {
	MaxRetries   int
	InitialDelay time.Duration
	MaxDelay     time.Duration
}

func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:   2,
		InitialDelay: 500 * time.Millisecond,
		MaxDelay:     20 * time.Second,
	}
}

// Delay returns the delay before the given retry, counting from 0
func (config RetryConfig) Delay(retry int) time.Duration {
	delay := config.InitialDelay
	for i := 0; i < retry && delay < config.MaxDelay; i++ {
		delay *= 2
	}
	return min(delay, config.MaxDelay)
}
