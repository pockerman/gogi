package vector_storage

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// fakeChroma answers as Chroma's v2 API does, for the collection "idx.bge" of id "c-1" only
func fakeChroma(t *testing.T, requests map[string]map[string]any) *ChromaDBClient {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const collections = "/api/v2/tenants/default_tenant/databases/default_database/collections/"
		name := strings.TrimPrefix(r.URL.Path, collections)
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		requests[name] = body
		switch name {
		case "idx.bge":
			json.NewEncoder(w).Encode(map[string]any{"id": "c-1", "name": "idx.bge"})
		case "c-1/query":
			json.NewEncoder(w).Encode(map[string]any{
				"ids":       [][]string{{"a", "b"}},
				"documents": [][]any{{"wing flutter", nil}},
				"metadatas": [][]any{{map[string]any{"document_id": "fem-notes", "page": 3}, nil}},
				"distances": [][]float64{{0.25, 0.5}},
			})
		case "c-1/get":
			json.NewEncoder(w).Encode(map[string]any{"ids": []string{"a", "b"}})
		case "c-1/delete":
			w.Write([]byte("{}"))
		default:
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error":"NotFoundError"}`))
		}
	}))
	t.Cleanup(server.Close)
	host, port, _ := strings.Cut(strings.TrimPrefix(server.URL, "http://"), ":")
	portNumber, _ := strconv.Atoi(port)
	return NewChromaDBClient(host, portNumber)
}

func TestQueryFindsTheNearestChunks(t *testing.T) {
	requests := map[string]map[string]any{}
	client := fakeChroma(t, requests)

	results, err := client.Query(context.Background(), "idx.bge", []float32{0.5, 0.5}, 2, map[string]any{"subject": "fem"})

	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || results[0].Text != "wing flutter" || results[0].DocumentID != "fem-notes" ||
		results[0].Metadata["page"] != "3" || results[0].Score != 0.25 || results[1].Text != "" {
		t.Errorf("results = %+v", results)
	}
	query := requests["c-1/query"]
	if query["n_results"] != float64(2) || query["where"].(map[string]any)["subject"] != "fem" {
		t.Errorf("query = %v", query)
	}
}

func TestACollectionNothingWasStoredInHasNoChunks(t *testing.T) {
	client := fakeChroma(t, map[string]map[string]any{})

	results, err := client.Query(context.Background(), "idx.minilm", []float32{1}, 5, nil)
	deleted, deleteErr := client.DeleteWhere(context.Background(), "idx.minilm", map[string]any{"document_id": "x"})

	if err != nil || deleteErr != nil || len(results) != 0 || deleted != 0 {
		t.Errorf("results = %v, deleted = %d, errors = %v, %v", results, deleted, err, deleteErr)
	}
}

func TestDeleteWhereDeletesTheMatchingChunks(t *testing.T) {
	requests := map[string]map[string]any{}
	client := fakeChroma(t, requests)

	deleted, err := client.DeleteWhere(context.Background(), "idx.bge", map[string]any{"document_id": "fem-notes"})

	if err != nil || deleted != 2 {
		t.Fatalf("deleted = %d, err = %v", deleted, err)
	}
	if ids := requests["c-1/delete"]["ids"].([]any); len(ids) != 2 {
		t.Errorf("deleted ids = %v", ids)
	}
}
