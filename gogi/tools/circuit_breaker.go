package tools

import (
	"fmt"
	"gogi/gogi/utils"
	"strconv"
	"sync"
	"time"
)

// CircuitState is the state of the circuit of a tool
type CircuitState string

const (
	// CircuitClosed means the tool is healthy and calls go through
	CircuitClosed CircuitState = "closed"
	// CircuitOpen means the tool failed too often: calls fail immediately
	CircuitOpen CircuitState = "open"
	// CircuitHalfOpen means the recovery timeout elapsed: a few trial calls go through
	CircuitHalfOpen CircuitState = "half_open"
)

type circuit struct {
	state               CircuitState
	consecutiveFailures int
	openedAt            time.Time
	trialCalls          int
}

// CircuitBreaker tracks the failures of each tool and stops calling a tool that keeps
// failing, so that the platform does not hammer an external system that is struggling.
// After failureThreshold consecutive failures the circuit opens; after recoveryTimeout
// it lets halfOpenMax trial calls through, and closes again if one succeeds
type CircuitBreaker struct {
	mu               sync.Mutex
	circuits         map[string]*circuit
	failureThreshold int
	recoveryTimeout  time.Duration
	halfOpenMax      int
	now              func() time.Time
}

func NewCircuitBreaker(failureThreshold int, recoveryTimeout time.Duration, halfOpenMax int) *CircuitBreaker {
	return &CircuitBreaker{
		circuits:         make(map[string]*circuit),
		failureThreshold: failureThreshold,
		recoveryTimeout:  recoveryTimeout,
		halfOpenMax:      halfOpenMax,
		now:              time.Now,
	}
}

func (b *CircuitBreaker) circuit(key string) *circuit {
	c, ok := b.circuits[key]
	if !ok {
		c = &circuit{state: CircuitClosed}
		b.circuits[key] = c
	}
	return c
}

// AllowRequest reports whether a call to the tool may go ahead
func (b *CircuitBreaker) AllowRequest(key string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	c := b.circuit(key)
	if c.state == CircuitOpen {
		if b.now().Sub(c.openedAt) < b.recoveryTimeout {
			return false
		}
		c.state = CircuitHalfOpen
		c.trialCalls = 0
	}
	if c.state == CircuitHalfOpen {
		if c.trialCalls >= b.halfOpenMax {
			return false
		}
		c.trialCalls++
	}
	return true
}

// RecordResult records the outcome of a call to the tool
func (b *CircuitBreaker) RecordResult(key string, success bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	c := b.circuit(key)
	if success {
		*c = circuit{state: CircuitClosed}
		return
	}

	c.consecutiveFailures++
	if c.state == CircuitHalfOpen || c.consecutiveFailures >= b.failureThreshold {
		c.state = CircuitOpen
		c.openedAt = b.now()
	}
}

// State returns the state of the circuit of the tool
func (b *CircuitBreaker) State(key string) CircuitState {
	b.mu.Lock()
	defer b.mu.Unlock()

	c, ok := b.circuits[key]
	if !ok {
		return CircuitClosed
	}
	if c.state == CircuitOpen && b.now().Sub(c.openedAt) >= b.recoveryTimeout {
		return CircuitHalfOpen
	}
	return c.state
}

// NewCircuitBreakerFromEnv creates the circuit breaker configured by
// GOGI_TOOLS_CIRCUIT_FAILURE_THRESHOLD (default 5 consecutive failures),
// GOGI_TOOLS_CIRCUIT_RECOVERY_TIMEOUT (default 60s) and
// GOGI_TOOLS_CIRCUIT_HALF_OPEN_MAX (default 2 trial calls)
func NewCircuitBreakerFromEnv() (*CircuitBreaker, error) {
	threshold, err := strconv.Atoi(utils.GetEnv("GOGI_TOOLS_CIRCUIT_FAILURE_THRESHOLD", "5"))
	if err != nil || threshold < 1 {
		return nil, fmt.Errorf("invalid GOGI_TOOLS_CIRCUIT_FAILURE_THRESHOLD: must be a positive integer")
	}
	recovery, err := time.ParseDuration(utils.GetEnv("GOGI_TOOLS_CIRCUIT_RECOVERY_TIMEOUT", "60s"))
	if err != nil || recovery <= 0 {
		return nil, fmt.Errorf("invalid GOGI_TOOLS_CIRCUIT_RECOVERY_TIMEOUT: must be a positive duration")
	}
	halfOpenMax, err := strconv.Atoi(utils.GetEnv("GOGI_TOOLS_CIRCUIT_HALF_OPEN_MAX", "2"))
	if err != nil || halfOpenMax < 1 {
		return nil, fmt.Errorf("invalid GOGI_TOOLS_CIRCUIT_HALF_OPEN_MAX: must be a positive integer")
	}
	return NewCircuitBreaker(threshold, recovery, halfOpenMax), nil
}
