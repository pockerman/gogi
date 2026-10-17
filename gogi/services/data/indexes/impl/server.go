package impl

import (
	"context"
	"errors"
	"sort"
	"time"

	gogiv1 "gogi/gogi/gogi/v1"

	"gogi/gogi/storage/postgres"
	"gogi/gogi/storage/vector_storage"
	"gogi/gogi/utils"

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

	// Idempotent by name: if the index metadata already exists (e.g. the client calls this
	// every run), reuse it instead of erroring on the name's unique constraint. Always
	// (re)ensure the vector store collection regardless, since it can go missing independently
	// of the Postgres row (e.g. the vector store lost data on its own).
	index, err := s.gogiIndexRepo.GetIndexByName(indexName)
	if err != nil {
		if !errors.Is(err, utils.ErrIndexNotFound) {
			return nil, status.Errorf(codes.Internal, "failed to look up index: %v", err)
		}
		if err := s.gogiIndexRepo.Create(ctx, postgres.GogiIndex{Name: indexName, Owner: owner, Id: uuid.New().String()}); err != nil {
			return nil, status.Errorf(codes.Internal, "failed to create index: %v", err)
		}
		if index, err = s.gogiIndexRepo.GetIndexByName(indexName); err != nil {
			return nil, status.Errorf(codes.Internal, "failed to read the new index: %v", err)
		}
	}

	if err := s.chromaDBClient.CreateCollection(indexName); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create vector store collection: %v", err)
	}

	return toIndexResponse(index), nil
}

func (s *IndexServer) ListIndexes(ctx context.Context, req *gogiv1.ListIndexesRequest) (*gogiv1.ListIndexesResponse, error) {
	log.Infof("Listing indexes")

	var indexes []postgres.GogiIndex
	var err error
	if req.GetMember() != "" {
		indexes, err = s.gogiIndexRepo.GetIndexesForMember(req.GetMember())
	} else {
		indexes, err = s.gogiIndexRepo.GetIndexesForOwner(req.GetOwnerName())
	}

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
		resp.Indexes = append(resp.Indexes, toIndexResponse(idx))
	}

	return resp, nil
}

func (s *IndexServer) GetIndexByName(ctx context.Context, req *gogiv1.GetIndexByNameRequest) (*gogiv1.IndexResponse, error) {
	log.Infof("Getting index %s", req.GetIndexName())

	index, err := s.gogiIndexRepo.GetIndexByName(req.GetIndexName())
	if err != nil {
		return nil, indexError(err, req.GetIndexName())
	}
	return toIndexResponse(index), nil
}

func (s *IndexServer) GetIndexById(ctx context.Context, req *gogiv1.GetIndexByIdRequest) (*gogiv1.IndexResponse, error) {
	log.Infof("Getting index %s", req.GetIndexId())

	index, err := s.gogiIndexRepo.GetIndexById(req.GetIndexId())
	if err != nil {
		return nil, indexError(err, req.GetIndexId())
	}
	return toIndexResponse(index), nil
}

func (s *IndexServer) SetIndexMember(ctx context.Context, req *gogiv1.SetIndexMemberRequest) (*gogiv1.IndexResponse, error) {
	log.Infof("Giving %s the role %s on index %s", req.GetUser(), req.GetRole(), req.GetIndexName())

	role, ok := fromProtoRole[req.GetRole()]
	if !ok {
		return nil, status.Errorf(codes.InvalidArgument, "a member needs a role")
	}
	if req.GetUser() == "" {
		return nil, status.Errorf(codes.InvalidArgument, "a member needs a user")
	}

	index, err := s.gogiIndexRepo.GetIndexByName(req.GetIndexName())
	if err != nil {
		return nil, indexError(err, req.GetIndexName())
	}
	if err := s.gogiIndexRepo.SetMember(ctx, index.Id, req.GetUser(), role); err != nil {
		return nil, indexError(err, req.GetIndexName())
	}
	return s.GetIndexByName(ctx, &gogiv1.GetIndexByNameRequest{IndexName: req.GetIndexName()})
}

