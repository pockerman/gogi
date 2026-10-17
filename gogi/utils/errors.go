package utils

import "errors"

var ErrJobNotFound = errors.New("job not found")

var ErrRegisteredLLMNotFound = errors.New("registered LLM not found")

var ErrToolNotFound = errors.New("tool not found")

var ErrToolVersionExists = errors.New("tool version already registered")

var ErrToolTaskNotFound = errors.New("tool task not found")

var ErrIndexNotFound = errors.New("index not found")

var ErrLastIndexOwner = errors.New("an index must keep at least one owner")
