package utils

import "errors"

var ErrJobNotFound = errors.New("job not found")

var ErrRegisteredLLMNotFound = errors.New("registered LLM not found")
