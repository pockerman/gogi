package tools

import (
	"context"
	"errors"
	"gogi/gogi/credentials"
	"gogi/gogi/storage/postgres"
	"gogi/gogi/utils"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const bookingParameters = `{"type":"object","properties":{"patient_id":{"type":"string"},"slot_id":{"type":"string"}},
	"required":["patient_id","slot_id"]}`

func newTool(name, version, parameters string) *postgres.GogiTool {
	return &postgres.GogiTool{
		Name: name, Version: version, Description: "Book an appointment slot for a patient",
		ParametersJSON: parameters, Endpoint: "https://scheduler.example.com/v1/bookings",
	}
}

func TestNormalizeDefinition(t *testing.T) {
	tool := &postgres.GogiTool{Name: "healthcare.scheduling.check_availability", Description: "Check slots",
		Endpoint: "http://scheduler:8080/availability"}
	if err := NormalizeDefinition(tool); err != nil {
		t.Fatal(err)
	}
	if tool.Version != DefaultToolVersion || tool.Owner != DefaultToolOwner || tool.ParametersJSON != DefaultParametersJSON {
		t.Errorf("defaults not set %+v", tool)
	}

	mcpTool := &postgres.GogiTool{Name: "devtools.github.create_issue", Description: "Create an issue",
		MCPServerURL: "http://github-mcp:3000/mcp"}
	if err := NormalizeDefinition(mcpTool); err != nil || mcpTool.MCPToolName != "create_issue" {
		t.Errorf("MCP tool name not set %+v, %v", mcpTool, err)
	}

	invalid := map[string]func(*postgres.GogiTool){
		"name":                func(tool *postgres.GogiTool) { tool.Name = "healthcare..book" },
		"name with spaces":    func(tool *postgres.GogiTool) { tool.Name = "book appointment" },
		"version":             func(tool *postgres.GogiTool) { tool.Version = "1.0" },
		"description":         func(tool *postgres.GogiTool) { tool.Description = " " },
		"parameters JSON":     func(tool *postgres.GogiTool) { tool.ParametersJSON = "{" },
		"parameters schema":   func(tool *postgres.GogiTool) { tool.ParametersJSON = `{"type": 5}` },
		"parameters type":     func(tool *postgres.GogiTool) { tool.ParametersJSON = `{"type":"string"}` },
		"external reference":  func(tool *postgres.GogiTool) { tool.ParametersJSON = `{"type":"object","$ref":"file:///etc/passwd"}` },
		"returns schema":      func(tool *postgres.GogiTool) { tool.ReturnsJSON = `{"type": 5}` },
		"no endpoint":         func(tool *postgres.GogiTool) { tool.Endpoint = "" },
		"endpoint scheme":     func(tool *postgres.GogiTool) { tool.Endpoint = "ftp://scheduler/bookings" },
		"endpoint and server": func(tool *postgres.GogiTool) { tool.MCPServerURL = "http://mcp:3000/mcp" },
		"negative limit":      func(tool *postgres.GogiTool) { tool.ExecutionLimits.TimeoutSeconds = -1 },
	}
	for name, change := range invalid {
		tool := newTool("healthcare.scheduling.book_appointment", "1.0.0", bookingParameters)
		change(tool)
		if err := NormalizeDefinition(tool); !errors.Is(err, ErrInvalidDefinition) {
			t.Errorf("%s: expected ErrInvalidDefinition, got %v", name, err)
		}
	}
}

func TestCheckCompatibility(t *testing.T) {
	name := "healthcare.scheduling.book_appointment"
	registered := []*postgres.GogiTool{newTool(name, "1.0.0", bookingParameters)}

	compatible := newTool(name, "1.1.0", `{"type":"object","properties":{"patient_id":{"type":"string"},
		"slot_id":{"type":"string"},"notes":{"type":"string"}},"required":["patient_id","slot_id"]}`)
	if err := CheckCompatibility(compatible, registered); err != nil {
		t.Errorf("an optional parameter is compatible: %v", err)
	}
	registered = append(registered, compatible)

	// compared with 1.1.0, the previous version in major version 1
	removesNotes := newTool(name, "1.2.0", bookingParameters)
	if err := CheckCompatibility(removesNotes, registered); !errors.Is(err, ErrIncompatibleVersion) ||
		!strings.Contains(err.Error(), `removes property "notes"`) || !strings.Contains(err.Error(), "2.0.0") {
		t.Errorf("expected ErrIncompatibleVersion for a removed parameter, got %v", err)
	}

	newRequired := newTool(name, "1.2.0", `{"type":"object","properties":{"patient_id":{"type":"string"},
		"slot_id":{"type":"string"},"notes":{"type":"string"},"appointment_type":{"type":"string"}},
		"required":["patient_id","slot_id","appointment_type"]}`)
	if err := CheckCompatibility(newRequired, registered); !errors.Is(err, ErrIncompatibleVersion) {
		t.Errorf("expected ErrIncompatibleVersion for a new required parameter, got %v", err)
	}

	// a new major version may break callers
	newRequired.Version = "2.0.0"
	if err := CheckCompatibility(newRequired, registered); err != nil {
		t.Errorf("a major version may change the parameters: %v", err)
	}

	withResults := newTool(name, "3.0.0", bookingParameters)
	withResults.ReturnsJSON = `{"type":"object","properties":{"booking_id":{"type":"string"}}}`
	withoutResult := newTool(name, "3.1.0", bookingParameters)
	withoutResult.ReturnsJSON = `{"type":"object","properties":{}}`
	if err := CheckCompatibility(withoutResult, []*postgres.GogiTool{withResults}); !errors.Is(err, ErrIncompatibleVersion) {
		t.Errorf("expected ErrIncompatibleVersion for a removed result, got %v", err)
	}
}

func TestSelectVersion(t *testing.T) {
	name := "healthcare.scheduling.book_appointment"
	versions := []*postgres.GogiTool{newTool(name, "1.0.0", ""), newTool(name, "1.4.2", ""),
		newTool(name, "2.0.0", ""), newTool(name, "1.10.0", "")}

	cases := map[string]string{"": "2.0.0", "^1.0.0": "1.10.0", ">=1.0.0 <1.5.0": "1.4.2", "~1.4": "1.4.2"}
	for constraint, expected := range cases {
		tool, err := SelectVersion(versions, constraint)
		if err != nil || tool.Version != expected {
			t.Errorf("%q: expected %s, got %v, %v", constraint, expected, tool, err)
		}
	}

	if _, err := SelectVersion(versions, "^3.0.0"); !errors.Is(err, utils.ErrToolNotFound) {
		t.Errorf("expected ErrToolNotFound, got %v", err)
	}
	if _, err := SelectVersion(versions, "not a constraint"); err == nil || errors.Is(err, utils.ErrToolNotFound) {
		t.Errorf("expected an invalid constraint error, got %v", err)
	}

	if tool, err := FindVersion(versions, "1.4.2"); err != nil || tool.Version != "1.4.2" {
		t.Errorf("expected 1.4.2, got %v, %v", tool, err)
	}
	if tool, err := FindVersion(versions, ""); err != nil || tool.Version != "2.0.0" {
		t.Errorf("expected the latest version, got %v, %v", tool, err)
	}
	if _, err := FindVersion(versions, "1.2.0"); !errors.Is(err, utils.ErrToolNotFound) {
		t.Errorf("expected ErrToolNotFound, got %v", err)
	}
}

func TestNamespacePrefix(t *testing.T) {
	cases := map[string]string{"": "", "*": "", "healthcare": "healthcare.", "healthcare.*": "healthcare.",
		"healthcare.scheduling": "healthcare.scheduling.", "healthcare.scheduling.*": "healthcare.scheduling."}
	for namespace, expected := range cases {
		if prefix := NamespacePrefix(namespace); prefix != expected {
			t.Errorf("%q: expected %q, got %q", namespace, expected, prefix)
		}
	}
}

func TestValidateArguments(t *testing.T) {
	schema, err := CompileSchema(bookingParameters)
	if err != nil {
		t.Fatal(err)
	}

	if problems := ValidateArguments(schema, `{"patient_id":"p-1","slot_id":"s-1"}`); len(problems) != 0 {
		t.Errorf("valid arguments rejected: %v", problems)
	}

	problems := ValidateArguments(schema, `{"patient_id":7}`)
	joined := strings.Join(problems, "; ")
	if !strings.Contains(joined, "slot_id") || !strings.Contains(joined, "/patient_id") {
		t.Errorf("expected the missing slot_id and the wrong patient_id type, got %v", problems)
	}

	if problems := ValidateArguments(schema, `{"patient_id":`); len(problems) != 1 ||
		!strings.Contains(problems[0], "not valid JSON") {
		t.Errorf("expected invalid JSON, got %v", problems)
	}

	empty, _ := CompileSchema(DefaultParametersJSON)
	if problems := ValidateArguments(empty, ""); len(problems) != 0 {
		t.Errorf("no arguments are an empty object: %v", problems)
	}
}

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

func TestRateLimiter(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)}
	limiter := NewRateLimiter()
	limiter.now = clock.Now

	perMinute := newTool("healthcare.scheduling.check_availability", "1.0.0", "")
	perMinute.RateLimits.RequestsPerMinute = 2
	for range 2 {
		if err := limiter.Allow(perMinute, ""); err != nil {
			t.Fatal(err)
		}
	}
	if err := limiter.Allow(perMinute, ""); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("expected ErrRateLimited, got %v", err)
	}
	clock.now = clock.now.Add(time.Minute)
	if err := limiter.Allow(perMinute, ""); err != nil {
		t.Errorf("a new minute starts a new window: %v", err)
	}

	perSession := newTool("healthcare.scheduling.book_appointment", "1.0.0", "")
	perSession.RateLimits.RequestsPerSession = 1
	if err := limiter.Allow(perSession, ""); !errors.Is(err, ErrRateLimited) {
		t.Errorf("a tool limited per session needs a session id, got %v", err)
	}
	if err := limiter.Allow(perSession, "session-1"); err != nil {
		t.Fatal(err)
	}
	if err := limiter.Allow(perSession, "session-1"); !errors.Is(err, ErrRateLimited) {
		t.Errorf("expected ErrRateLimited, got %v", err)
	}
	if err := limiter.Allow(perSession, "session-2"); err != nil {
		t.Errorf("sessions are limited separately: %v", err)
	}

	daily := newTool("healthcare.billing.verify_insurance", "1.0.0", "")
	daily.RateLimits.DailyLimit = 1
	if err := limiter.Allow(daily, ""); err != nil {
		t.Fatal(err)
	}
	clock.now = clock.now.Add(time.Hour)
	if err := limiter.Allow(daily, ""); !errors.Is(err, ErrRateLimited) {
		t.Errorf("expected ErrRateLimited, got %v", err)
	}
	clock.now = time.Date(2026, 1, 2, 0, 0, 1, 0, time.UTC)
	if err := limiter.Allow(daily, ""); err != nil {
		t.Errorf("a new day starts a new window: %v", err)
	}

	unlimited := newTool("healthcare.scheduling.list_clinics", "1.0.0", "")
	for range 100 {
		if err := limiter.Allow(unlimited, ""); err != nil {
			t.Fatal(err)
		}
	}
}

