package impl

import (
	"context"
	"errors"
	gogiv1 "gogi/gogi/gogi/v1"
	"gogi/gogi/utils"
	"time"

	"gogi/gogi/documents/workflows"
	"gogi/gogi/storage/postgres"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	log "github.com/sirupsen/logrus"
	"go.temporal.io/sdk/client"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type DocumentsServer struct {
	gogiv1.UnimplementedDocumentServerServer
	temporalClient client.Client // Inject this via dependency injection
	jobsRepo       postgres.JobsRepository
	documentsRepo  *postgres.DocumentsRepository
}

func NewDocumentsServer(temporalClient client.Client, dbClient *pgxpool.Pool) *DocumentsServer {
	return &DocumentsServer{
		temporalClient: temporalClient,
		jobsRepo:       *postgres.NewJobsRepository(dbClient),
		documentsRepo:  postgres.NewDocumentsRepository(dbClient),
	}
}

func (s *DocumentsServer) IngestDocument(ctx context.Context,
	req *gogiv1.IngestDocumentRequest) (*gogiv1.IngestDocumentJobResponse, error) {

	indexName := req.GetIndexName()
	log.Infof("Ingesting document for %s", indexName)

	documentID := req.GetDocumentId()
	if documentID == "" {
		documentID = uuid.New().String()
	}

	// create a Job fof the ingestion
	newUUID := uuid.New().String()
	job := postgres.Job{
		ID:           newUUID,
		DocumentID:   documentID,
		Status:       utils.JobPending,
		JobType:      string(utils.DocumentIngestionJob),
		ErrorMessage: "NONE",
		CreatedAt:    time.Now(),
	}

	if err := s.jobsRepo.Create(ctx, job); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create the ingestion job: %v", err)
	}

	// the document is listed from now on, with its job's status
	err := s.documentsRepo.Create(ctx, postgres.Document{
		ID:          documentID,
		IndexName:   indexName,
		Filename:    req.GetFilename(),
		ContentType: req.GetContentType(),
		Metadata:    req.GetMetadata(),
		JobID:       job.ID,
	})
	if errors.Is(err, utils.ErrIndexNotFound) {
		return nil, status.Errorf(codes.NotFound, "index %s not found", indexName)
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to register the document: %v", err)
	}

	// 1. Map gRPC request to Workflow Config
	pipelineConfig := workflows.IngestDocumentWorkflowConfig{
		JobId:            job.ID,
		IndexName:        req.GetIndexName(),
		Filename:         req.GetFilename(),
		DocumentID:       documentID,
		Content:          req.GetContent(),
		ChunkStrategy:    req.GetChunkStrategy(),
		EmbeddingsModel:  req.GetEmbeddingsModel(),
		EmbeddingsClient: req.GetEmbeddingsClient(),
		BatchSize:        int(req.GetBatchSize()),
		Metadata:         req.GetMetadata(),
	}

	taskQueueName := utils.GetIngestionDocumentQueueName()

	// 2. Start the Workflow asynchronously
	// We generate a deterministic ID based on DocumentID to prevent duplicate processing
	options := client.StartWorkflowOptions{
		ID:        newUUID,
		TaskQueue: taskQueueName, // Queue where the GO workflow worker is listening
		// Idempotency: Fail if already running, or use USE_EXISTING to attach to running one
		WorkflowIDConflictPolicy: 1, // WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING
	}

	// Execute the workflow defined in step 1
	we, err := s.temporalClient.ExecuteWorkflow(ctx, options, workflows.IngestDocumentWorkflow, pipelineConfig)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to start workflow: %v", err)
	}

	// 3. Return immediately (Fire-and-Forget pattern)
	// The client gets a Job ID (Workflow ID) to poll status later if needed
	return &gogiv1.IngestDocumentJobResponse{
		IndexName:    req.GetIndexName(),
		DocumentId:   documentID,
		Filename:     req.GetFilename(),
		Status:       string(utils.JobPending), // Initial status
		Progress:     0.0,
		JobId:        we.GetID(), // Return the Workflow ID for tracking
		ErrorMessage: "",
	}, nil
}

func (s *DocumentsServer) SearchDocuments(ctx context.Context, req *gogiv1.SearchDocumentsRequest) (*gogiv1.SearchDocumentsResponse, error) {

	config := workflows.SearchDocumentsWorkflowConfig{
		IndexName:        req.GetIndexName(),
		Query:            req.GetQuery(),
		TopK:             int(req.GetTopK()),
		EmbeddingsModel:  req.GetEmbeddingsModel(),
		EmbeddingsClient: req.GetEmbeddingsClient(),
		DocumentIds:      req.GetDocumentIds(),
		MetadataFilter:   req.GetMetadataFilter(),
	}

	taskQueueName := utils.GetIngestionDocumentQueueName()
	options := client.StartWorkflowOptions{
		ID:        uuid.New().String(),
		TaskQueue: taskQueueName,
	}

	// Unlike IngestDocument, a search has to answer the caller directly, so we block on
	// the workflow's result instead of returning a job id for the client to poll.
	we, err := s.temporalClient.ExecuteWorkflow(ctx, options, workflows.SearchDocumentsWorkflow, config)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to start search workflow: %v", err)
	}

	var result workflows.SearchDocumentsWorkflowResult
	if err := we.Get(ctx, &result); err != nil {
		return nil, status.Errorf(codes.Internal, "search failed: %v", err)
	}

	chunks := make([]*gogiv1.DocumentChunk, 0, len(result.Chunks))
	for _, c := range result.Chunks {
		chunks = append(chunks, &gogiv1.DocumentChunk{
			ChunkId:    c.ChunkID,
			DocumentId: c.DocumentID,
			IndexName:  c.IndexName,
			Content:    c.Content,
			Score:      c.Score,
			Metadata:   c.Metadata,
		})
	}

	return &gogiv1.SearchDocumentsResponse{Chunks: chunks}, nil
}

