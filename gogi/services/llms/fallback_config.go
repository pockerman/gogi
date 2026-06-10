package model_service

import "gogi/gogi/utils"

type FallBackConfig struct {
	Enabled     bool
	Providers   []string
	RetryConfig utils.RetryConfig
}