func newTestExecutor(store credentials.CredentialStore, config ExecutorConfig) *Executor {
	return NewExecutor(store, NewCircuitBreaker(2, time.Minute, 1), NewRateLimiter(),
		NewHTTPAdapter(nil), NewMCPAdapter(nil), config)
}

func TestExecutorInjectsAndRedactsTheCredential(t *testing.T) {
	var authorization, body, toolHeader, sessionHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization, toolHeader, sessionHeader = r.Header.Get("Authorization"), r.Header.Get("X-Gogi-Tool"),
			r.Header.Get("X-Gogi-Session-Id")
		data, _ := io.ReadAll(r.Body)
		body = string(data)
		// a careless tool echoes the key back
		_, _ = w.Write([]byte(`{"booking_id":"b-1","debug":"key s3cr3t-key"}`))
	}))
	defer server.Close()

	store := credentials.NewMemoryCredentialStore()
	if err := store.Store(context.Background(), "scheduling-api-prod", "s3cr3t-key"); err != nil {
		t.Fatal(err)
	}

	tool := newTool("healthcare.scheduling.book_appointment", "1.0.0", bookingParameters)
	tool.Endpoint, tool.CredentialRef = server.URL, "scheduling-api-prod"

	result := newTestExecutor(store, DefaultExecutorConfig()).Execute(context.Background(), tool,
		`{"patient_id":"p-1","slot_id":"s-1"}`, "session-1")
	if result.Status != StatusSucceeded {
		t.Fatalf("expected success, got %+v", result)
	}
	if authorization != "Bearer s3cr3t-key" || toolHeader != tool.Name || sessionHeader != "session-1" ||
		body != `{"patient_id":"p-1","slot_id":"s-1"}` {
		t.Errorf("wrong request: %q %q %q %q", authorization, toolHeader, sessionHeader, body)
	}
	if strings.Contains(result.ResultJSON, "s3cr3t-key") || !strings.Contains(result.ResultJSON, redacted) {
		t.Errorf("the credential was not redacted: %s", result.ResultJSON)
	}

	// without a credential store, a tool with a credential cannot be called
	result = newTestExecutor(nil, DefaultExecutorConfig()).Execute(context.Background(), tool, `{}`, "")
	if result.Status != StatusFailed || !strings.Contains(result.Error, "no credential store") {
		t.Errorf("expected a failure, got %+v", result)
	}

	// an unknown credential
	tool.CredentialRef = "unknown"
	result = newTestExecutor(store, DefaultExecutorConfig()).Execute(context.Background(), tool, `{}`, "")
	if result.Status != StatusFailed || result.Error != "the credential of the tool is unavailable" {
		t.Errorf("expected a failure, got %+v", result)
	}
}

func TestExecutorResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/text":
			_, _ = w.Write([]byte("3 slots available"))
		case "/empty":
			w.WriteHeader(http.StatusNoContent)
		case "/large":
			_, _ = w.Write([]byte(`"` + strings.Repeat("x", 3000) + `"`))
		case "/rejected":
			http.Error(w, "unknown patient", http.StatusNotFound)
		}
	}))
	defer server.Close()

	executor := newTestExecutor(nil, DefaultExecutorConfig())
	cases := map[string]Result{
		"/text":     {Status: StatusSucceeded, ResultJSON: `"3 slots available"`},
		"/empty":    {Status: StatusSucceeded, ResultJSON: "null"},
		"/large":    {Status: StatusFailed, Error: "tool response exceeds the limit of 2 KB"},
		"/rejected": {Status: StatusFailed, Error: "tool endpoint returned 404 Not Found"},
	}
	for path, expected := range cases {
		tool := newTool("healthcare.scheduling.check_availability", "1.0.0", "")
		tool.Endpoint = server.URL + path
		tool.ExecutionLimits.MaxResponseSizeKB = 2

		result := executor.Execute(context.Background(), tool, `{}`, "")
		if result.Status != expected.Status || result.ResultJSON != expected.ResultJSON || result.Error != expected.Error {
			t.Errorf("%s: expected %+v, got %+v", path, expected, result)
		}
	}

	// the endpoint is not disclosed when the tool cannot be reached
	tool := newTool("healthcare.scheduling.check_availability", "1.0.0", "")
	tool.Endpoint = "http://127.0.0.1:1/unreachable"
	result := executor.Execute(context.Background(), tool, `{}`, "")
	if result.Status != StatusFailed || result.Error != "the tool could not be reached" {
		t.Errorf("expected an unreachable tool, got %+v", result)
	}
}

