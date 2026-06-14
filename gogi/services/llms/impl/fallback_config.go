package impl

import "gogi/gogi/utils"

type FallBackConfig struct {
	Enabled     bool
	Providers   []string
	RetryConfig utils.RetryConfig
}
