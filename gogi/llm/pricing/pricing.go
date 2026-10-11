// Package pricing prices LLM calls from their token counts
package pricing

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Price is what a model charges, in USD per million tokens
type Price struct {
	InputPerMTok  float64 `yaml:"input_per_mtok"`
	OutputPerMTok float64 `yaml:"output_per_mtok"`
}

// Cost returns the cost of a call with the given token counts
func (p Price) Cost(promptTokens, completionTokens int64) float64 {
	return (float64(promptTokens)*p.InputPerMTok + float64(completionTokens)*p.OutputPerMTok) / 1_000_000
}

// defaults are list prices of the built-in providers' models; operators should check them
// against their contracts and override them with a price file. Anthropic's are first-party
// API rates; claude-haiku-5-5 costs more above 100K prompt tokens, which is not modelled
var defaults = map[string]Price{
	"anthropic/claude-fable-5-1":  {InputPerMTok: 10, OutputPerMTok: 50},
	"anthropic/claude-opus-5-5":   {InputPerMTok: 4, OutputPerMTok: 20},
	"anthropic/claude-sonnet-5-5": {InputPerMTok: 2, OutputPerMTok: 10},
	"anthropic/claude-haiku-5-5":  {InputPerMTok: 0.10, OutputPerMTok: 0.50},
	"anthropic/claude-haiku-4-5":  {InputPerMTok: 1, OutputPerMTok: 5},
	"openai/gpt-4o":               {InputPerMTok: 2.50, OutputPerMTok: 10},
	"openai/gpt-4o-mini":          {InputPerMTok: 0.15, OutputPerMTok: 0.60},
	"openai/gpt-4.1":              {InputPerMTok: 2, OutputPerMTok: 8},
	"openai/gpt-4.1-mini":         {InputPerMTok: 0.40, OutputPerMTok: 1.60},
}

// Table prices models by provider and model
type Table struct {
	prices map[string]Price
}

// Default returns the table of the default prices
func Default() *Table {
	prices := make(map[string]Price, len(defaults))
	for key, price := range defaults {
		prices[key] = price
	}
	return &Table{prices: prices}
}

func key(provider, model string) string {
	return strings.ToLower(provider) + "/" + strings.ToLower(model)
}

// Lookup returns the price of the model. A model the provider reports with a version
// suffix, e.g. gpt-4o-mini-2024-07-18, has the price of the longest model name it starts with
func (t *Table) Lookup(provider, model string) (Price, bool) {
	if price, ok := t.prices[key(provider, model)]; ok {
		return price, true
	}
	prefix := key(provider, "")
	best, bestLength := Price{}, 0
	for name, price := range t.prices {
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		modelName := strings.TrimPrefix(name, prefix)
		if strings.HasPrefix(strings.ToLower(model), modelName+"-") && len(modelName) > bestLength {
			best, bestLength = price, len(modelName)
		}
	}
	return best, bestLength > 0
}

// Cost returns the cost of a call, and whether the model has a price
func (t *Table) Cost(provider, model string, promptTokens, completionTokens int64) (float64, bool) {
	price, ok := t.Lookup(provider, model)
	if !ok {
		return 0, false
	}
	return price.Cost(promptTokens, completionTokens), true
}

// Set sets the price of a model
func (t *Table) Set(provider, model string, price Price) {
	t.prices[key(provider, model)] = price
}

// Models returns the provider/model names the table prices, sorted
func (t *Table) Models() []string {
	names := make([]string, 0, len(t.prices))
	for name := range t.prices {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// priceFile is a YAML price file:
//
//	anthropic:
//	  claude-opus-5-5: {input_per_mtok: 4, output_per_mtok: 20}
//	my-vllm:
//	  llama3.2: {input_per_mtok: 0, output_per_mtok: 0}
type priceFile map[string]map[string]Price

// Load returns the default prices, overridden and extended by the price file at path; an
// empty path means the defaults only
func Load(path string) (*Table, error) {
	table := Default()
	if path == "" {
		return table, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var file priceFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("price file %s: %w", path, err)
	}
	for provider, models := range file {
		for model, price := range models {
			if price.InputPerMTok < 0 || price.OutputPerMTok < 0 {
				return nil, fmt.Errorf("price file %s: %s/%s: prices must not be negative", path, provider, model)
			}
			table.Set(provider, model, price)
		}
	}
	return table, nil
}
