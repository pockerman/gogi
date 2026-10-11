package impl

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"gogi/gogi/utils"
	"net/http"
	"strconv"
	"time"

	gogiv1 "gogi/gogi/gogi/v1"

	log "github.com/sirupsen/logrus"
)

// Config configures the observability service
type Config struct {
	// a service is degraded, or unhealthy, from these shares of failed spans
	DegradedErrorRate  float64
	UnhealthyErrorRate float64
	// how long log events, metric points and traces are kept
	LogRetention    time.Duration
	MetricRetention time.Duration
	TraceRetention  time.Duration
	// how often budgets are checked for webhook notifications
	BudgetCheckInterval time.Duration
	// the price file that overrides and extends the default prices
	PriceFile string
}

func DefaultConfig() Config {
	return Config{DegradedErrorRate: 0.05, UnhealthyErrorRate: 0.25, LogRetention: 30 * 24 * time.Hour,
		MetricRetention: 90 * 24 * time.Hour, TraceRetention: 90 * 24 * time.Hour, BudgetCheckInterval: 5 * time.Minute}
}

// ConfigFromEnv reads the configuration from the environment:
//
//	GOGI_OBSERVABILITY_DEGRADED_ERROR_RATE   share of failed spans from which a service is degraded (0.05)
//	GOGI_OBSERVABILITY_UNHEALTHY_ERROR_RATE  ... unhealthy (0.25)
//	GOGI_OBSERVABILITY_LOG_RETENTION         how long log events are kept, a Go duration (720h)
//	GOGI_OBSERVABILITY_METRIC_RETENTION      ... metric points (2160h)
//	GOGI_OBSERVABILITY_TRACE_RETENTION       ... traces, with their spans and scores (2160h)
//	GOGI_OBSERVABILITY_BUDGET_CHECK_INTERVAL how often budgets are checked for notifications (5m)
//	GOGI_MODEL_PRICES_FILE                   a YAML price file
func ConfigFromEnv() (Config, error) {
	config := DefaultConfig()
	for name, value := range map[string]*float64{
		"GOGI_OBSERVABILITY_DEGRADED_ERROR_RATE":  &config.DegradedErrorRate,
		"GOGI_OBSERVABILITY_UNHEALTHY_ERROR_RATE": &config.UnhealthyErrorRate,
	} {
		parsed, err := strconv.ParseFloat(utils.GetEnv(name, strconv.FormatFloat(*value, 'f', -1, 64)), 64)
		if err != nil || parsed <= 0 || parsed > 1 {
			return config, fmt.Errorf("invalid %s: must be above 0 and at most 1", name)
		}
		*value = parsed
	}
	if config.DegradedErrorRate > config.UnhealthyErrorRate {
		return config, fmt.Errorf("the degraded error rate must not be above the unhealthy one")
	}
	for name, value := range map[string]*time.Duration{
		"GOGI_OBSERVABILITY_LOG_RETENTION":         &config.LogRetention,
		"GOGI_OBSERVABILITY_METRIC_RETENTION":      &config.MetricRetention,
		"GOGI_OBSERVABILITY_TRACE_RETENTION":       &config.TraceRetention,
		"GOGI_OBSERVABILITY_BUDGET_CHECK_INTERVAL": &config.BudgetCheckInterval,
	} {
		parsed, err := time.ParseDuration(utils.GetEnv(name, value.String()))
		if err != nil || parsed <= 0 {
			return config, fmt.Errorf("invalid %s: must be a positive duration", name)
		}
		*value = parsed
	}
	config.PriceFile = utils.GetEnv("GOGI_MODEL_PRICES_FILE", "")
	return config, nil
}

// budgetNotification is the JSON posted to a budget's webhook. text makes it readable as a
// Slack message
type budgetNotification struct {
	Text              string  `json:"text"`
	Budget            string  `json:"budget"`
	Threshold         float64 `json:"threshold"`
	State             string  `json:"state"`
	SpentUSD          float64 `json:"spent_usd"`
	LimitUSD          float64 `json:"limit_usd"`
	Fraction          float64 `json:"fraction"`
	ProjectedUSD      float64 `json:"projected_usd"`
	ProjectedFraction float64 `json:"projected_fraction"`
	PeriodStart       int64   `json:"period_start"`
	PeriodEnd         int64   `json:"period_end"`
}

