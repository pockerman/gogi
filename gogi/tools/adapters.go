package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"gogi/gogi/storage/postgres"
	"io"
	"net/http"
)

// Invocation is a call to a tool
type Invocation struct {
	Tool *postgres.GogiTool
	// ArgumentsJSON are the arguments of the call, validated against the tool's parameters
	ArgumentsJSON string
	// Credential is the secret the tool's credential_ref names, or empty if it has none
	Credential string
	SessionID  string
	// MaxResponseBytes bounds the size of the result
	MaxResponseBytes int64
}

// Adapter calls a tool where it is served, e.g. an HTTP endpoint or an MCP server, and
// returns its result as a JSON document
type Adapter interface {
	Invoke(ctx context.Context, invocation Invocation) (json.RawMessage, error)
}

// ToolError is a failed tool call. Retryable failures (the tool could not be reached, it
// timed out, or reported being overloaded or failing) may succeed if retried and count
// towards opening the tool's circuit. Other failures, e.g. the tool rejected the call,
// would fail again
type ToolError struct {
	Message   string
	Retryable bool
}

func (e *ToolError) Error() string {
	return e.Message
}

// IsRetryable reports whether a failed tool call may succeed if retried
func IsRetryable(err error) bool {
	var toolErr *ToolError
	if errors.As(err, &toolErr) {
		return toolErr.Retryable
	}
	// transport and timeout errors
	return err != nil
}

// HTTPAdapter calls a tool by POSTing its arguments, as JSON, to its endpoint. The
// credential, if any, is sent as a bearer token. A 2xx response is the result; its body
// is returned as is if it is JSON, or as a JSON string otherwise
type HTTPAdapter struct {
	client *http.Client
}

func NewHTTPAdapter(client *http.Client) *HTTPAdapter {
	if client == nil {
		client = &http.Client{}
	}
	return &HTTPAdapter{client: client}
}

func (a *HTTPAdapter) Invoke(ctx context.Context, invocation Invocation) (json.RawMessage, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, invocation.Tool.Endpoint,
		bytes.NewBufferString(invocation.ArgumentsJSON))
	if err != nil {
		return nil, &ToolError{Message: fmt.Sprintf("invalid endpoint: %v", err)}
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-Gogi-Tool", invocation.Tool.Name)
	request.Header.Set("X-Gogi-Tool-Version", invocation.Tool.Version)
	if invocation.SessionID != "" {
		request.Header.Set("X-Gogi-Session-Id", invocation.SessionID)
	}
	if invocation.Credential != "" {
		request.Header.Set("Authorization", "Bearer "+invocation.Credential)
	}

	response, err := a.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	body, err := readLimited(response.Body, invocation.MaxResponseBytes)
	if err != nil {
		return nil, err
	}

	if response.StatusCode < 200 || response.StatusCode > 299 {
		retryable := response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500
		return nil, &ToolError{Message: fmt.Sprintf("tool endpoint returned %s", response.Status), Retryable: retryable}
	}
	return asJSON(body), nil
}

// readLimited reads at most maxBytes, failing for a longer body
func readLimited(body io.Reader, maxBytes int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, &ToolError{Message: fmt.Sprintf("tool response exceeds the limit of %d KB", maxBytes/1024)}
	}
	return data, nil
}

// asJSON returns data if it is a JSON document, or data as a JSON string otherwise
func asJSON(data []byte) json.RawMessage {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return json.RawMessage("null")
	}
	if json.Valid(trimmed) {
		return json.RawMessage(trimmed)
	}
	encoded, _ := json.Marshal(string(data))
	return encoded
}
