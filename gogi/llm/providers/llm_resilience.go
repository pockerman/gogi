package providers

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"

	gogiv1 "gogi/gogi/gogi/v1"
	"gogi/gogi/utils"

	log "github.com/sirupsen/logrus"
)

// maxFallbackModels is the most models a request may fall back to
const maxFallbackModels = 5

var (
	// ErrCircuitOpen is returned for a model that failed too often recently: it is not
	// called until its circuit breaker's recovery timeout elapses
	ErrCircuitOpen = errors.New("circuit open")
	// errStalled is the cause of a call cancelled because its provider sent nothing
	errStalled = errors.New("the provider stalled")
)

// ProviderError is an error returned by a provider: StatusCode is the HTTP status of
// the provider's response, or 0 if there was no response, e.g. on a network error
type ProviderError struct {
	StatusCode int
	// RetryAfter is how long the provider asked to wait before retrying; 0 if it did not say
	RetryAfter time.Duration
	Err        error
}

func (e *ProviderError) Error() string {
	return e.Err.Error()
}

func (e *ProviderError) Unwrap() error {
	return e.Err
}

// IsRetryable returns true if err may go away if the call is retried: rate limits,
// overloaded or failing servers, timeouts and network errors
func IsRetryable(err error) bool {
	var providerErr *ProviderError
	if !errors.As(err, &providerErr) {
		return false
	}

	switch code := providerErr.StatusCode; {
	case code == 0:
		return true
	case code == http.StatusRequestTimeout, code == http.StatusConflict, code == http.StatusTooManyRequests:
		return true
	default:
		return code >= http.StatusInternalServerError
	}
}

// canFallBack returns true if another model may succeed where the failed one did not.
// An invalid request fails with every model
func canFallBack(err error) bool {
	return !errors.Is(err, ErrInvalidRequest)
}

// retryAfter returns how long the provider asked to wait before retrying, or 0
func retryAfter(err error) time.Duration {
	var providerErr *ProviderError
	if errors.As(err, &providerErr) {
		return providerErr.RetryAfter
	}
	return 0
}

// parseRetryAfter reads the retry-after-ms header, which OpenAI and Anthropic
// send, or the standard Retry-After header, in seconds or as an HTTP date
func parseRetryAfter(header http.Header) time.Duration {
	if ms, err := strconv.ParseFloat(header.Get("retry-after-ms"), 64); err == nil && ms > 0 {
		return time.Duration(ms * float64(time.Millisecond))
	}

	value := header.Get("Retry-After")
	if seconds, err := strconv.ParseFloat(value, 64); err == nil && seconds > 0 {
		return time.Duration(seconds * float64(time.Second))
	}
	if date, err := http.ParseTime(value); err == nil {
		return max(time.Until(date), 0)
	}
	return 0
}

// FallbackModel is a model to fall back to
type FallbackModel struct {
	Provider string
	Model    string
}

func (m FallbackModel) String() string {
	return m.Provider + "/" + m.Model
}

// ResilienceConfig says how the router retries failed calls and which models it
// falls back to if a request does not name any
type ResilienceConfig struct {
	// Retry is the default retry configuration; requests may lower, but not raise, its values
	Retry utils.RetryConfig
	// Fallback are the models tried, in order, if a request's model fails; empty
	// means requests that do not name models to fall back to only retry
	Fallback []FallbackModel
	// StallTimeout is how long a provider may send nothing before the call is given
	// up as stalled, and retried; 0 means calls are not watched
	StallTimeout time.Duration
}

func DefaultResilienceConfig() ResilienceConfig {
	return ResilienceConfig{Retry: utils.DefaultRetryConfig(), StallTimeout: 90 * time.Second}
}

// ResilienceConfigFromEnv reads the resilience configuration from the environment:
// GOGI_LLM_MAX_RETRIES, GOGI_LLM_RETRY_INITIAL_DELAY, GOGI_LLM_RETRY_MAX_DELAY and
// GOGI_LLM_STALL_TIMEOUT (Go durations), and GOGI_LLM_FALLBACK_MODELS, a
// comma-separated list of provider/model
func ResilienceConfigFromEnv() (ResilienceConfig, error) {
	config := DefaultResilienceConfig()

	maxRetries, err := strconv.Atoi(utils.GetEnv("GOGI_LLM_MAX_RETRIES", strconv.Itoa(config.Retry.MaxRetries)))
	if err != nil || maxRetries < 0 {
		return config, fmt.Errorf("invalid GOGI_LLM_MAX_RETRIES: must be a non-negative integer")
	}
	config.Retry.MaxRetries = maxRetries

	durations := map[string]*time.Duration{
		"GOGI_LLM_RETRY_INITIAL_DELAY": &config.Retry.InitialDelay,
		"GOGI_LLM_RETRY_MAX_DELAY":     &config.Retry.MaxDelay,
		"GOGI_LLM_STALL_TIMEOUT":       &config.StallTimeout,
	}
	for name, value := range durations {
		parsed, err := time.ParseDuration(utils.GetEnv(name, value.String()))
		if err != nil || parsed <= 0 {
			return config, fmt.Errorf("invalid %s: must be a positive duration", name)
		}
		*value = parsed
	}

	config.Fallback, err = parseFallbackModels(utils.GetEnv("GOGI_LLM_FALLBACK_MODELS", ""))
	if err != nil {
		return config, fmt.Errorf("invalid GOGI_LLM_FALLBACK_MODELS: %w", err)
	}
	return config, nil
}

