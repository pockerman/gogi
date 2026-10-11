package providers

import (
	"context"
	"errors"
	"fmt"
	gogiv1 "gogi/gogi/gogi/v1"
	"gogi/gogi/llm"
	"gogi/gogi/utils"
	"net/http"
	"slices"
	"testing"
	"time"

	"google.golang.org/grpc"
)

// scriptedProvider fails its first calls with the given errors, then answers
type scriptedProvider struct {
	name   string
	errs   []error
	models []string
	// sendFirst makes RunStream send a chunk before failing
	sendFirst bool
}

func (p *scriptedProvider) next(model string) error {
	p.models = append(p.models, model)
	if len(p.errs) == 0 {
		return nil
	}
	err := p.errs[0]
	p.errs = p.errs[1:]
	return err
}

func (p *scriptedProvider) Run(ctx context.Context, messages []llm.LLMMessage, config llm.LLMModelConfig) (llm.LLModelResponse, error) {
	if err := p.next(config.ModelName); err != nil {
		return llm.LLModelResponse{}, err
	}
	return llm.LLModelResponse{Provider: p.name, Model: config.ModelName}, nil
}

func (p *scriptedProvider) RunStream(messages []llm.LLMMessage, config llm.LLMModelConfig,
	stream grpc.ServerStreamingServer[gogiv1.LLMStreamChunkResponse]) error {
	if p.sendFirst {
		if err := stream.Send(&gogiv1.LLMStreamChunkResponse{Token: "partial"}); err != nil {
			return err
		}
	}
	if err := p.next(config.ModelName); err != nil {
		return err
	}
	return stream.Send(&gogiv1.LLMStreamChunkResponse{Token: p.name, Model: config.ModelName})
}

func (p *scriptedProvider) EstimateCost(tokens []string, model string) (float64, error) {
	return 0, nil
}

func statusError(code int) error {
	return &ProviderError{StatusCode: code, Err: fmt.Errorf("API error (status %d)", code)}
}

// newTestRouter returns a router that records its waits instead of waiting
func newTestRouter(config ResilienceConfig, providers map[string]ModelProvider) (*LLMProviderRouter, *[]time.Duration) {
	router := NewLLMProviderRouter(providers)
	router.SetResilienceConfig(config)

	waits := make([]time.Duration, 0)
	router.sleep = func(ctx context.Context, d time.Duration) error {
		waits = append(waits, d)
		return ctx.Err()
	}
	router.jitter = func(d time.Duration) time.Duration { return d }
	return router, &waits
}

func testResilienceConfig(fallback ...FallbackModel) ResilienceConfig {
	return ResilienceConfig{
		Retry:    utils.RetryConfig{MaxRetries: 2, InitialDelay: 100 * time.Millisecond, MaxDelay: time.Second},
		Fallback: fallback,
	}
}

func fallbackRequest(fallback *gogiv1.FallbackConfig) *gogiv1.LLMRunRequest {
	request := runRequest("anthropic", "claude-sonnet-5-5")
	request.Config.Fallback = fallback
	return request
}

func toGPT4o() *gogiv1.FallbackConfig {
	return &gogiv1.FallbackConfig{Models: []*gogiv1.FallbackModel{{Provider: "openai", Model: "gpt-4o"}}}
}

func TestRetryConfigDelay(t *testing.T) {
	config := utils.RetryConfig{MaxRetries: 5, InitialDelay: 100 * time.Millisecond, MaxDelay: 300 * time.Millisecond}

	expected := []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 300 * time.Millisecond,
		300 * time.Millisecond}
	for retry, delay := range expected {
		if got := config.Delay(retry); got != delay {
			t.Errorf("retry %d: expected %s, got %s", retry, delay, got)
		}
	}
}