func TestExecutorRetriesOnlyIdempotentTools(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			http.Error(w, "overloaded", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"slots":3}`))
	}))
	defer server.Close()

	executor := newTestExecutor(nil, DefaultExecutorConfig())

	idempotent := newTool("healthcare.scheduling.check_availability", "1.0.0", "")
	idempotent.Endpoint, idempotent.Behavior.IsIdempotent, idempotent.ExecutionLimits.MaxRetries = server.URL, true, 2
	result := executor.Execute(context.Background(), idempotent, `{}`, "")
	if result.Status != StatusSucceeded || calls.Load() != 3 {
		t.Errorf("expected success after 2 retries, got %+v after %d calls", result, calls.Load())
	}

	calls.Store(0)
	booking := newTool("healthcare.scheduling.book_appointment", "1.0.0", "")
	booking.Endpoint, booking.ExecutionLimits.MaxRetries = server.URL, 2
	result = executor.Execute(context.Background(), booking, `{}`, "")
	if result.Status != StatusFailed || calls.Load() != 1 {
		t.Errorf("a tool that is not idempotent is not retried, got %+v after %d calls", result, calls.Load())
	}
}

func TestExecutorTimeoutAndCircuit(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	defer server.Close()
	defer close(release)

	config := DefaultExecutorConfig()
	config.DefaultTimeout = 50 * time.Millisecond
	executor := newTestExecutor(nil, config)

	tool := newTool("healthcare.billing.verify_insurance", "1.0.0", "")
	tool.Endpoint = server.URL

	// the circuit opens after 2 consecutive failures
	for range 2 {
		result := executor.Execute(context.Background(), tool, `{}`, "")
		if result.Status != StatusTimedOut || !strings.Contains(result.Error, "timed out") {
			t.Fatalf("expected a timeout, got %+v", result)
		}
	}
	if executor.Available(tool) {
		t.Error("the tool should be unavailable")
	}
	result := executor.Execute(context.Background(), tool, `{}`, "")
	if result.Status != StatusFailed || !strings.Contains(result.Error, "unavailable after repeated failures") ||
		result.Duration > 40*time.Millisecond {
		t.Errorf("expected an immediate failure, got %+v", result)
	}

	// the tool's own timeout is capped by the platform ceiling
	config.MaxTimeout = time.Second
	tool.ExecutionLimits.TimeoutSeconds = 60
	if timeout := newTestExecutor(nil, config).timeout(tool); timeout != time.Second {
		t.Errorf("expected the ceiling, got %s", timeout)
	}
}

func TestExecutorConfigFromEnv(t *testing.T) {
	t.Setenv("GOGI_TOOLS_MAX_TIMEOUT", "2m")
	t.Setenv("GOGI_TOOLS_MAX_RETRIES", "1")
	config, err := ExecutorConfigFromEnv()
	if err != nil || config.MaxTimeout != 2*time.Minute || config.MaxRetries != 1 || config.DefaultTimeout != 30*time.Second {
		t.Errorf("wrong config %+v, %v", config, err)
	}

	t.Setenv("GOGI_TOOLS_DEFAULT_TIMEOUT", "soon")
	if _, err := ExecutorConfigFromEnv(); err == nil {
		t.Error("expected an invalid timeout")
	}

	t.Setenv("GOGI_TOOLS_CIRCUIT_FAILURE_THRESHOLD", "0")
	if _, err := NewCircuitBreakerFromEnv(); err == nil {
		t.Error("expected an invalid threshold")
	}
}
