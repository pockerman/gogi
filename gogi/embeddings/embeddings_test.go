package embeddings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestURLPolicy(t *testing.T) {
	tests := []struct {
		url          string
		allowPrivate bool
		allowed      bool
	}{
		{"https://8.8.8.8/v1", false, true},
		// the cloud's metadata endpoint hands out the server's credentials
		{"http://169.254.169.254/v1", false, false},
		{"http://169.254.169.254/v1", true, false},
		{"http://[fe80::1]/v1", true, false},
		{"http://0.0.0.0/v1", true, false},
		// gogi's own services and the developer's machine
		{"http://10.0.0.5/v1", false, false},
		{"http://192.168.1.20/v1", false, false},
		{"http://127.0.0.1:11434/v1", false, false},
		{"http://100.64.0.1/v1", false, false},
		{"http://[::ffff:10.0.0.5]/v1", false, false},
		{"http://10.0.0.5/v1", true, true},
		{"http://127.0.0.1:11434/v1", true, true},
		{"ftp://8.8.8.8/v1", false, false},
		{"https://user:secret@8.8.8.8/v1", false, false},
		{"not a url", false, false},
	}
	for _, test := range tests {
		err := URLPolicy{AllowPrivate: test.allowPrivate}.Check(context.Background(), test.url)
		if test.allowed && err != nil {
			t.Errorf("%s (private allowed: %v): %v, want allowed", test.url, test.allowPrivate, err)
		}
		if !test.allowed && !errors.Is(err, ErrURLNotAllowed) {
			t.Errorf("%s (private allowed: %v): %v, want not allowed", test.url, test.allowPrivate, err)
		}
	}
}

func TestAModelIsNotCalledAtAnAddressThePolicyRefuses(t *testing.T) {
	// a name that resolved somewhere allowed when the model was registered, and now resolves here
	model := newFakeModel(t, 3, "")

	client := NewClient(URLPolicy{}.HTTPClient(5 * time.Second))
	_, err := client.Embed(context.Background(), model.URL+"/v1", "nomic", "", []string{"wing flutter"})

	if !errors.Is(err, ErrURLNotAllowed) || !strings.Contains(err.Error(), "private network") {
		t.Errorf("err = %v, want the private address refused", err)
	}
}

func TestEmbedInTheOrderOfTheTexts(t *testing.T) {
	model := newFakeModel(t, 3, "models-key")
	texts := make([]string, MaxBatch+5)
	for i := range texts {
		texts[i] = fmt.Sprintf("text %d", i)
	}

	client := NewClient(URLPolicy{AllowPrivate: true}.HTTPClient(5 * time.Second))
	vectors, err := client.Embed(context.Background(), model.URL+"/v1/", "nomic", "models-key", texts)

	if err != nil {
		t.Fatal(err)
	}
	if len(vectors) != len(texts) {
		t.Fatalf("vectors = %d, want %d", len(vectors), len(texts))
	}
	for i, vector := range vectors {
		if len(vector) != 3 || vector[0] != float32(len(texts[i])) {
			t.Fatalf("vector %d = %v, not text %d's", i, vector, i)
		}
	}
}

func TestEmbedWithAWrongKey(t *testing.T) {
	model := newFakeModel(t, 3, "models-key")

	client := NewClient(URLPolicy{AllowPrivate: true}.HTTPClient(5 * time.Second))
	_, err := client.Embed(context.Background(), model.URL+"/v1", "nomic", "wrong", []string{"wing flutter"})

	var answered *StatusError
	if !errors.As(err, &answered) || answered.Code != http.StatusUnauthorized {
		t.Errorf("err = %v, want 401", err)
	}
}

func TestChunkWordsFitTheModel(t *testing.T) {
	tests := []struct{ tokens, size, overlap int }{{512, 307, 30}, {256, 153, 15}, {8191, 400, 40}, {10, 20, 2}}
	for _, test := range tests {
		if size, overlap := ChunkWords(test.tokens); size != test.size || overlap != test.overlap {
			t.Errorf("ChunkWords(%d) = %d, %d; want %d, %d", test.tokens, size, overlap, test.size, test.overlap)
		}
	}
}

func TestARegisteredModelsName(t *testing.T) {
	for _, name := range []string{"nomic", "e5-large"} {
		if err := CheckName(name); err != nil {
			t.Errorf("%q: %v", name, err)
		}
	}
	for _, name := range []string{"", "n", "Nomic", "nomic.v2", "1nomic", "bge-small-en-v1.5"} {
		if CheckName(name) == nil {
			t.Errorf("%q is allowed", name)
		}
	}
}

// newFakeModel serves an OpenAI-compatible embeddings endpoint whose vectors start with the length
// of their text, giving them in reverse order; it needs the key, if there is one
func newFakeModel(t *testing.T, dimensions int, key string) *httptest.Server {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		if key != "" && r.Header.Get("Authorization") != "Bearer "+key {
			http.Error(w, `{"error":"invalid key"}`, http.StatusUnauthorized)
			return
		}
		var request embeddingsRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		type item struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		}
		data := make([]item, 0, len(request.Input))
		for i := len(request.Input) - 1; i >= 0; i-- {
			vector := make([]float32, dimensions)
			vector[0] = float32(len(request.Input[i]))
			data = append(data, item{Index: i, Embedding: vector})
		}
		json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	t.Cleanup(server.Close)
	return server
}