func TestIsRetryable(t *testing.T) {
	cases := map[error]bool{
		statusError(429):                            true,
		statusError(500):                            true,
		statusError(529):                            true,
		statusError(408):                            true,
		&ProviderError{Err: errors.New("reset")}:    true,
		statusError(400):                            false,
		statusError(401):                            false,
		statusError(404):                            false,
		invalidRequest("bad"):                       false,
		errors.New("decode failed"):                 false,
		fmt.Errorf("wrapped: %w", statusError(503)): true,
	}
	for err, retryable := range cases {
		if IsRetryable(err) != retryable {
			t.Errorf("IsRetryable(%v) should be %v", err, retryable)
		}
	}
}

func TestParseRetryAfter(t *testing.T) {
	cases := []struct {
		header   http.Header
		expected time.Duration
	}{
		{http.Header{"Retry-After": {"3"}}, 3 * time.Second},
		{http.Header{"Retry-After-Ms": {"1500"}, "Retry-After": {"2"}}, 1500 * time.Millisecond},
		{http.Header{"Retry-After": {"soon"}}, 0},
		{http.Header{}, 0},
	}
	for _, c := range cases {
		if got := parseRetryAfter(c.header); got != c.expected {
			t.Errorf("parseRetryAfter(%v): expected %s, got %s", c.header, c.expected, got)
		}
	}

	date := http.Header{"Retry-After": {time.Now().Add(time.Hour).UTC().Format(http.TimeFormat)}}
	if got := parseRetryAfter(date); got < 59*time.Minute || got > time.Hour {
		t.Errorf("wrong delay for an HTTP date: %s", got)
	}
}

func TestParseFallbackModels(t *testing.T) {
	models, err := parseFallbackModels(" openai/gpt-4o, together/meta-llama/Llama-3.1-8B ,")
	expected := []FallbackModel{{"openai", "gpt-4o"}, {"together", "meta-llama/Llama-3.1-8B"}}
	if err != nil || !slices.Equal(models, expected) {
		t.Errorf("wrong models %v, %v", models, err)
	}

	if models, err := parseFallbackModels(""); err != nil || len(models) != 0 {
		t.Errorf("an empty list should give no models, got %v, %v", models, err)
	}
	for _, invalid := range []string{"gpt-4o", "/gpt-4o", "openai/", "a/1,b/2,c/3,d/4,e/5,f/6"} {
		if _, err := parseFallbackModels(invalid); err == nil {
			t.Errorf("expected an error for %q", invalid)
		}
	}
}

func TestResilienceConfigFromEnv(t *testing.T) {
	t.Setenv("GOGI_LLM_MAX_RETRIES", "4")
	t.Setenv("GOGI_LLM_RETRY_MAX_DELAY", "5s")
	t.Setenv("GOGI_LLM_FALLBACK_MODELS", "openai/gpt-4o")

	config, err := ResilienceConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if config.Retry.MaxRetries != 4 || config.Retry.MaxDelay != 5*time.Second ||
		config.Retry.InitialDelay != utils.DefaultRetryConfig().InitialDelay ||
		!slices.Equal(config.Fallback, []FallbackModel{{"openai", "gpt-4o"}}) {
		t.Errorf("wrong config %+v", config)
	}

	t.Setenv("GOGI_LLM_MAX_RETRIES", "-1")
	if _, err := ResilienceConfigFromEnv(); err == nil {
		t.Errorf("expected an error for negative retries")
	}
}

func TestRouterRetriesRetryableErrors(t *testing.T) {
	anthropic := &scriptedProvider{name: "anthropic", errs: []error{statusError(529), statusError(503)}}
	router, waits := newTestRouter(testResilienceConfig(), map[string]ModelProvider{"anthropic": anthropic})

	response, err := router.Run(context.Background(), runRequest("anthropic", "claude-sonnet-5-5"))
	if err != nil || response.Provider != "anthropic" {
		t.Fatalf("wrong response %+v, %v", response, err)
	}
	if len(anthropic.models) != 3 {
		t.Errorf("expected 3 calls, got %d", len(anthropic.models))
	}
	if !slices.Equal(*waits, []time.Duration{100 * time.Millisecond, 200 * time.Millisecond}) {
		t.Errorf("wrong waits %v", *waits)
	}
}

