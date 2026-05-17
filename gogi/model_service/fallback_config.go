package model_service

type FallBackConfig struct {
	Enabled     bool
	Providers   []string
	RetryConfig RetryConfig
}
