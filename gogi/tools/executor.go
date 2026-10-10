package tools

import (
	"context"
	"errors"
	"fmt"
	"gogi/gogi/credentials"
	"gogi/gogi/storage/postgres"
	"gogi/gogi/utils"
	"strconv"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
)

// Status is the outcome of a tool call
type Status string

const (
	StatusPending   Status = "pending"
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
	StatusTimedOut  Status = "timed_out"
)

// redacted replaces a credential that appears in a result or an error
const redacted = "[REDACTED]"

// ExecutorConfig holds the platform's defaults for tool calls, used when a tool does not
// set its own limits, and the ceilings that no tool can exceed
type ExecutorConfig struct {
	DefaultTimeout       time.Duration
	MaxTimeout           time.Duration
	DefaultMaxResponseKB int32
	MaxResponseKB        int32
	MaxRetries           int32
}

// DefaultExecutorConfig returns the default configuration of tool calls
func DefaultExecutorConfig() ExecutorConfig {
	return ExecutorConfig{
		DefaultTimeout:       30 * time.Second,
		MaxTimeout:           5 * time.Minute,
		DefaultMaxResponseKB: 1024,
		MaxResponseKB:        10 * 1024,
		MaxRetries:           3,
	}
}

// ExecutorConfigFromEnv reads the configuration of tool calls from the environment:
// GOGI_TOOLS_DEFAULT_TIMEOUT, GOGI_TOOLS_MAX_TIMEOUT (Go durations),
// GOGI_TOOLS_DEFAULT_MAX_RESPONSE_KB, GOGI_TOOLS_MAX_RESPONSE_KB and GOGI_TOOLS_MAX_RETRIES
func ExecutorConfigFromEnv() (ExecutorConfig, error) {
	config := DefaultExecutorConfig()

	durations := map[string]*time.Duration{
		"GOGI_TOOLS_DEFAULT_TIMEOUT": &config.DefaultTimeout,
		"GOGI_TOOLS_MAX_TIMEOUT":     &config.MaxTimeout,
	}
	for name, value := range durations {
		parsed, err := time.ParseDuration(utils.GetEnv(name, value.String()))
		if err != nil || parsed <= 0 {
			return config, fmt.Errorf("invalid %s: must be a positive duration", name)
		}
		*value = parsed
	}

	integers := map[string]*int32{
		"GOGI_TOOLS_DEFAULT_MAX_RESPONSE_KB": &config.DefaultMaxResponseKB,
		"GOGI_TOOLS_MAX_RESPONSE_KB":         &config.MaxResponseKB,
		"GOGI_TOOLS_MAX_RETRIES":             &config.MaxRetries,
	}
	for name, value := range integers {
		parsed, err := strconv.ParseInt(utils.GetEnv(name, strconv.Itoa(int(*value))), 10, 32)
		if err != nil || parsed < 0 {
			return config, fmt.Errorf("invalid %s: must be a non-negative integer", name)
		}
		*value = int32(parsed)
	}
	return config, nil
}

// Result is the outcome of a tool call
type Result struct {
	Status     Status
	ResultJSON string
	Error      string
	Duration   time.Duration
}

// Executor calls tools on behalf of applications: it enforces their rate limits,
// injects their credentials, stops calling tools that keep failing, bounds every call
// by the tool's timeout and response size, and retries failed calls to idempotent tools
type Executor struct {
	credentialStore credentials.CredentialStore
	breaker         *CircuitBreaker
	limiter         *RateLimiter
	httpAdapter     Adapter
	mcpAdapter      Adapter
	config          ExecutorConfig
}

func NewExecutor(credentialStore credentials.CredentialStore, breaker *CircuitBreaker, limiter *RateLimiter,
	httpAdapter, mcpAdapter Adapter, config ExecutorConfig) *Executor {
	return &Executor{
		credentialStore: credentialStore,
		breaker:         breaker,
		limiter:         limiter,
		httpAdapter:     httpAdapter,
		mcpAdapter:      mcpAdapter,
		config:          config,
	}
}

// CircuitKey identifies the circuit of a version of a tool
func CircuitKey(tool *postgres.GogiTool) string {
	return tool.Name + "@" + tool.Version
}

// Available reports whether the tool's circuit lets calls through
func (e *Executor) Available(tool *postgres.GogiTool) bool {
	return e.breaker.State(CircuitKey(tool)) != CircuitOpen
}

func failed(start time.Time, format string, args ...any) Result {
	return Result{Status: StatusFailed, Error: fmt.Sprintf(format, args...), Duration: time.Since(start)}
}