func TestRouterGivesUpAfterMaxRetries(t *testing.T) {
	anthropic := &scriptedProvider{name: "anthropic",
		errs: []error{statusError(503), statusError(503), statusError(503), statusError(503)}}
	router, _ := newTestRouter(testResilienceConfig(), map[string]ModelProvider{"anthropic": anthropic})

	_, err := router.Run(context.Background(), runRequest("anthropic", "claude-sonnet-5-5"))
	var providerErr *ProviderError
	if !errors.As(err, &providerErr) || providerErr.StatusCode != 503 {
		t.Errorf("expected the provider's error, got %v", err)
	}
	if len(anthropic.models) != 3 {
		t.Errorf("expected 1 call and 2 retries, got %d calls", len(anthropic.models))
	}
}

func TestRouterDoesNotRetryOtherErrors(t *testing.T) {
	anthropic := &scriptedProvider{name: "anthropic", errs: []error{statusError(401)}}
	router, waits := newTestRouter(testResilienceConfig(), map[string]ModelProvider{"anthropic": anthropic})

	if _, err := router.Run(context.Background(), runRequest("anthropic", "claude-sonnet-5-5")); err == nil {
		t.Fatal("expected an error")
	}
	if len(anthropic.models) != 1 || len(*waits) != 0 {
		t.Errorf("a 401 should not be retried, got %d calls", len(anthropic.models))
	}
}

func TestRouterFallsBackToRequestedModels(t *testing.T) {
	anthropic := &scriptedProvider{name: "anthropic", errs: []error{statusError(401)}}
	openai := &scriptedProvider{name: "openai"}
	router, _ := newTestRouter(testResilienceConfig(FallbackModel{"openai", "gpt-4o-mini"}),
		map[string]ModelProvider{"anthropic": anthropic, "openai": openai})

	response, err := router.Run(context.Background(), fallbackRequest(toGPT4o()))
	if err != nil || response.Provider != "openai" || response.Model != "gpt-4o" {
		t.Fatalf("expected gpt-4o to answer, got %+v, %v", response, err)
	}
	// the request's models replace the default ones
	if !slices.Equal(openai.models, []string{"gpt-4o"}) {
		t.Errorf("wrong models called %v", openai.models)
	}
}

func TestRouterFallsBackAfterRetries(t *testing.T) {
	anthropic := &scriptedProvider{name: "anthropic",
		errs: []error{statusError(503), statusError(503), statusError(503)}}
	openai := &scriptedProvider{name: "openai"}
	router, _ := newTestRouter(testResilienceConfig(),
		map[string]ModelProvider{"anthropic": anthropic, "openai": openai})

	response, err := router.Run(context.Background(), fallbackRequest(toGPT4o()))
	if err != nil || response.Provider != "openai" {
		t.Fatalf("expected openai to answer, got %+v, %v", response, err)
	}
	if len(anthropic.models) != 3 {
		t.Errorf("the model should be retried before falling back, got %d calls", len(anthropic.models))
	}
}

func TestRouterDefaultFallback(t *testing.T) {
	providers := func() (*scriptedProvider, *scriptedProvider, map[string]ModelProvider) {
		anthropic := &scriptedProvider{name: "anthropic", errs: []error{statusError(400)}}
		openai := &scriptedProvider{name: "openai"}
		return anthropic, openai, map[string]ModelProvider{"anthropic": anthropic, "openai": openai}
	}

	// the default models are used when the request names none
	_, openai, all := providers()
	router, _ := newTestRouter(testResilienceConfig(FallbackModel{"openai", "gpt-4o-mini"}), all)
	response, err := router.Run(context.Background(), fallbackRequest(nil))
	if err != nil || response.Model != "gpt-4o-mini" || !slices.Equal(openai.models, []string{"gpt-4o-mini"}) {
		t.Errorf("expected the default model to answer, got %+v, %v", response, err)
	}

	// enabled = false turns them off
	disabled := false
	_, openai, all = providers()
	router, _ = newTestRouter(testResilienceConfig(FallbackModel{"openai", "gpt-4o-mini"}), all)
	if _, err := router.Run(context.Background(), fallbackRequest(&gogiv1.FallbackConfig{Enabled: &disabled})); err == nil {
		t.Errorf("expected an error with fallback disabled")
	}
	if len(openai.models) != 0 {
		t.Errorf("no model should be fallen back to, got %v", openai.models)
	}

	// without default models, failed calls are not fallen back
	_, openai, all = providers()
	router, _ = newTestRouter(testResilienceConfig(), all)
	if _, err := router.Run(context.Background(), fallbackRequest(nil)); err == nil || len(openai.models) != 0 {
		t.Errorf("expected an error and no fallback, got %v, %v", err, openai.models)
	}
}

