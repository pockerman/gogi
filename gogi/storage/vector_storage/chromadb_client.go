package vector_storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"gogi/gogi/storage/vector_storage/models"
)

type ChromaDBClient struct {
	baseURL string
	http    *http.Client
}

func NewChromaDBClient(host string, port int) *ChromaDBClient {
	return &ChromaDBClient{
		baseURL: fmt.Sprintf("http://%s:%d", host, port),
		http:    &http.Client{},
	}
}

type CreateCollectionRequest struct {
	Name        string                 `json:"name"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
	GetOrCreate bool                   `json:"get_or_create"`
}

// CreateCollection ensures a collection with this name exists. It is idempotent: calling it
// for a collection that already exists returns the existing collection instead of erroring,
// so callers can safely call it to self-heal a collection that went missing (e.g. after the
// vector store lost data) without having to track whether it was created before.
func (c *ChromaDBClient) CreateCollection(name string) error {

	reqBody := CreateCollectionRequest{
		Name:        name,
		GetOrCreate: true,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(
		http.MethodPost,
		c.baseURL+"/api/v2/tenants/default_tenant/databases/default_database/collections",
		bytes.NewReader(body),
	)
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK &&
		resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("chroma returned status %d", resp.StatusCode)
	}

	return nil
}

// DeleteCollection deletes the collection and everything in it. Deleting a collection that
// doesn't exist is not an error.
func (c *ChromaDBClient) DeleteCollection(name string) error {

	req, err := http.NewRequest(
		http.MethodDelete,
		c.baseURL+"/api/v2/tenants/default_tenant/databases/default_database/collections/"+url.PathEscape(name),
		nil,
	)
	if err != nil {
		return err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("chroma returned status %d", resp.StatusCode)
	}

	return nil
}

const collectionsPath = "/api/v2/tenants/default_tenant/databases/default_database/collections"

// errNoCollection is a collection that doesn't exist: nothing has been stored in it yet
var errNoCollection = errors.New("no such collection")

// collectionID is the id Chroma knows the collection by
func (c *ChromaDBClient) collectionID(ctx context.Context, name string) (string, error) {
	var collection struct {
		ID string `json:"id"`
	}
	if err := c.call(ctx, http.MethodGet, collectionsPath+"/"+url.PathEscape(name), nil, &collection); err != nil {
		return "", err
	}
	return collection.ID, nil
}

// call sends the request body as JSON and decodes the answer into out, if given
func (c *ChromaDBClient) call(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return errNoCollection
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("chroma returned status %d: %s", resp.StatusCode, message)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// Query is the topK chunks of the collection nearest the embedding, of those whose metadata matches
// where (nil for all), nearest first. A collection nothing has been stored in has none
func (c *ChromaDBClient) Query(ctx context.Context, collection string, embedding []float32, topK int,
	where map[string]any) ([]models.VectorSearchResult, error) {

	id, err := c.collectionID(ctx, collection)
	if errors.Is(err, errNoCollection) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	query := map[string]any{
		"query_embeddings": [][]float32{embedding},
		"n_results":        topK,
		"include":          []string{"documents", "metadatas", "distances"},
	}
	if len(where) > 0 {
		query["where"] = where
	}
	var answer struct {
		IDs       [][]string         `json:"ids"`
		Documents [][]*string        `json:"documents"`
		Metadatas [][]map[string]any `json:"metadatas"`
		Distances [][]float64        `json:"distances"`
	}
	if err := c.call(ctx, http.MethodPost, collectionsPath+"/"+id+"/query", query, &answer); err != nil {
		return nil, err
	}
	if len(answer.IDs) == 0 {
		return nil, nil
	}
	results := make([]models.VectorSearchResult, 0, len(answer.IDs[0]))
	for i, chunkID := range answer.IDs[0] {
		result := models.VectorSearchResult{ChunkID: chunkID, Metadata: map[string]string{}}
		if len(answer.Documents) > 0 && answer.Documents[0][i] != nil {
			result.Text = *answer.Documents[0][i]
		}
		if len(answer.Distances) > 0 {
			result.Score = answer.Distances[0][i]
		}
		if len(answer.Metadatas) > 0 {
			for key, value := range answer.Metadatas[0][i] {
				// a chunk's metadata can hold numbers, e.g. its page
				result.Metadata[key] = fmt.Sprint(value)
			}
		}
		result.DocumentID = result.Metadata["document_id"]
		results = append(results, result)
	}
	return results, nil
}

// DeleteWhere deletes the chunks of the collection whose metadata matches where, and returns how many
// it deleted. A collection nothing has been stored in has none
func (c *ChromaDBClient) DeleteWhere(ctx context.Context, collection string, where map[string]any) (int, error) {
	id, err := c.collectionID(ctx, collection)
	if errors.Is(err, errNoCollection) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var matched struct {
		IDs []string `json:"ids"`
	}
	if err := c.call(ctx, http.MethodPost, collectionsPath+"/"+id+"/get",
		map[string]any{"where": where, "include": []string{}}, &matched); err != nil {
		return 0, err
	}
	if len(matched.IDs) == 0 {
		return 0, nil
	}
	if err := c.call(ctx, http.MethodPost, collectionsPath+"/"+id+"/delete",
		map[string]any{"ids": matched.IDs}, nil); err != nil {
		return 0, err
	}
	return len(matched.IDs), nil
}