// Execute calls the tool with the arguments, which must be valid for its parameters.
// Failures are reported in the result, as errors the model can reason about, rather
// than returned
func (e *Executor) Execute(ctx context.Context, tool *postgres.GogiTool, argumentsJSON, sessionID string) Result {
	start := time.Now()

	if err := e.limiter.Allow(tool, sessionID); err != nil {
		return failed(start, "%v", err)
	}

	credential := ""
	if tool.CredentialRef != "" {
		if e.credentialStore == nil {
			return failed(start, "the tool needs credential %q but no credential store is configured", tool.CredentialRef)
		}
		stored, err := e.credentialStore.Retrieve(ctx, tool.CredentialRef)
		if err != nil {
			log.Errorf("failed to retrieve credential %q of tool %s: %v", tool.CredentialRef, CircuitKey(tool), err)
			return failed(start, "the credential of the tool is unavailable")
		}
		credential = stored.Value
	}

	key := CircuitKey(tool)
	if !e.breaker.AllowRequest(key) {
		return failed(start, "the tool is unavailable after repeated failures; try again later or use another tool")
	}

	adapter := e.httpAdapter
	if tool.MCPServerURL != "" {
		adapter = e.mcpAdapter
	}

	timeout := e.timeout(tool)
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	invocation := Invocation{
		Tool:             tool,
		ArgumentsJSON:    argumentsJSON,
		Credential:       credential,
		SessionID:        sessionID,
		MaxResponseBytes: int64(e.maxResponseKB(tool)) * 1024,
	}

	retries := e.retries(tool)
	for attempt := int32(0); ; attempt++ {
		output, err := adapter.Invoke(callCtx, invocation)
		if err == nil {
			e.breaker.RecordResult(key, true)
			return Result{Status: StatusSucceeded, ResultJSON: redact(string(output), credential), Duration: time.Since(start)}
		}

		switch {
		case ctx.Err() != nil:
			// the caller went away; that says nothing about the tool's health
			return failed(start, "the call was cancelled")
		case callCtx.Err() != nil:
			e.breaker.RecordResult(key, false)
			return Result{Status: StatusTimedOut, Error: fmt.Sprintf("tool execution timed out after %s", timeout),
				Duration: time.Since(start)}
		case IsRetryable(err) && attempt < retries:
			log.Warnf("call %d to tool %s failed, retrying: %v", attempt+1, key, redact(err.Error(), credential))
			// if the timeout expires while waiting, the next attempt reports it
			_ = sleep(callCtx, backoff(attempt))
			continue
		}

		// a tool that is reached but rejects the call is healthy
		e.breaker.RecordResult(key, !IsRetryable(err))
		return failed(start, "%s", e.describe(err, key, credential))
	}
}

// describe returns the message of a failed call. Errors reported by the tool are passed
// on; transport errors are logged, but not returned, as they name the tool's endpoint
func (e *Executor) describe(err error, key, credential string) string {
	var toolErr *ToolError
	if errors.As(err, &toolErr) {
		return redact(toolErr.Message, credential)
	}
	log.Errorf("tool %s could not be reached: %v", key, redact(err.Error(), credential))
	return "the tool could not be reached"
}

func (e *Executor) timeout(tool *postgres.GogiTool) time.Duration {
	timeout := e.config.DefaultTimeout
	if tool.ExecutionLimits.TimeoutSeconds > 0 {
		timeout = time.Duration(tool.ExecutionLimits.TimeoutSeconds) * time.Second
	}
	return min(timeout, e.config.MaxTimeout)
}

func (e *Executor) maxResponseKB(tool *postgres.GogiTool) int32 {
	limit := e.config.DefaultMaxResponseKB
	if tool.ExecutionLimits.MaxResponseSizeKB > 0 {
		limit = tool.ExecutionLimits.MaxResponseSizeKB
	}
	return min(limit, e.config.MaxResponseKB)
}

// retries returns how many times a failed call may be retried: retrying a tool that is
// not idempotent could repeat its side effects, e.g. book an appointment twice
func (e *Executor) retries(tool *postgres.GogiTool) int32 {
	if !tool.Behavior.IsIdempotent {
		return 0
	}
	return min(tool.ExecutionLimits.MaxRetries, e.config.MaxRetries)
}

func backoff(attempt int32) time.Duration {
	return min(100*time.Millisecond<<attempt, 2*time.Second)
}

func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// redact removes the credential from text returned to the caller
func redact(text, credential string) string {
	if credential == "" {
		return text
	}
	return strings.ReplaceAll(text, credential, redacted)
}
