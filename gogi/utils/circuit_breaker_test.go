package utils

import (
	"testing"
	"time"
)

// fakeClock is a settable clock
type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

func TestCircuitBreaker(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	breaker := NewCircuitBreaker(3, time.Minute, 1)
	breaker.now = clock.Now

	for range 2 {
		breaker.RecordResult("tool@1.0.0", false)
	}
	// a success resets the count of consecutive failures
	breaker.RecordResult("tool@1.0.0", true)
	for range 2 {
		breaker.RecordResult("tool@1.0.0", false)
	}
	if !breaker.AllowRequest("tool@1.0.0") || breaker.State("tool@1.0.0") != CircuitClosed {
		t.Fatal("the circuit opened before 3 consecutive failures")
	}

	breaker.RecordResult("tool@1.0.0", false)
	if breaker.AllowRequest("tool@1.0.0") || breaker.State("tool@1.0.0") != CircuitOpen {
		t.Fatal("the circuit did not open after 3 consecutive failures")
	}
	if !breaker.AllowRequest("other@1.0.0") {
		t.Error("circuits are per tool")
	}

	// after the recovery timeout, one trial call goes through
	clock.now = clock.now.Add(time.Minute)
	if breaker.State("tool@1.0.0") != CircuitHalfOpen || !breaker.AllowRequest("tool@1.0.0") {
		t.Fatal("expected a trial call after the recovery timeout")
	}
	if breaker.AllowRequest("tool@1.0.0") {
		t.Error("only one trial call is allowed")
	}

	// a failed trial opens the circuit again
	breaker.RecordResult("tool@1.0.0", false)
	if breaker.AllowRequest("tool@1.0.0") {
		t.Fatal("a failed trial opens the circuit")
	}

	clock.now = clock.now.Add(time.Minute)
	if !breaker.AllowRequest("tool@1.0.0") {
		t.Fatal("expected a trial call")
	}
	breaker.RecordResult("tool@1.0.0", true)
	if breaker.State("tool@1.0.0") != CircuitClosed || !breaker.AllowRequest("tool@1.0.0") || !breaker.AllowRequest("tool@1.0.0") {
		t.Error("a successful trial closes the circuit")
	}
}

func TestNewCircuitBreakerFromEnv(t *testing.T) {
	t.Setenv("GOGI_TEST_CIRCUIT_FAILURE_THRESHOLD", "3")
	breaker, err := NewCircuitBreakerFromEnv("GOGI_TEST")
	if err != nil || breaker.failureThreshold != 3 || breaker.recoveryTimeout != time.Minute || breaker.halfOpenMax != 2 {
		t.Errorf("wrong breaker %+v, %v", breaker, err)
	}

	t.Setenv("GOGI_TEST_CIRCUIT_FAILURE_THRESHOLD", "0")
	if _, err := NewCircuitBreakerFromEnv("GOGI_TEST"); err == nil {
		t.Error("expected an invalid threshold")
	}
}
