package impl

import (
	"fmt"
	"gogi/gogi/sessions"
	"gogi/gogi/utils"
	"strconv"
	"time"
)

// SummaryConfig says which model writes summaries; an empty model means no summaries
type SummaryConfig struct {
	Provider string
	Model    string
	Timeout  time.Duration
}

// ConfigFromEnv reads the configuration of the session service from the environment:
//
//	GOGI_SESSIONS_DEFAULT_TOKEN_BUDGET  the budget of a context strategy when a request gives none (8000)
//	GOGI_SESSIONS_CHARS_PER_TOKEN       characters per estimated token (4)
//	GOGI_SESSIONS_MEMORY_SHARE          the share of the budget memories may take (0.1)
//	GOGI_SESSIONS_SUMMARY_SHARE         the share of the budget the summary may take (0.2)
//	GOGI_SESSIONS_SUMMARY_CHUNK_TOKENS  the most tokens of messages summarized in one call (6000)
//	GOGI_SESSIONS_SUMMARY_PROVIDER      the provider of the summary model, e.g. openai
//	GOGI_SESSIONS_SUMMARY_MODEL         the summary model, e.g. gpt-4o-mini; empty means no summaries
//	GOGI_SESSIONS_SUMMARY_TIMEOUT       how long a summary may take, a Go duration (60s)
func ConfigFromEnv() (Config, *sessions.Assembler, SummaryConfig, error) {
	config := DefaultConfig()
	assembler := &sessions.Assembler{Estimator: sessions.DefaultEstimator(), MemoryShare: 0.1, SummaryShare: 0.2,
		ChunkTokens: 6000}
	summary := SummaryConfig{
		Provider: utils.GetEnv("GOGI_SESSIONS_SUMMARY_PROVIDER", ""),
		Model:    utils.GetEnv("GOGI_SESSIONS_SUMMARY_MODEL", ""),
		Timeout:  60 * time.Second,
	}

	integers := map[string]*int{
		"GOGI_SESSIONS_DEFAULT_TOKEN_BUDGET": &config.DefaultTokenBudget,
		"GOGI_SESSIONS_SUMMARY_CHUNK_TOKENS": &assembler.ChunkTokens,
	}
	for name, value := range integers {
		parsed, err := strconv.Atoi(utils.GetEnv(name, strconv.Itoa(*value)))
		if err != nil || parsed <= 0 {
			return config, nil, summary, fmt.Errorf("invalid %s: must be a positive integer", name)
		}
		*value = parsed
	}

	floats := map[string]*float64{
		"GOGI_SESSIONS_CHARS_PER_TOKEN": &assembler.Estimator.CharsPerToken,
		"GOGI_SESSIONS_MEMORY_SHARE":    &assembler.MemoryShare,
		"GOGI_SESSIONS_SUMMARY_SHARE":   &assembler.SummaryShare,
	}
	for name, value := range floats {
		parsed, err := strconv.ParseFloat(utils.GetEnv(name, strconv.FormatFloat(*value, 'f', -1, 64)), 64)
		if err != nil || parsed <= 0 {
			return config, nil, summary, fmt.Errorf("invalid %s: must be a positive number", name)
		}
		*value = parsed
	}
	if assembler.MemoryShare+assembler.SummaryShare >= 1 {
		return config, nil, summary, fmt.Errorf("GOGI_SESSIONS_MEMORY_SHARE and GOGI_SESSIONS_SUMMARY_SHARE " +
			"must leave a share for the recent messages")
	}

	timeout, err := time.ParseDuration(utils.GetEnv("GOGI_SESSIONS_SUMMARY_TIMEOUT", summary.Timeout.String()))
	if err != nil || timeout <= 0 {
		return config, nil, summary, fmt.Errorf("invalid GOGI_SESSIONS_SUMMARY_TIMEOUT: must be a positive duration")
	}
	summary.Timeout = timeout
	if summary.Model != "" && summary.Provider == "" {
		return config, nil, summary, fmt.Errorf("GOGI_SESSIONS_SUMMARY_MODEL needs GOGI_SESSIONS_SUMMARY_PROVIDER")
	}
	return config, assembler, summary, nil
}