func TestRouterAllModelsFail(t *testing.T) {
	anthropic := &scriptedProvider{name: "anthropic", errs: []error{statusError(401)}}
	openai := &scriptedProvider{name: "openai", errs: []error{statusError(404)}}
	router, _ := newTestRouter(testResilienceConfig(),
		map[string]ModelProvider{"anthropic": anthropic, "openai": openai})

	_, err := router.Run(context.Background(), fallbackRequest(toGPT4o()))
	var fallbackErr *FallbackError
	if !errors.As(err, &fallbackErr) || len(fallbackErr.Errors) != 2 {
		t.Fatalf("expected a FallbackError for both models, got %v", err)
	}
	var providerErr *ProviderError
	if !errors.As(err, &providerErr) || providerErr.StatusCode != 404 {
		t.Errorf("the error should unwrap to the last model's error, got %v", err)
	}
}

func TestRouterSkipsUnknownFallbackModels(t *testing.T) {
	anthropic := &scriptedProvider{name: "anthropic", errs: []error{statusError(500)}}
	openai := &scriptedProvider{name: "openai"}
	router, _ := newTestRouter(ResilienceConfig{Retry: utils.RetryConfig{}},
		map[string]ModelProvider{"anthropic": anthropic, "openai": openai})

	request := fallbackRequest(&gogiv1.FallbackConfig{Models: []*gogiv1.FallbackModel{
		{Provider: "ollama", Model: "llama3.2"}, {Provider: "openai", Model: "gpt-4o"}}})
	response, err := router.Run(context.Background(), request)
	if err != nil || response.Provider != "openai" {
		t.Errorf("expected openai to answer, got %+v, %v", response, err)
	}

	// the requested model must be known
	request.Config.Provider = "ollama"
	if _, err := router.Run(context.Background(), request); !errors.Is(err, ErrUnknownProvider) {
		t.Errorf("expected ErrUnknownProvider, got %v", err)
	}
}

func TestRouterDoesNotFallBackOnInvalidRequests(t *testing.T) {
	anthropic := &scriptedProvider{name: "anthropic", errs: []error{invalidRequest("bad tool")}}
	openai := &scriptedProvider{name: "openai"}
	router, _ := newTestRouter(testResilienceConfig(),
		map[string]ModelProvider{"anthropic": anthropic, "openai": openai})

	_, err := router.Run(context.Background(), fallbackRequest(toGPT4o()))
	if !errors.Is(err, ErrInvalidRequest) || len(openai.models) != 0 {
		t.Errorf("an invalid request should not fall back, got %v, %v", err, openai.models)
	}

	// too many models to fall back to
	models := make([]*gogiv1.FallbackModel, maxFallbackModels+1)
	for i := range models {
		models[i] = &gogiv1.FallbackModel{Provider: "openai", Model: fmt.Sprintf("model-%d", i)}
	}
	_, err = router.Run(context.Background(), fallbackRequest(&gogiv1.FallbackConfig{Models: models}))
	if !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("expected ErrInvalidRequest, got %v", err)
	}
}

