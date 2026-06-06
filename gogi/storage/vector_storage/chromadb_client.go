package vector_storage

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
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
	Name     string                 `json:"name"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

func (c *ChromaDBClient) CreateCollection(name string) error {

	reqBody := CreateCollectionRequest{
		Name: name,
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
