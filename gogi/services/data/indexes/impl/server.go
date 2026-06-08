package impl

import (
	"context"
	"time"

	gogiv1 "gogi/gogi/gogi/v1"

	"gogi/gogi/storage/postgres"
	"gogi/gogi/storage/vector_storage"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// IndexServer implements the gRPC server.
// It provides methods to create, list, retrieve, and delete indexes based on the IndexConfig defined in models.go.
// This server is responsible for handling index-related operations and maintaining the lifecycle of vector indexes within the platform.
type IndexServer struct {
	gogiv1.UnimplementedIndexServiceServer
	chromaDBClient *vector_storage.ChromaDBClient
	gogiIndexRepo  postgres.GogiIndexRepository
}

func NewIndexServer(chromaDBClient *vector_storage.ChromaDBClient, dbClient *pgxpool.Pool) *IndexServer {
	return &IndexServer{
		chromaDBClient: chromaDBClient,
		gogiIndexRepo:  *postgres.NewGogiIndexesRepository(dbClient),
	}
}

func (s *IndexServer) CreateIndex(ctx context.Context, req *gogiv1.CreateIndexRequest) (*gogiv1.IndexResponse, error) {

	indexName := req.GetIndexName()
	owner := req.GetOwner()

	log.Infof("Creating index %s for owner %s", indexName, owner)

	newUUID := uuid.New().String()
	index := postgres.GogiIndex{Name: indexName, Owner: owner, Id: newUUID}
	s.gogiIndexRepo.Create(ctx, index)

	// vector storage create index
	s.chromaDBClient.CreateCollection(indexName)

	return &gogiv1.IndexResponse{
		IndexName:     indexName,
		Owner:         owner,
		Id:            newUUID,
		CreatedAt:     time.Now().Format(time.RFC3339),
		LastUpdatedAt: time.Now().Format(time.RFC3339),
	}, nil
}

func (s *IndexServer) ListIndexes(ctx context.Context, req *gogiv1.ListIndexesRequest) (*gogiv1.ListIndexesResponse, error) {
	log.Infof("Listing indexes")

	indexes, err := s.gogiIndexRepo.GetIndexesForOwner(req.GetOwnerName())

	if err != nil {
		return nil, status.Errorf(
			codes.Internal,
			"failed to list indexes: %v",
			err,
		)
	}

	resp := &gogiv1.ListIndexesResponse{
		Indexes: make([]*gogiv1.IndexResponse, 0, len(indexes)),
	}

	for _, idx := range indexes {
		resp.Indexes = append(resp.Indexes, &gogiv1.IndexResponse{
			Id:            idx.Id,
			IndexName:     idx.Name,
			Owner:         idx.Owner,
			CreatedAt:     idx.CreatedAt.Format(time.RFC3339),
			LastUpdatedAt: idx.LastUpdatedAt.Format(time.RFC3339),
		})
	}

	return resp, nil
}

func (s *IndexServer) GetIndexByName(ctx context.Context, req *gogiv1.GetIndexByNameRequest) (*gogiv1.IndexResponse, error) {
	log.Infof("Getting index %s", req.GetIndexName())

	index, _ := s.gogiIndexRepo.GetIndexByName(req.GetIndexName())

	return &gogiv1.IndexResponse{
		IndexName:     index.Name,
		Id:            index.Id,
		Owner:         index.Owner,
		CreatedAt:     index.CreatedAt.Format(time.RFC3339),
		LastUpdatedAt: index.LastUpdatedAt.Format(time.RFC3339),
	}, nil
}

func (s *IndexServer) GetIndexById(ctx context.Context, req *gogiv1.GetIndexByIdRequest) (*gogiv1.IndexResponse, error) {
	log.Infof("Getting index %s", req.GetIndexId())

	index, _ := s.gogiIndexRepo.GetIndexById(req.GetIndexId())

	return &gogiv1.IndexResponse{
		IndexName:     index.Name,
		Id:            index.Id,
		Owner:         index.Owner,
		CreatedAt:     index.CreatedAt.Format(time.RFC3339),
		LastUpdatedAt: index.LastUpdatedAt.Format(time.RFC3339),
	}, nil
}

func (s *IndexServer) DeleteIndex(ctx context.Context, req *gogiv1.DeleteIndexRequest) (*gogiv1.DeleteIndexResponse, error) {
	log.Infof("Deleting index %s", req.GetIndexName())
	return &gogiv1.DeleteIndexResponse{Success: true, ChunksDeleted: 0}, nil
}