func TestRouterRetryAfter(t *testing.T) {
	rateLimited := func(after time.Duration) error {
		return &ProviderError{StatusCode: 429, RetryAfter: after, Err: errors.New("rate limited")}
	}

	// waits as long as the provider asks
	anthropic := &scriptedProvider{name: "anthropic", errs: []error{rateLimited(700 * time.Millisecond)}}
	router, waits := newTestRouter(testResilienceConfig(), map[string]ModelProvider{"anthropic": anthropic})
	if _, err := router.Run(context.Background(), fallbackRequest(nil)); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(*waits, []time.Duration{700 * time.Millisecond}) {
		t.Errorf("wrong waits %v", *waits)
	}

	// but falls back rather than wait longer than the longest delay
	anthropic = &scriptedProvider{name: "anthropic", errs: []error{rateLimited(time.Minute)}}
	openai := &scriptedProvider{name: "openai"}
	router, waits = newTestRouter(testResilienceConfig(),
		map[string]ModelProvider{"anthropic": anthropic, "openai": openai})
	response, err := router.Run(context.Background(), fallbackRequest(toGPT4o()))
	if err != nil || response.Provider != "openai" || len(*waits) != 0 {
		t.Errorf("expected openai to answer without waiting, got %+v, %v, %v", response, err, *waits)
	}
}

func TestRouterRequestRetryConfigIsCapped(t *testing.T) {
	retries := func(maxRetries int32) int {
		anthropic := &scriptedProvider{name: "anthropic", errs: slices.Repeat([]error{statusError(503)}, 10)}
		router, _ := newTestRouter(testResilienceConfig(), map[string]ModelProvider{"anthropic": anthropic})
		request := fallbackRequest(&gogiv1.FallbackConfig{Retry: &gogiv1.RetryConfig{MaxRetries: &maxRetries}})
		_, _ = router.Run(context.Background(), request)
		return len(anthropic.models) - 1
	}

	if got := retries(0); got != 0 {
		t.Errorf("max_retries 0 should not retry, got %d retries", got)
	}
	if got := retries(1); got != 1 {
		t.Errorf("expected 1 retry, got %d", got)
	}
	if got := retries(100); got != 2 {
		t.Errorf("retries should be capped at the platform's 2, got %d", got)
	}
}

func TestRouterStopsWhenTheCallerGoesAway(t *testing.T) {
	anthropic := &scriptedProvider{name: "anthropic", errs: []error{statusError(503), statusError(503)}}
	openai := &scriptedProvider{name: "openai"}
	router, _ := newTestRouter(testResilienceConfig(),
		map[string]ModelProvider{"anthropic": anthropic, "openai": openai})

	ctx, cancel := context.WithCancel(context.Background())
	router.sleep = func(ctx context.Context, d time.Duration) error {
		cancel()
		return ctx.Err()
	}

	if _, err := router.Run(ctx, fallbackRequest(toGPT4o())); err == nil {
		t.Fatal("expected an error")
	}
	if len(anthropic.models) != 1 || len(openai.models) != 0 {
		t.Errorf("no more calls should be made, got %v, %v", anthropic.models, openai.models)
	}
}

func TestRouterStreamFallsBackBeforeTheFirstChunk(t *testing.T) {
	anthropic := &scriptedProvider{name: "anthropic", errs: []error{statusError(401)}}
	openai := &scriptedProvider{name: "openai"}
	router, _ := newTestRouter(testResilienceConfig(),
		map[string]ModelProvider{"anthropic": anthropic, "openai": openai})

	stream := &fakeChunkStream{}
	if err := router.RunStream(fallbackRequest(toGPT4o()), stream); err != nil {
		t.Fatal(err)
	}
	if len(stream.chunks) != 1 || stream.chunks[0].Provider != "openai" || stream.chunks[0].Model != "gpt-4o" {
		t.Errorf("expected one chunk from openai, got %+v", stream.chunks)
	}
}

