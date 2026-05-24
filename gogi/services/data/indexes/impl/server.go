package impl

import (
	"context"
	"time"

	gogiv1 "gogi/gogi/gogi/v1"
)

// IndexServer implements the gRPC server.
// It provides methods to create, list, retrieve, and delete indexes based on the IndexConfig defined in models.go.
// This server is responsible for handling index-related operations and maintaining the lifecycle of vector indexes within the platform.
type IndexServer struct {
	gogiv1.UnimplementedIndexServerServer
}

func (s *IndexServer) CreateIndex(ctx context.Context, req *gogiv1.CreateIndexRequest) (*gogiv1.IndexResponse, error) {

	indexName := req.GetIndexName()
	owner := req.GetOwner()

	log.Infof("Creating index %s for owner %s", indexName, owner)

	return &gogiv1.IndexResponse{
		Name: indexName,
		IndexConfig: &gogiv1.IndexConfig{
			Name:                indexName,
			EmbeddingModel:      "text-embedding-3-small",
			EmbeddingDimensions: 1536,
			ChunkingStrategy:    "fixed",
			ChunkSize:           500,
			ChunkOverlap:        50,
			MetadataSchema:      map[string]string{"source": "string", "created_at": "timestamp"},
		},
		Owner:         owner,
		DocumentCount: 0,
		CreatedAt:     time.Now().Format(time.RFC3339),
		LastUpdatedAt: time.Now().Format(time.RFC3339),
	}, nil
}

func (s *IndexServer) ListIndexes(ctx context.Context, req *gogiv1.ListIndexesRequest) (*gogiv1.ListIndexesResponse, error) {
	log.Infof("Listing indexes")
	return &gogiv1.ListIndexesResponse{}, nil
}

func (s *IndexServer) GetIndex(ctx context.Context, req *gogiv1.GetIndexRequest) (*gogiv1.IndexResponse, error) {
	log.Infof("Getting index %s", req.GetIndexName())

	return &gogiv1.IndexResponse{
		Name: req.GetIndexName(),
		IndexConfig: &gogiv1.IndexConfig{
			Name:                req.GetIndexName(),
			EmbeddingModel:      "text-embedding-3-small",
			EmbeddingDimensions: 1536,
			ChunkingStrategy:    "fixed",
			ChunkSize:           500,
			ChunkOverlap:        50,
			MetadataSchema:      map[string]string{"source": "string", "created_at": "timestamp"},
		},
		Owner:         "example_owner",
		DocumentCount: 10,
		CreatedAt:     time.Now().Add(-24 * time.Hour).Format(time.RFC3339),
		LastUpdatedAt: time.Now().Format(time.RFC3339),
	}, nil
}

func (s *IndexServer) DeleteIndex(ctx context.Context, req *gogiv1.DeleteIndexRequest) (*gogiv1.DeleteIndexResponse, error) {
	log.Infof("Deleting index %s", req.GetIndexName())
	return &gogiv1.DeleteIndexResponse{Success: true, ChunksDeleted: 0}, nil
}