// parseFallbackModels parses a comma-separated list of provider/model. Model names
// may contain slashes, e.g. meta-llama/Llama-3.1-8B, provider names may not
func parseFallbackModels(value string) ([]FallbackModel, error) {
	models := make([]FallbackModel, 0)
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		provider, model, ok := strings.Cut(item, "/")
		if !ok || provider == "" || model == "" {
			return nil, fmt.Errorf("%q is not provider/model", item)
		}
		models = append(models, FallbackModel{Provider: provider, Model: model})
	}
	if len(models) > maxFallbackModels {
		return nil, fmt.Errorf("at most %d models can be given", maxFallbackModels)
	}
	return models, nil
}

// FallbackError is returned when the requested model and every model it falls back to
// fail. It unwraps to the error of the last model tried
type FallbackError struct {
	// Errors are the errors of the models tried, in order
	Errors []error
}

func (e *FallbackError) Error() string {
	messages := make([]string, 0, len(e.Errors))
	for _, err := range e.Errors {
		messages = append(messages, err.Error())
	}
	return fmt.Sprintf("every model tried failed: %s", strings.Join(messages, "; "))
}

func (e *FallbackError) Unwrap() error {
	return e.Errors[len(e.Errors)-1]
}

// plan returns the models to try for the request, in order, starting with the
// requested model, and how failed calls to each of them are retried
func (p *LLMProviderRouter) plan(req *gogiv1.LLMRunRequest) ([]FallbackModel, utils.RetryConfig, error) {

	p.mu.RLock()
	config := p.resilience
	p.mu.RUnlock()

	requested := req.GetConfig()
	fallback := requested.GetFallback()

	retry := config.Retry
	if requestRetry := fallback.GetRetry(); requestRetry != nil {
		if requestRetry.MaxRetries != nil {
			retry.MaxRetries = min(max(int(requestRetry.GetMaxRetries()), 0), retry.MaxRetries)
		}
		if requestRetry.InitialDelayMs != nil {
			retry.InitialDelay = min(max(time.Duration(requestRetry.GetInitialDelayMs())*time.Millisecond, 0),
				retry.InitialDelay)
		}
		if requestRetry.MaxDelayMs != nil {
			retry.MaxDelay = min(max(time.Duration(requestRetry.GetMaxDelayMs())*time.Millisecond, 0),
				retry.MaxDelay)
		}
	}

	models := []FallbackModel{{Provider: requested.GetProvider(), Model: requested.GetModel()}}
	if fallback != nil && fallback.Enabled != nil && !fallback.GetEnabled() {
		return models, retry, nil
	}

	others := config.Fallback
	if len(fallback.GetModels()) > 0 {
		if len(fallback.GetModels()) > maxFallbackModels {
			return nil, retry, invalidRequest("at most %d models to fall back to can be given", maxFallbackModels)
		}
		others = make([]FallbackModel, 0, len(fallback.GetModels()))
		for i, model := range fallback.GetModels() {
			if model.GetProvider() == "" {
				return nil, retry, invalidRequest("model %d to fall back to has no provider", i)
			}
			others = append(others, FallbackModel{Provider: model.GetProvider(), Model: model.GetModel()})
		}
	}

	for _, model := range others {
		if !containsModel(models, model) {
			models = append(models, model)
		}
	}
	return models, retry, nil
}

func containsModel(models []FallbackModel, model FallbackModel) bool {
	for _, m := range models {
		if m == model {
			return true
		}
	}
	return false
}

