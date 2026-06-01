package impl

import (
	"context"
	gogiv1 "gogi/gogi/gogi/v1"
	"gogi/gogi/utils"
	"time"

	"gogi/gogi/documents/workflows"
	"gogi/gogi/storage/postgres"

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
}

func NewDocumentsServer(temporalClient client.Client, dbClient *pgxpool.Pool) *DocumentsServer {
	return &DocumentsServer{
		temporalClient: temporalClient,
		jobsRepo:       *postgres.NewJobsRepository(dbClient),
	}
}

func (s *DocumentsServer) IngestDocument(ctx context.Context, req *gogiv1.IngestDocumentRequest) (*gogiv1.IngestDocumentJobResponse, error) {

	indexName := req.GetIndexName()

	// Your logic here
	log.Infof("Ingesting document for %s", indexName)

	// 1. Map gRPC request to Workflow Config
	pipelineConfig := workflows.IngestDocumentWorkflowConfig{
		IndexName:       req.GetIndexName(),
		Filename:        req.GetFilename(),
		DocumentID:      req.GetDocumentId(),
		Content:         req.GetContent(),
		ChunkStrategy:   req.GetChunkStrategy(),
		EmbeddingsModel: req.GetEmbeddingsModel(),
		BatchSize:       int(req.GetBatchSize()),
		Metadata:        req.GetMetadata(),
	}

	// 2. Start the Workflow asynchronously
	// We generate a deterministic ID based on DocumentID to prevent duplicate processing
	workflowID := "ingest-" + req.GetDocumentId()

	options := client.StartWorkflowOptions{
		ID:        workflowID,
		TaskQueue: "ingestion-document-queue", // Queue where the GO workflow worker is listening
		// Idempotency: Fail if already running, or use USE_EXISTING to attach to running one
		WorkflowIDConflictPolicy: 1, // WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING
	}

	// Execute the workflow defined in step 1
	we, err := s.temporalClient.ExecuteWorkflow(ctx, options, workflows.IngestDocumentWorkflow, pipelineConfig)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to start workflow: %v", err)
	}

	jobId := we.GetID()

	job := postgres.Job{
		ID:           jobId,
		DocumentID:   req.GetDocumentId(),
		Status:       utils.JobPending,
		JobType:      string(utils.DocumentIngestionJob),
		ErrorMessage: "NONE",
		CreatedAt:    time.Now(),
	}

	// we need to update the DB for this
	s.jobsRepo.Create(ctx, job)

	// 3. Return immediately (Fire-and-Forget pattern)
	// The client gets a Job ID (Workflow ID) to poll status later if needed
	return &gogiv1.IngestDocumentJobResponse{
		IndexName:    req.GetIndexName(),
		DocumentId:   req.GetDocumentId(),
		Filename:     req.GetFilename(),
		Status:       string(utils.JobPending), // Initial status
		Progress:     0.0,
		JobId:        we.GetID(), // Return the Workflow ID for tracking
		ErrorMessage: "",
	}, nil
}

func (s *DocumentsServer) GetDocumentIngestJob(ctx context.Context, req *gogiv1.GetIngestDocumentJobRequest) (*gogiv1.IngestDocumentJobResponse, error) {

	// we need to update the DB for this
	job, _ := s.jobsRepo.GetJob(req.GetJobId())

	return &gogiv1.IngestDocumentJobResponse{
		DocumentId:   job.DocumentID,
		Status:       string(job.Status),
		ErrorMessage: job.ErrorMessage,
	}, nil
}

func (s *DocumentsServer) ListDocuments(ctx context.Context, req *gogiv1.ListDocumentsRequest) (*gogiv1.ListDocumentsResponse, error) {
	indexName := req.GetIndexName()

	// Your logic here
	log.Infof("Listing documents for %s", indexName)

	return &gogiv1.ListDocumentsResponse{
		Documents: []*gogiv1.DocumentMetadata{{IndexName: indexName,
			DocumentId: "example",
			Filename:   "example",
			ChunkCount: 1, PageCount: 1, WordCount: 100}},
	}, nil
}

func (s *DocumentsServer) GetDocument(ctx context.Context, req *gogiv1.GetDocumentRequest) (*gogiv1.GetDocumentResponse, error) {
	indexName := req.GetIndexName()
	documentID := req.GetDocumentId()
	log.Infof("Getting document %s for index %s", documentID, indexName)

	// Your logic here

	return &gogiv1.GetDocumentResponse{
		Document: &gogiv1.DocumentMetadata{IndexName: indexName, DocumentId: documentID,
			Filename: "example", ChunkCount: 1, PageCount: 1, WordCount: 100},
	}, nil
}

func (s *DocumentsServer) DeleteDocument(ctx context.Context, req *gogiv1.DeleteDocumentRequest) (*gogiv1.DeleteDocumentResponse, error) {

	indexName := req.GetIndexName()
	documentID := req.GetDocumentId()

	log.Infof("Deleting document %s from index %s", documentID, indexName)

	// Your deletion logic here

	return &gogiv1.DeleteDocumentResponse{
		Response: "Document deleted",
	}, nil
}
