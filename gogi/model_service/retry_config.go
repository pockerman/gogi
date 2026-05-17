package model_service

type RetryConfig struct {
	maxRetries         int
	intialDelay        float32
	exponentialBackoff bool
	maxDelay           float32
}

func New() RetryConfig {
	return RetryConfig{
		maxRetries:         3,
		intialDelay:        1.0,
		exponentialBackoff: true,
		maxDelay:           60.0,
	}
}