func TestRouterStreamDoesNotRetryAfterTheFirstChunk(t *testing.T) {
	anthropic := &scriptedProvider{name: "anthropic", errs: []error{statusError(503)}, sendFirst: true}
	openai := &scriptedProvider{name: "openai"}
	router, _ := newTestRouter(testResilienceConfig(),
		map[string]ModelProvider{"anthropic": anthropic, "openai": openai})

	stream := &fakeChunkStream{}
	if err := router.RunStream(fallbackRequest(toGPT4o()), stream); err == nil {
		t.Fatal("expected an error")
	}
	if len(anthropic.models) != 1 || len(openai.models) != 0 {
		t.Errorf("a started stream should not be retried, got %v, %v", anthropic.models, openai.models)
	}
	if len(stream.chunks) != 1 || stream.chunks[0].Provider != "anthropic" {
		t.Errorf("expected the partial chunk from anthropic, got %+v", stream.chunks)
	}
}

func TestEqualJitter(t *testing.T) {
	for i := 0; i < 100; i++ {
		if d := equalJitter(time.Second); d < 500*time.Millisecond || d > time.Second {
			t.Fatalf("jittered delay %s out of range", d)
		}
	}
	if equalJitter(0) != 0 {
		t.Errorf("no delay should stay no delay")
	}
}

// stallingProvider sends nothing, until the call is cancelled, for its first stalls
// calls; then it answers, sending something every beat for beats beats
type stallingProvider struct {
	stalls int
	beats  int
	beat   time.Duration
	calls  int
}

func (p *stallingProvider) call(ctx context.Context) error {
	p.calls++
	if p.stalls > 0 {
		p.stalls--
		<-ctx.Done()
		return &ProviderError{Err: fmt.Errorf("request failed: %w", ctx.Err())}
	}
	for range p.beats {
		select {
		case <-ctx.Done():
			return &ProviderError{Err: ctx.Err()}
		case <-time.After(p.beat):
			touch(ctx)
		}
	}
	return nil
}

func (p *stallingProvider) Run(ctx context.Context, messages []llm.LLMMessage,
	config llm.LLMModelConfig) (llm.LLModelResponse, error) {
	if err := p.call(ctx); err != nil {
		return llm.LLModelResponse{}, err
	}
	return llm.LLModelResponse{Provider: "slow", Model: config.ModelName}, nil
}

func (p *stallingProvider) RunStream(messages []llm.LLMMessage, config llm.LLMModelConfig,
	stream grpc.ServerStreamingServer[gogiv1.LLMStreamChunkResponse]) error {
	if err := p.call(stream.Context()); err != nil {
		return err
	}
	return stream.Send(&gogiv1.LLMStreamChunkResponse{Token: "done"})
}

func (p *stallingProvider) EstimateCost(tokens []string, model string) (float64, error) {
	return 0, nil
}

func stallConfig() ResilienceConfig {
	config := testResilienceConfig()
	config.StallTimeout = 30 * time.Millisecond
	return config
}

func TestRouterRetriesStalledCalls(t *testing.T) {
	slow := &stallingProvider{stalls: 1}
	router, _ := newTestRouter(stallConfig(), map[string]ModelProvider{"slow": slow})

	response, err := router.Run(context.Background(), runRequest("slow", "model"))
	if err != nil || response.Provider != "slow" || slow.calls != 2 {
		t.Errorf("expected the stalled call to be retried, got %+v, %v after %d calls", response, err, slow.calls)
	}

	// a stalled stream that sent nothing is retried as well
	slow = &stallingProvider{stalls: 1}
	router, _ = newTestRouter(stallConfig(), map[string]ModelProvider{"slow": slow})
	stream := &fakeChunkStream{}
	if err := router.RunStream(runRequest("slow", "model"), stream); err != nil || len(stream.chunks) != 1 {
		t.Errorf("expected the stalled stream to be retried, got %v, %+v", err, stream.chunks)
	}
}

