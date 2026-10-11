package impl

import (
	gogiv1 "gogi/gogi/gogi/v1"
)

// builtinModels are the models of the built-in providers, by provider
var builtinModels = map[string][]*gogiv1.ModelInfo{
	"anthropic": {
		builtinModel("claude-opus-5-5", "anthropic", 1_000_000, true),
		builtinModel("claude-sonnet-5-5", "anthropic", 1_000_000, true),
		builtinModel("claude-haiku-4-5", "anthropic", 200_000, true),
		builtinModel("claude-fable-5-1", "anthropic", 1_000_000, true),
	},
	"openai": {
		builtinModel("gpt-4o", "openai", 128_000, true),
		builtinModel("gpt-4o-mini", "openai", 128_000, true),
		builtinModel("gpt-4.1", "openai", 1_047_576, true),
		builtinModel("gpt-4.1-mini", "openai", 1_047_576, true),
	},
}

func builtinModel(name, provider string, contextWindow int32, supportsVision bool) *gogiv1.ModelInfo {
	return &gogiv1.ModelInfo{
		Name:     name,
		Provider: provider,
		Capabilities: &gogiv1.LLMCapabilities{
			ContextWindow:     contextWindow,
			SupportsVision:    supportsVision,
			SupportsTools:     true,
			SupportsStreaming: true,
			SupportsJsonMode:  true,
		},
	}
}

// findBuiltinModel returns the built-in model with the given name, or nil
func findBuiltinModel(name string) *gogiv1.ModelInfo {
	for _, models := range builtinModels {
		for _, model := range models {
			if model.Name == name {
				return model
			}
		}
	}
	return nil
}