func (s *DocumentsServer) GetDocumentIngestJob(ctx context.Context, req *gogiv1.GetIngestDocumentJobRequest) (*gogiv1.IngestDocumentJobResponse, error) {

	job, err := s.jobsRepo.GetJob(req.GetJobId())
	if errors.Is(err, utils.ErrJobNotFound) {
		return nil, status.Errorf(codes.NotFound, "job %s not found", req.GetJobId())
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get job %s: %v", req.GetJobId(), err)
	}

	return &gogiv1.IngestDocumentJobResponse{
		JobId:        job.ID,
		DocumentId:   job.DocumentID,
		Status:       string(job.Status),
		ErrorMessage: job.ErrorMessage,
	}, nil
}

func (s *DocumentsServer) ListDocuments(ctx context.Context, req *gogiv1.ListDocumentsRequest) (*gogiv1.ListDocumentsResponse, error) {
	log.Infof("Listing documents of %s", req.GetIndexName())

	documents, err := s.documentsRepo.List(ctx, req.GetIndexName(), req.GetMetadataFilter())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list the documents of %s: %v", req.GetIndexName(), err)
	}

	response := &gogiv1.ListDocumentsResponse{Documents: make([]*gogiv1.DocumentMetadata, 0, len(documents))}
	for _, document := range documents {
		response.Documents = append(response.Documents, toDocumentMetadata(document))
	}
	return response, nil
}

func (s *DocumentsServer) GetDocument(ctx context.Context, req *gogiv1.GetDocumentRequest) (*gogiv1.GetDocumentResponse, error) {
	log.Infof("Getting document %s of %s", req.GetDocumentId(), req.GetIndexName())

	document, err := s.getDocument(ctx, req.GetIndexName(), req.GetDocumentId())
	if err != nil {
		return nil, err
	}
	return &gogiv1.GetDocumentResponse{Document: toDocumentMetadata(document)}, nil
}

// DeleteDocument deletes the document's chunks from the vector store, then the document
func (s *DocumentsServer) DeleteDocument(ctx context.Context, req *gogiv1.DeleteDocumentRequest) (*gogiv1.DeleteDocumentResponse, error) {
	indexName := req.GetIndexName()
	documentID := req.GetDocumentId()
	log.Infof("Deleting document %s from index %s", documentID, indexName)

	if _, err := s.getDocument(ctx, indexName, documentID); err != nil {
		return nil, err
	}

	options := client.StartWorkflowOptions{
		ID:        uuid.New().String(),
		TaskQueue: utils.GetIngestionDocumentQueueName(),
	}
	config := workflows.DeleteDocumentWorkflowConfig{IndexName: indexName, DocumentID: documentID}
	we, err := s.temporalClient.ExecuteWorkflow(ctx, options, workflows.DeleteDocumentWorkflow, config)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to start the delete workflow: %v", err)
	}

	var result workflows.DeleteDocumentWorkflowResult
	if err := we.Get(ctx, &result); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to delete the chunks of document %s: %v", documentID, err)
	}

	if err := s.documentsRepo.Delete(ctx, indexName, documentID); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to delete document %s: %v", documentID, err)
	}

	return &gogiv1.DeleteDocumentResponse{
		Response:      "Document deleted",
		ChunksDeleted: int32(result.ChunksDeleted),
	}, nil
}

func (s *DocumentsServer) getDocument(ctx context.Context, indexName string, documentID string) (postgres.Document, error) {
	document, err := s.documentsRepo.Get(ctx, indexName, documentID)
	if errors.Is(err, utils.ErrDocumentNotFound) {
		return postgres.Document{}, status.Errorf(codes.NotFound, "document %s not found in index %s", documentID, indexName)
	}
	if err != nil {
		return postgres.Document{}, status.Errorf(codes.Internal, "failed to get document %s: %v", documentID, err)
	}
	return document, nil
}

func toDocumentMetadata(document postgres.Document) *gogiv1.DocumentMetadata {
	return &gogiv1.DocumentMetadata{
		DocumentId:  document.ID,
		IndexName:   document.IndexName,
		Filename:    document.Filename,
		ChunkCount:  document.ChunkCount,
		PageCount:   document.PageCount,
		WordCount:   document.WordCount,
		Metadata:    document.Metadata,
		ContentType: document.ContentType,
		Status:      document.Status,
		JobId:       document.JobID,
		CreatedAt:   document.CreatedAt.Format(time.RFC3339),
	}
}
