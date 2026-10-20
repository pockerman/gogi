package embeddings

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
)

// MaxBatch is how many texts go in one request to a model's endpoint
const MaxBatch = 128

// ErrUnreachable is a model's endpoint that could not be called, or did not answer as an
// OpenAI-compatible embeddings endpoint
var ErrUnreachable = errors.New("the embedding model could not be reached")

// Client calls OpenAI-compatible embeddings endpoints
type Client struct {
	http *http.Client
}

func NewClient(httpClient *http.Client) *Client {
	return &Client{http: httpClient}
}

type embeddingsRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embeddingsResponse struct {
	Data []struct {
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

// Embed embeds the texts with the model at baseURL, sending key as a bearer token if there is one.
// The vectors are in the order of the texts
func (c *Client) Embed(ctx context.Context, baseURL, model, key string, texts []string) ([][]float32, error) {
	vectors := make([][]float32, 0, len(texts))
	for start := 0; start < len(texts); start += MaxBatch {
		end := min(start+MaxBatch, len(texts))
		batch, err := c.embedBatch(ctx, baseURL, model, key, texts[start:end])
		if err != nil {
			return nil, err
		}
		vectors = append(vectors, batch...)
	}
	return vectors, nil
}

func (c *Client) embedBatch(ctx context.Context, baseURL, model, key string, texts []string) ([][]float32, error) {
	body, err := json.Marshal(embeddingsRequest{Model: model, Input: texts})
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnreachable, err)
	}
	request.Header.Set("Content-Type", "application/json")
	if key != "" {
		request.Header.Set("Authorization", "Bearer "+key)
	}

	response, err := c.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnreachable, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 512))
		return nil, &StatusError{Code: response.StatusCode, Message: strings.TrimSpace(string(message))}
	}

	var decoded embeddingsResponse
	if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("%w: its answer is not an embeddings response: %v", ErrUnreachable, err)
	}
	if len(decoded.Data) != len(texts) {
		return nil, fmt.Errorf("%w: it gave %d embeddings for %d texts", ErrUnreachable, len(decoded.Data), len(texts))
	}
	sort.Slice(decoded.Data, func(i, j int) bool { return decoded.Data[i].Index < decoded.Data[j].Index })
	vectors := make([][]float32, len(texts))
	for i, item := range decoded.Data {
		vectors[i] = item.Embedding
	}
	return vectors, nil
}

// StatusError is a model's endpoint that answered with an error, e.g. 401 for a wrong key
type StatusError struct {
	Code    int
	Message string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("the embedding model answered %d: %s", e.Code, e.Message)
}
