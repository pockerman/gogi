package pricing

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

func near(a, b float64) bool {
	return math.Abs(a-b) < 1e-12
}

func TestCost(t *testing.T) {
	table := Default()

	cost, ok := table.Cost("anthropic", "claude-opus-5-5", 3000, 200)
	if !ok || !near(cost, (3000*4+200*20)/1e6) {
		t.Errorf("wrong cost %v, %v", cost, ok)
	}
	// providers report versioned model names
	cost, ok = table.Cost("openai", "gpt-4o-mini-2024-07-18", 1_000_000, 0)
	if !ok || !near(cost, 0.15) {
		t.Errorf("a versioned name should get the price of its model, got %v, %v", cost, ok)
	}
	// the longest matching name wins: gpt-4o-mini, not gpt-4o
	if price, _ := table.Lookup("openai", "gpt-4o-mini-2024-07-18"); price.InputPerMTok != 0.15 {
		t.Errorf("expected gpt-4o-mini's price, got %+v", price)
	}
	if _, ok := table.Cost("ollama", "llama3.2", 10, 10); ok {
		t.Errorf("an unknown model has no price")
	}
	if _, ok := table.Cost("openai", "gpt-4o2", 10, 10); ok {
		t.Errorf("a prefix without a separator is another model")
	}
}

func TestLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prices.yaml")
	os.WriteFile(path, []byte(`
anthropic:
  claude-opus-5-5: {input_per_mtok: 3, output_per_mtok: 15}
my-vllm:
  llama3.2: {input_per_mtok: 0.05, output_per_mtok: 0.05}
`), 0o600)

	table, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if price, _ := table.Lookup("anthropic", "claude-opus-5-5"); price.InputPerMTok != 3 {
		t.Errorf("the file should override the default, got %+v", price)
	}
	if _, ok := table.Lookup("my-vllm", "llama3.2"); !ok {
		t.Errorf("the file should add models")
	}
	if _, ok := table.Lookup("openai", "gpt-4o"); !ok {
		t.Errorf("the defaults should stay")
	}

	os.WriteFile(path, []byte("openai:\n  gpt-4o: {input_per_mtok: -1, output_per_mtok: 1}\n"), 0o600)
	if _, err := Load(path); err == nil {
		t.Errorf("expected an error for a negative price")
	}
	if _, err := Load(filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Errorf("expected an error for a missing file")
	}
}