func (s *IndexServer) RemoveIndexMember(ctx context.Context, req *gogiv1.RemoveIndexMemberRequest) (*gogiv1.IndexResponse, error) {
	log.Infof("Removing %s from index %s", req.GetUser(), req.GetIndexName())

	index, err := s.gogiIndexRepo.GetIndexByName(req.GetIndexName())
	if err != nil {
		return nil, indexError(err, req.GetIndexName())
	}
	if err := s.gogiIndexRepo.RemoveMember(ctx, index.Id, req.GetUser()); err != nil {
		return nil, indexError(err, req.GetIndexName())
	}
	return s.GetIndexByName(ctx, &gogiv1.GetIndexByNameRequest{IndexName: req.GetIndexName()})
}

var fromProtoRole = map[gogiv1.IndexMemberRole]postgres.IndexRole{
	gogiv1.IndexMemberRole_INDEX_MEMBER_ROLE_OWNER:  postgres.IndexRoleOwner,
	gogiv1.IndexMemberRole_INDEX_MEMBER_ROLE_READER: postgres.IndexRoleReader,
}

var toProtoRole = map[postgres.IndexRole]gogiv1.IndexMemberRole{
	postgres.IndexRoleOwner:  gogiv1.IndexMemberRole_INDEX_MEMBER_ROLE_OWNER,
	postgres.IndexRoleReader: gogiv1.IndexMemberRole_INDEX_MEMBER_ROLE_READER,
}

func toIndexResponse(index postgres.GogiIndex) *gogiv1.IndexResponse {
	users := make([]string, 0, len(index.Members))
	for user := range index.Members {
		users = append(users, user)
	}
	sort.Strings(users)

	members := make([]*gogiv1.IndexMember, 0, len(users))
	for _, user := range users {
		members = append(members, &gogiv1.IndexMember{User: user, Role: toProtoRole[index.Members[user]]})
	}

	return &gogiv1.IndexResponse{
		Id:            index.Id,
		IndexName:     index.Name,
		Owner:         index.Owner,
		CreatedAt:     index.CreatedAt.Format(time.RFC3339),
		LastUpdatedAt: index.LastUpdatedAt.Format(time.RFC3339),
		Members:       members,
	}
}

func indexError(err error, index string) error {
	switch {
	case errors.Is(err, utils.ErrIndexNotFound):
		return status.Errorf(codes.NotFound, "index %s not found", index)
	case errors.Is(err, utils.ErrLastIndexOwner):
		return status.Errorf(codes.FailedPrecondition, "%v", err)
	default:
		return status.Errorf(codes.Internal, "index %s: %v", index, err)
	}
}

func (s *IndexServer) DeleteIndexByName(ctx context.Context, req *gogiv1.DeleteIndexByNameRequest) (*gogiv1.DeleteIndexResponse, error) {
	log.Infof("Deleting index %s", req.GetIndexName())

	success, _ := s.gogiIndexRepo.DeleteIndexByName(req.GetIndexName())

	return &gogiv1.DeleteIndexResponse{Success: success}, nil
}

func (s *IndexServer) DeleteIndexById(ctx context.Context, req *gogiv1.DeleteIndexByIdRequest) (*gogiv1.DeleteIndexResponse, error) {
	log.Infof("Deleting index %s", req.GetIndexId())

	success, _ := s.gogiIndexRepo.DeleteIndexById(req.GetIndexId())

	return &gogiv1.DeleteIndexResponse{Success: success}, nil
}

func (s *IndexServer) DeleteOwnerIndexes(ctx context.Context, req *gogiv1.DeleteOwnerIndexesRequest) (*gogiv1.DeleteIndexResponse, error) {
	log.Infof("Deleting indexes for owner %s", req.GetOwnerName())

	success, _ := s.gogiIndexRepo.DeleteOwnerIndexes(req.GetOwnerName())

	return &gogiv1.DeleteIndexResponse{Success: success}, nil
}
