package providers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"gogi/gogi/llm"
)

func keysOf(keys map[string]string) ApplicationKeys {
	return func(provider string) (string, error) {
		if key, ok := keys[provider]; ok {
			return key, nil
		}
		return "", fmt.Errorf("no %s key", provider)
	}
}

func TestAnthropicIsCalledWithTheApplicationsKey(t *testing.T) {
	seen := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("x-api-key")
		w.Header().Set("Content-Type", "text/event-stream")
		for _, event := range anthropicToolStream {
			fmt.Fprintf(w, "%s\n\n", event)
		}
	}))
	t.Cleanup(server.Close)
	provider := NewAnthropicLLMModelProvider("platform-key")
	provider.SetBaseURL(server.URL)
	messages := []llm.LLMMessage{{Role: "user", Content: "Hello"}}
	config := llm.LLMModelConfig{ModelName: "claude-opus-5-5"}

	ctx := WithApplicationKeys(context.Background(), keysOf(map[string]string{"anthropic": "application-key"}))
	if _, err := provider.Run(ctx, messages, config); err != nil {
		t.Fatal(err)
	}
	if seen != "application-key" {
		t.Fatalf("called with %q", seen)
	}

	// an application without a key is not given the platform's
	seen = ""
	ctx = WithApplicationKeys(context.Background(), keysOf(nil))
	if _, err := provider.Run(ctx, messages, config); err == nil || seen != "" {
		t.Fatalf("expected an error without calling Anthropic, got %v and %q", err, seen)
	}

	// a call that is not an application's uses the platform's key
	if _, err := provider.Run(context.Background(), messages, config); err != nil || seen != "platform-key" {
		t.Fatalf("got %v and %q", err, seen)
	}
}

func TestOpenAIIsCalledWithTheApplicationsKey(t *testing.T) {
	seen := ""
	handler := func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"model":"gpt-5","choices":[{"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}]}`)
	}
	messages := []llm.LLMMessage{{Role: "user", Content: "Hello"}}
	config := llm.LLMModelConfig{ModelName: "gpt-5"}
	ctx := WithApplicationKeys(context.Background(), keysOf(map[string]string{"openai": "application-key"}))

	builtin := newTestOpenAIProvider(t, handler)
	if _, err := builtin.Run(ctx, messages, config); err != nil {
		t.Fatal(err)
	}
	if seen != "Bearer application-key" {
		t.Fatalf("called with %q", seen)
	}

	// a registered model keeps the credential it was registered with
	server := httptest.NewServer(http.HandlerFunc(handler))
	t.Cleanup(server.Close)
	registered, err := NewProviderForAdapter("openai", "openai", server.URL, func() (string, error) { return "registered-key", nil })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registered.Run(ctx, messages, config); err != nil {
		t.Fatal(err)
	}
	if seen != "Bearer registered-key" {
		t.Fatalf("called with %q", seen)
	}

	if _, err := builtin.Run(WithApplicationKeys(context.Background(), keysOf(nil)), messages, config); err == nil {
		t.Fatal("expected an error for an application without an OpenAI key")
	}
}
