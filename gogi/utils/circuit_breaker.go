package utils

import (
	"fmt"
	"strconv"
	"sync"
	"time"
)

// CircuitState is the state of a circuit
type CircuitState string

const (
	// CircuitClosed means the callee is healthy and calls go through
	CircuitClosed CircuitState = "closed"
	// CircuitOpen means the callee failed too often: calls fail immediately
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

// CircuitBreaker tracks the failures of each callee, e.g. a tool or a model, by key, and
// stops calling a callee that keeps failing, so that the platform does not hammer an
// external system that is struggling, and callers do not wait for calls that will fail.
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

// AllowRequest reports whether a call to the callee may go ahead
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

// RecordResult records the outcome of a call to the callee
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

// State returns the state of the circuit of the callee
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
// <prefix>_CIRCUIT_FAILURE_THRESHOLD (default 5 consecutive failures),
// <prefix>_CIRCUIT_RECOVERY_TIMEOUT (default 60s) and
// <prefix>_CIRCUIT_HALF_OPEN_MAX (default 2 trial calls), e.g. GOGI_TOOLS_CIRCUIT_FAILURE_THRESHOLD
func NewCircuitBreakerFromEnv(prefix string) (*CircuitBreaker, error) {
	thresholdName := prefix + "_CIRCUIT_FAILURE_THRESHOLD"
	threshold, err := strconv.Atoi(GetEnv(thresholdName, "5"))
	if err != nil || threshold < 1 {
		return nil, fmt.Errorf("invalid %s: must be a positive integer", thresholdName)
	}
	recoveryName := prefix + "_CIRCUIT_RECOVERY_TIMEOUT"
	recovery, err := time.ParseDuration(GetEnv(recoveryName, "60s"))
	if err != nil || recovery <= 0 {
		return nil, fmt.Errorf("invalid %s: must be a positive duration", recoveryName)
	}
	halfOpenName := prefix + "_CIRCUIT_HALF_OPEN_MAX"
	halfOpenMax, err := strconv.Atoi(GetEnv(halfOpenName, "2"))
	if err != nil || halfOpenMax < 1 {
		return nil, fmt.Errorf("invalid %s: must be a positive integer", halfOpenName)
	}
	return NewCircuitBreaker(threshold, recovery, halfOpenMax), nil
}