func TestRouterStalledCallError(t *testing.T) {
	slow := &stallingProvider{stalls: 10}
	router, _ := newTestRouter(stallConfig(), map[string]ModelProvider{"slow": slow})

	_, err := router.Run(context.Background(), runRequest("slow", "model"))
	if !errors.Is(err, errStalled) || !IsRetryable(err) {
		t.Errorf("expected a retryable stall error, got %v", err)
	}
	if slow.calls != 3 {
		t.Errorf("expected 1 call and 2 retries, got %d calls", slow.calls)
	}
}

func TestRouterDoesNotStopCallsThatKeepSending(t *testing.T) {
	// takes 4 times the stall timeout, but sends something every third of it
	slow := &stallingProvider{beats: 12, beat: 10 * time.Millisecond}
	router, _ := newTestRouter(stallConfig(), map[string]ModelProvider{"slow": slow})

	if _, err := router.Run(context.Background(), runRequest("slow", "model")); err != nil || slow.calls != 1 {
		t.Errorf("a call that keeps sending should not be stopped, got %v after %d calls", err, slow.calls)
	}
}

func TestRouterCancelledCallIsNotAStall(t *testing.T) {
	slow := &stallingProvider{stalls: 1}
	router, _ := newTestRouter(stallConfig(), map[string]ModelProvider{"slow": slow})
	router.SetCircuitBreaker(utils.NewCircuitBreaker(1, time.Minute, 1))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()

	_, err := router.Run(ctx, runRequest("slow", "model"))
	if err == nil || errors.Is(err, errStalled) || slow.calls != 1 {
		t.Errorf("expected the caller's cancellation, got %v after %d calls", err, slow.calls)
	}
	if router.breaker.State("slow/model") != utils.CircuitClosed {
		t.Errorf("a call the caller gave up on should not count against the model")
	}
}

func TestRouterCircuitBreaker(t *testing.T) {
	anthropic := &scriptedProvider{name: "anthropic", errs: []error{statusError(503), statusError(503)}}
	openai := &scriptedProvider{name: "openai"}
	router, _ := newTestRouter(ResilienceConfig{Retry: utils.RetryConfig{}},
		map[string]ModelProvider{"anthropic": anthropic, "openai": openai})
	router.SetCircuitBreaker(utils.NewCircuitBreaker(2, time.Minute, 1))

	for range 2 {
		if _, err := router.Run(context.Background(), fallbackRequest(nil)); err == nil {
			t.Fatal("expected an error")
		}
	}

	// the circuit is open: the model is not called
	_, err := router.Run(context.Background(), fallbackRequest(nil))
	if !errors.Is(err, ErrCircuitOpen) || len(anthropic.models) != 2 {
		t.Errorf("expected ErrCircuitOpen without a call, got %v after %d calls", err, len(anthropic.models))
	}

	// and requests fall back straight away
	response, err := router.Run(context.Background(), fallbackRequest(toGPT4o()))
	if err != nil || response.Provider != "openai" || len(anthropic.models) != 2 {
		t.Errorf("expected openai to answer straight away, got %+v, %v", response, err)
	}

	// circuits are per model
	if _, err := router.Run(context.Background(), runRequest("anthropic", "claude-haiku-4-5")); err != nil {
		t.Errorf("another model of the provider should be called, got %v", err)
	}
}

func TestRouterCircuitBreakerIgnoresCallerErrors(t *testing.T) {
	anthropic := &scriptedProvider{name: "anthropic",
		errs: []error{statusError(401), statusError(400), invalidRequest("bad tool")}}
	router, _ := newTestRouter(ResilienceConfig{Retry: utils.RetryConfig{}},
		map[string]ModelProvider{"anthropic": anthropic})
	router.SetCircuitBreaker(utils.NewCircuitBreaker(2, time.Minute, 1))

	for range 3 {
		_, _ = router.Run(context.Background(), fallbackRequest(nil))
	}
	if router.breaker.State("anthropic/claude-sonnet-5-5") != utils.CircuitClosed {
		t.Errorf("a model that answers with an error is healthy")
	}
}