// callWithFallback calls the requested model and, if it fails, the models to fall
// back to, in order, until one succeeds. Calls are retried as the request says, and
// models whose circuit is open are skipped. call must make the call with the given
// context. started, if not nil, returns true once part of the response reached the
// caller; the call then cannot be retried or answered by another model
func (p *LLMProviderRouter) callWithFallback(ctx context.Context, req *gogiv1.LLMRunRequest,
	call func(ctx context.Context, provider ModelProvider, model FallbackModel) error, started func() bool) error {

	models, retry, err := p.plan(req)
	if err != nil {
		return err
	}
	if started == nil {
		started = func() bool { return false }
	}

	errs := make([]error, 0, len(models))
	for i, model := range models {
		provider, err := p.findProvider(model.Provider, model.Model)
		if err != nil {
			// the requested model must be one the platform knows
			if i == 0 {
				return err
			}
			log.Warnf("cannot fall back to %s: %v", model, err)
			errs = append(errs, fmt.Errorf("%s: %w", model, err))
			continue
		}

		if p.breaker != nil && !p.breaker.AllowRequest(model.String()) {
			err = fmt.Errorf("%w for %s: it failed too often recently", ErrCircuitOpen, model)
		} else {
			err = p.callWithRetries(ctx, model, retry, func(ctx context.Context) error {
				return call(ctx, provider, model)
			}, started)
			// a model that answers, even with an error such as an invalid request, is healthy;
			// a call the caller gave up on says nothing about the model
			if p.breaker != nil && ctx.Err() == nil {
				p.breaker.RecordResult(model.String(), !IsRetryable(err))
			}
		}

		if err == nil {
			if i > 0 {
				log.Infof("model %s failed, %s answered", models[0], model)
			}
			return nil
		}

		if len(models) == 1 {
			return err
		}
		errs = append(errs, fmt.Errorf("%s: %w", model, err))
		if ctx.Err() != nil || started() || !canFallBack(err) {
			break
		}
		if i < len(models)-1 {
			log.Warnf("model %s failed, falling back: %v", model, err)
		}
	}
	return &FallbackError{Errors: errs}
}

// callWithRetries calls the model, retrying failed calls as long as the error is
// retryable and no part of the response reached the caller
func (p *LLMProviderRouter) callWithRetries(ctx context.Context, model FallbackModel, retry utils.RetryConfig,
	call func(ctx context.Context) error, started func() bool) error {

	for attempt := 0; ; attempt++ {
		err := p.callWatched(ctx, model, call)
		if err == nil || attempt >= retry.MaxRetries || !IsRetryable(err) || started() || ctx.Err() != nil {
			return err
		}

		delay := p.jitter(retry.Delay(attempt))
		if after := retryAfter(err); after > 0 {
			// waiting that long would hold the caller up; better try another model
			if after > retry.MaxDelay {
				return err
			}
			delay = max(delay, after)
		}

		log.Warnf("call %d to model %s failed, retrying in %s: %v", attempt+1, model, delay, err)
		if p.sleep(ctx, delay) != nil {
			return err
		}
	}
}

// callWatched makes a call that is cancelled if the provider sends nothing for the
// stall timeout. A stalled call fails with a retryable error
func (p *LLMProviderRouter) callWatched(ctx context.Context, model FallbackModel, call func(ctx context.Context) error) error {

	p.mu.RLock()
	timeout := p.resilience.StallTimeout
	p.mu.RUnlock()

	if timeout <= 0 {
		return call(ctx)
	}

	callCtx, cancel := withStallTimeout(ctx, timeout)
	defer cancel()

	err := call(callCtx)
	if err != nil && ctx.Err() == nil && errors.Is(context.Cause(callCtx), errStalled) {
		return &ProviderError{Err: fmt.Errorf("model %s sent nothing for %s: %w", model, timeout, errStalled)}
	}
	return err
}

// stallWatchdog cancels a call whose provider sends nothing for its timeout
type stallWatchdog struct {
	timer   *time.Timer
	timeout time.Duration
}

type stallWatchdogKey struct{}

// withStallTimeout returns a context that is cancelled, with the cause errStalled, if
// touch is not called with it for timeout
func withStallTimeout(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancelCause(parent)
	watchdog := &stallWatchdog{timeout: timeout}
	watchdog.timer = time.AfterFunc(timeout, func() { cancel(errStalled) })

	return context.WithValue(ctx, stallWatchdogKey{}, watchdog), func() {
		watchdog.timer.Stop()
		cancel(context.Canceled)
	}
}

// touch tells the stall watchdog of the call, if any, that the provider sent something
func touch(ctx context.Context) {
	if watchdog, ok := ctx.Value(stallWatchdogKey{}).(*stallWatchdog); ok {
		watchdog.timer.Reset(watchdog.timeout)
	}
}

// equalJitter returns a random delay between delay/2 and delay, so that callers
// that failed at the same time do not all retry at the same time
func equalJitter(delay time.Duration) time.Duration {
	if delay <= 0 {
		return 0
	}
	half := delay / 2
	return half + rand.N(delay-half+1)
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