// CheckBudgets notifies the webhook of every budget whose spend crossed a threshold not yet
// notified in the current period. A notification that fails is sent again next time
func (s *ObservabilityServer) CheckBudgets(ctx context.Context, client *http.Client) {
	alerts, err := s.store.ListBudgets(ctx)
	if err != nil {
		log.Warnf("failed to list the budget alerts: %v", err)
		return
	}
	for _, alert := range alerts {
		if alert.GetWebhookUrl() == "" {
			continue
		}
		budget, err := s.budgetStatus(ctx, alert)
		if err != nil {
			log.Warnf("failed to compute the spend of budget %s: %v", alert.GetName(), err)
			continue
		}
		periodStart := time.UnixMilli(budget.GetPeriodStart()).UTC()
		for _, threshold := range budget.GetCrossedThresholds() {
			first, err := s.store.MarkNotified(ctx, alert.GetName(), periodStart, threshold)
			if err != nil || !first {
				continue
			}
			if err := notify(ctx, client, budget, threshold); err != nil {
				log.Warnf("failed to notify budget %s at %.0f%%, will retry: %v", alert.GetName(), threshold*100, err)
				s.store.UnmarkNotified(ctx, alert.GetName(), periodStart, threshold)
				continue
			}
			log.Infof("notified budget %s at %.0f%% of its limit", alert.GetName(), threshold*100)
		}
	}
}

func notify(ctx context.Context, client *http.Client, budget *gogiv1.BudgetStatus, threshold float64) error {
	alert := budget.GetAlert()
	payload, err := json.Marshal(budgetNotification{
		Text: fmt.Sprintf("Budget %s has reached %.0f%% of its %s limit: $%.2f of $%.2f spent, $%.2f projected by the end of the period.",
			alert.GetName(), threshold*100, alert.GetPeriod(), budget.GetSpentUsd(), alert.GetLimitUsd(), budget.GetProjectedUsd()),
		Budget: alert.GetName(), Threshold: threshold, State: budget.GetState(), SpentUSD: budget.GetSpentUsd(),
		LimitUSD: alert.GetLimitUsd(), Fraction: budget.GetFraction(), ProjectedUSD: budget.GetProjectedUsd(),
		ProjectedFraction: budget.GetProjectedFraction(), PeriodStart: budget.GetPeriodStart(), PeriodEnd: budget.GetPeriodEnd(),
	})
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, alert.GetWebhookUrl(), bytes.NewReader(payload))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("the webhook answered %s", response.Status)
	}
	return nil
}

// ApplyRetention deletes the telemetry older than the retention periods
func (s *ObservabilityServer) ApplyRetention(ctx context.Context) {
	now := s.now()
	deleted, err := s.store.DeleteOlderThan(ctx, now.Add(-s.config.LogRetention), now.Add(-s.config.MetricRetention),
		now.Add(-s.config.TraceRetention))
	if err != nil {
		log.Warnf("failed to apply the retention periods: %v", err)
		return
	}
	for kind, count := range deleted {
		if count > 0 {
			log.Infof("retention: deleted %d %s", count, kind)
		}
	}
}

// RunBackground checks the budgets and applies the retention periods until ctx is done
func (s *ObservabilityServer) RunBackground(ctx context.Context) {
	client := &http.Client{Timeout: 10 * time.Second}
	budgets := time.NewTicker(s.config.BudgetCheckInterval)
	retention := time.NewTicker(time.Hour)
	defer budgets.Stop()
	defer retention.Stop()

	s.ApplyRetention(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-budgets.C:
			s.CheckBudgets(ctx, client)
		case <-retention.C:
			s.ApplyRetention(ctx)
		}
	}
}
