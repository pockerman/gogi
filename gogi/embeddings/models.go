// Package embeddings is how gogi embeds text. Every model is called the same way, at an
// OpenAI-compatible embeddings endpoint: the small models gogi runs itself, each in a text
// embeddings inference server of its own; OpenAI's, with the application's key; and the models an
// application hosts and registers by their URL. gogi runs no heavy model.
package embeddings

import (
	"fmt"
	"regexp"

	"gogi/gogi/utils"
)

// OpenAICredential is the credential an application stores its OpenAI key as
const OpenAICredential = "openai"

// DefaultMaxInputTokens is what a registered model is taken to read if its registration doesn't say
const DefaultMaxInputTokens = 512

// Model is a text embedding model, named as the application names it
type Model struct {
	Name           string
	Dimensions     int
	MaxInputTokens int
	// the base URL of its OpenAI-compatible endpoint
	URL string
	// what the endpoint knows it by
	ServedName string
	// the credential it is called with, as the application named it; empty if none
	CredentialRef string
	// one gogi provides, rather than one an application registered
	BuiltIn bool
}

// BuiltIn are the models gogi provides: the two it runs, at the addresses of their servers, and
// OpenAI's
func BuiltIn() []Model {
	return []Model{
		{
			Name: "bge-small-en-v1.5", Dimensions: 384, MaxInputTokens: 512, BuiltIn: true,
			URL:        utils.GetEnv("GOGI_EMBEDDINGS_BGE_SMALL_URL", "http://embeddings-bge-small/v1"),
			ServedName: "BAAI/bge-small-en-v1.5",
		},
		{
			// its server reads 512 tokens, but the model was trained on 256 at most
			Name: "all-MiniLM-L6-v2", Dimensions: 384, MaxInputTokens: 256, BuiltIn: true,
			URL:        utils.GetEnv("GOGI_EMBEDDINGS_MINILM_URL", "http://embeddings-minilm/v1"),
			ServedName: "sentence-transformers/all-MiniLM-L6-v2",
		},
		{
			Name: "text-embedding-3-small", Dimensions: 1536, MaxInputTokens: 8191, BuiltIn: true,
			URL:           utils.GetEnv("GOGI_EMBEDDINGS_OPENAI_URL", utils.GetEnv("OPENAI_BASE_URL", "https://api.openai.com/v1")),
			ServedName:    "text-embedding-3-small",
			CredentialRef: OpenAICredential,
		},
	}
}

// FindBuiltIn is the built-in model of the name
func FindBuiltIn(name string) (Model, bool) {
	for _, model := range BuiltIn() {
		if model.Name == name {
			return model, true
		}
	}
	return Model{}, false
}

// a registered model's name has no dots, so it can't be read as part of a collection's index name
var namePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,62}$`)

// CheckName checks the name of a model an application registers
func CheckName(name string) error {
	if !namePattern.MatchString(name) {
		return fmt.Errorf("an embedding model's name is 2 to 63 lowercase letters, digits and dashes, "+
			"starting with a letter, not %q", name)
	}
	if _, ok := FindBuiltIn(name); ok {
		return fmt.Errorf("%q is a model gogi provides", name)
	}
	return nil
}

// ChunkWords is how many words a document's chunks for the model have, and how many of them
// overlap the chunk before: a word is a token or two, so a chunk is no longer than the model reads.
// Chunks are 400 words at most, however much the model reads, so a search finds passages rather
// than whole chapters
func ChunkWords(maxInputTokens int) (size, overlap int) {
	size = maxInputTokens * 3 / 5
	if size > 400 {
		size = 400
	}
	if size < 20 {
		size = 20
	}
	return size, size / 10
}
