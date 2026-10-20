package impl

import (
	"bytes"
	"context"
	"errors"
	gogiv1 "gogi/gogi/gogi/v1"
	"gogi/gogi/utils"
	"io"
	"time"

	"gogi/gogi/applications"
	"gogi/gogi/documents"
	"gogi/gogi/documents/workflows"
	"gogi/gogi/storage/postgres"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	log "github.com/sirupsen/logrus"
	"go.temporal.io/sdk/client"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ObjectStore keeps the documents' uploaded files
type ObjectStore interface {
	// Put stores the content and returns its size; a size of -1 means unknown
	Put(ctx context.Context, bucket, object string, content io.Reader, size int64, contentType string) (int64, error)
	Remove(ctx context.Context, bucket, object string) error
	// Get reads from offset, length bytes, or to the end if length is 0
	Get(ctx context.Context, bucket, object string, offset, length int64) (io.ReadCloser, error)
}

// WorkflowStarter starts Temporal workflows; client.Client is one
type WorkflowStarter interface {
	ExecuteWorkflow(ctx context.Context, options client.StartWorkflowOptions, workflow interface{},
		args ...interface{}) (client.WorkflowRun, error)
}

type DocumentsServer struct {
	gogiv1.UnimplementedDocumentServerServer
	temporalClient WorkflowStarter
	jobsRepo       postgres.JobsRepository
	documentsRepo  *postgres.DocumentsRepository
	sectionsRepo   *postgres.DocumentSectionsRepository
	objects        ObjectStore
	// the embedding models applications can use
	models EmbeddingModelCatalog
	// uploads larger than this are refused
	maxUploadBytes int64
}

func NewDocumentsServer(temporalClient WorkflowStarter, dbClient *pgxpool.Pool, objects ObjectStore,
	models EmbeddingModelCatalog, maxUploadBytes int64) *DocumentsServer {
	return &DocumentsServer{
		temporalClient: temporalClient,
		jobsRepo:       *postgres.NewJobsRepository(dbClient),
		documentsRepo:  postgres.NewDocumentsRepository(dbClient),
		sectionsRepo:   postgres.NewDocumentSectionsRepository(dbClient),
		objects:        objects,
		models:         models,
		maxUploadBytes: maxUploadBytes,
	}
}

// IngestDocument ingests a document small enough for a single gRPC message
func (s *DocumentsServer) IngestDocument(ctx context.Context,
	req *gogiv1.IngestDocumentRequest) (*gogiv1.IngestDocumentJobResponse, error) {
	return s.ingest(ctx, req, bytes.NewReader(req.GetContent()), int64(len(req.GetContent())))
}

// UploadDocument ingests a document streamed in parts: its details first, then its content
func (s *DocumentsServer) UploadDocument(stream grpc.ClientStreamingServer[gogiv1.UploadDocumentRequest,
	gogiv1.IngestDocumentJobResponse]) error {

	first, err := stream.Recv()
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "expected the document's details: %v", err)
	}
	info := first.GetInfo()
	if info == nil {
		return status.Error(codes.InvalidArgument, "the first message must carry the document's details")
	}
	if len(info.GetContent()) > 0 {
		return status.Error(codes.InvalidArgument, "the document's details must not carry content; send it in chunks")
	}

	response, err := s.ingest(stream.Context(), info, &uploadReader{stream: stream}, -1)
	if err != nil {
		return err
	}
	return stream.SendAndClose(response)
}

// ingest stores the document's content, registers the document (or replaces its content) and, if
// models are named, starts its ingestion with them
func (s *DocumentsServer) ingest(ctx context.Context, req *gogiv1.IngestDocumentRequest, content io.Reader,
	size int64) (*gogiv1.IngestDocumentJobResponse, error) {

	indexName := req.GetIndexName()
	log.Infof("Ingesting document %s for %s", req.GetFilename(), indexName)

	if size > s.maxUploadBytes {
		return nil, s.tooLarge(req.GetFilename())
	}

	documentID := req.GetDocumentId()
	replace := req.GetReplace()
	if documentID == "" {
		if replace {
			return nil, status.Error(codes.InvalidArgument, "replacing a document needs its document_id")
		}
		documentID = uuid.New().String()
	}

	existing, err := s.documentsRepo.Get(ctx, indexName, documentID)
	switch {
	case err == nil && !replace:
		// a document of the same id is replaced only when asked to
		return nil, status.Errorf(codes.AlreadyExists, "document %s already exists in index %s", documentID, indexName)
	case errors.Is(err, utils.ErrDocumentNotFound) && replace:
		return nil, status.Errorf(codes.NotFound, "document %s not found in index %s", documentID, indexName)
	case err != nil && !errors.Is(err, utils.ErrDocumentNotFound):
		return nil, status.Errorf(codes.Internal, "failed to get document %s: %v", documentID, err)
	}

	// a model the application can't use is refused before anything is stored
	models, err := s.embeddingModels(ctx, indexName, req.GetEmbeddingModels())
	if err != nil {
		return nil, err
	}

	newUUID := uuid.New().String()

	// 1. store the content, so the workflow carries a reference to it rather than the content. New content
	// goes to a key of its own, so the old content stays until the document no longer refers to it
	objectKey := documents.ObjectKey(indexName, documentID)
	if replace {
		objectKey = documents.VersionKey(indexName, documentID, newUUID)
	}
	limited := &limitedReader{reader: content, remaining: s.maxUploadBytes}
	storedSize, err := s.objects.Put(ctx, documents.Bucket, objectKey, limited, size, req.GetContentType())
	if err != nil {
		// a partly stored object isn't kept, but make sure
		s.removeObject(objectKey)
		if limited.exceeded {
			return nil, s.tooLarge(req.GetFilename())
		}
		if st, ok := status.FromError(err); ok && st.Code() != codes.Unknown {
			return nil, err
		}
		return nil, status.Errorf(codes.Internal, "failed to store the document's content: %v", err)
	}

	// 2. create a job for the ingestion
	job := postgres.Job{
		ID:           newUUID,
		DocumentID:   documentID,
		Status:       utils.JobPending,
		JobType:      string(utils.DocumentIngestionJob),
		ErrorMessage: "NONE",
		CreatedAt:    time.Now(),
	}

	if err := s.jobsRepo.Create(ctx, job); err != nil {
		s.removeObject(objectKey)
		return nil, status.Errorf(codes.Internal, "failed to create the ingestion job: %v", err)
	}

	document := postgres.Document{
		ID:          documentID,
		IndexName:   indexName,
		Filename:    req.GetFilename(),
		ContentType: req.GetContentType(),
		Metadata:    req.GetMetadata(),
		JobID:       job.ID,
		SizeBytes:   storedSize,
		ObjectKey:   objectKey,
	}
	if replace {
		if err := s.replaceContent(ctx, existing, document); err != nil {
			s.removeObject(objectKey)
			return nil, err
		}
	} else if err := s.documentsRepo.Create(ctx, document); err != nil {
		// the document is listed from now on, with its job's status
		s.removeObject(objectKey)
		if errors.Is(err, utils.ErrIndexNotFound) {
			return nil, status.Errorf(codes.NotFound, "index %s not found", indexName)
		}
		return nil, status.Errorf(codes.Internal, "failed to register the document: %v", err)
	}

	// a document stored without models, e.g. a video, is ready at once; it's chunked when it's ingested
	if len(models) == 0 {
		if err := s.jobsRepo.UpdateStatus(ctx, job.ID, utils.JobCompleted); err != nil {
			return nil, status.Errorf(codes.Internal, "failed to complete the job of document %s: %v", documentID, err)
		}
		return &gogiv1.IngestDocumentJobResponse{
			IndexName:  indexName,
			DocumentId: documentID,
			Filename:   req.GetFilename(),
			Status:     string(utils.JobCompleted),
			Progress:   100.0,
			JobId:      job.ID,
		}, nil
	}

	// 3. map the request to the workflow's config
	pipelineConfig := workflows.IngestDocumentWorkflowConfig{
		JobId:           job.ID,
		IndexName:       indexName,
		Filename:        req.GetFilename(),
		DocumentID:      documentID,
		Application:     applications.FromContext(ctx),
		Bucket:          documents.Bucket,
		ObjectKey:       objectKey,
		ChunkStrategy:   req.GetChunkStrategy(),
		EmbeddingModels: models,
		BatchSize:       int(req.GetBatchSize()),
		Metadata:        req.GetMetadata(),
	}
	return s.startIngestion(ctx, pipelineConfig, req.GetFilename())
}

// startIngestion starts the workflow that chunks and embeds the document; the client polls its job
func (s *DocumentsServer) startIngestion(ctx context.Context, pipelineConfig workflows.IngestDocumentWorkflowConfig,
	filename string) (*gogiv1.IngestDocumentJobResponse, error) {

	options := client.StartWorkflowOptions{
		ID:        pipelineConfig.JobId,
		TaskQueue: utils.GetIngestionDocumentQueueName(), // Queue where the GO workflow worker is listening
		// Idempotency: Fail if already running, or use USE_EXISTING to attach to running one
		WorkflowIDConflictPolicy: 1, // WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING
	}

	// 4. start the workflow asynchronously (fire and forget); the client polls the job
	we, err := s.temporalClient.ExecuteWorkflow(ctx, options, workflows.IngestDocumentWorkflow, pipelineConfig)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to start workflow: %v", err)
	}

	return &gogiv1.IngestDocumentJobResponse{
		IndexName:    pipelineConfig.IndexName,
		DocumentId:   pipelineConfig.DocumentID,
		Filename:     filename,
		Status:       string(utils.JobPending), // Initial status
		Progress:     0.0,
		JobId:        we.GetID(), // Return the Workflow ID for tracking
		ErrorMessage: "",
	}, nil
}

// replaceContent points the document at its new content: it stops the ingestion of the old content,
// deletes the old content's chunks, points the document at the new content, and removes the old file.
// The worker stores no sections or chunks for a job that isn't the document's job any more
func (s *DocumentsServer) replaceContent(ctx context.Context, existing postgres.Document, document postgres.Document) error {
	if existing.JobID != "" {
		if err := s.jobsRepo.Cancel(ctx, existing.JobID); err != nil {
			return status.Errorf(codes.Internal, "failed to cancel the ingestion of document %s: %v", document.ID, err)
		}
	}
	// the cancelled ingestion stores no more chunks, and removes any it stores while stopping; no
	// model embeds the new content until it's ingested
	if _, err := s.deleteChunks(ctx, document.IndexName, document.ID, existing.EmbeddingModels); err != nil {
		return err
	}
	if err := s.documentsRepo.Replace(ctx, document); err != nil {
		return status.Errorf(codes.Internal, "failed to replace document %s: %v", document.ID, err)
	}
	if existing.ObjectKey != "" && existing.ObjectKey != document.ObjectKey {
		s.removeObject(existing.ObjectKey)
	}
	return nil
}

// IngestStoredDocument chunks a stored document and embeds it with the models named. What a model
// embedded of the document before is replaced when the model's new chunks are stored; what other
// models embedded is kept. An ingestion of the document still running is stopped
func (s *DocumentsServer) IngestStoredDocument(ctx context.Context,
	req *gogiv1.IngestStoredDocumentRequest) (*gogiv1.IngestDocumentJobResponse, error) {

	indexName, documentID := req.GetIndexName(), req.GetDocumentId()
	if len(req.GetEmbeddingModels()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "name at least one embedding model to ingest the document with")
	}
	document, err := s.getDocument(ctx, indexName, documentID)
	if err != nil {
		return nil, err
	}
	if document.ObjectKey == "" {
		return nil, status.Errorf(codes.FailedPrecondition, "document %s has no stored file to ingest", documentID)
	}
	models, err := s.embeddingModels(ctx, indexName, req.GetEmbeddingModels())
	if err != nil {
		return nil, err
	}

	// the running ingestion stops, and removes the chunks it stored; the new one is the document's
	if document.JobID != "" {
		if err := s.jobsRepo.Cancel(ctx, document.JobID); err != nil {
			return nil, status.Errorf(codes.Internal, "failed to cancel the ingestion of document %s: %v", documentID, err)
		}
	}
	job := postgres.Job{
		ID:           uuid.New().String(),
		DocumentID:   documentID,
		Status:       utils.JobPending,
		JobType:      string(utils.DocumentIngestionJob),
		ErrorMessage: "NONE",
		CreatedAt:    time.Now(),
	}
	if err := s.jobsRepo.Create(ctx, job); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create the ingestion job: %v", err)
	}
	if err := s.documentsRepo.SetJob(ctx, indexName, documentID, job.ID); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to start the ingestion of document %s: %v", documentID, err)
	}

	return s.startIngestion(ctx, workflows.IngestDocumentWorkflowConfig{
		JobId:           job.ID,
		IndexName:       indexName,
		Filename:        document.Filename,
		DocumentID:      documentID,
		Application:     applications.FromContext(ctx),
		Bucket:          documents.Bucket,
		ObjectKey:       document.ObjectKey,
		ChunkStrategy:   req.GetChunkStrategy(),
		EmbeddingModels: models,
		BatchSize:       int(req.GetBatchSize()),
		Metadata:        document.Metadata,
	}, document.Filename)
}

func (s *DocumentsServer) tooLarge(filename string) error {
	return status.Errorf(codes.InvalidArgument, "document %s is larger than the upload limit of %d MB",
		filename, s.maxUploadBytes/(1024*1024))
}

// removeObject removes an object that's no longer needed; a failure leaves it behind, so it's only logged
func (s *DocumentsServer) removeObject(objectKey string) {
	if err := s.objects.Remove(context.Background(), documents.Bucket, objectKey); err != nil {
		log.Warnf("failed to remove object %s: %v", objectKey, err)
	}
}

// uploadReader reads the content chunks of an upload stream
type uploadReader struct {
	stream grpc.ClientStreamingServer[gogiv1.UploadDocumentRequest, gogiv1.IngestDocumentJobResponse]
	buffer []byte
}

func (r *uploadReader) Read(p []byte) (int, error) {
	for len(r.buffer) == 0 {
		message, err := r.stream.Recv()
		if err != nil {
			// io.EOF when the client has sent everything
			return 0, err
		}
		if message.GetInfo() != nil {
			return 0, status.Error(codes.InvalidArgument, "the document's details must be sent once, first")
		}
		r.buffer = message.GetChunk()
	}
	n := copy(p, r.buffer)
	r.buffer = r.buffer[n:]
	return n, nil
}

// limitedReader fails once more than remaining bytes are read
type limitedReader struct {
	reader    io.Reader
	remaining int64
	exceeded  bool
}

var errTooLarge = errors.New("the document is larger than the upload limit")

func (r *limitedReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.remaining -= int64(n)
	if r.remaining < 0 {
		r.exceeded = true
		return n, errTooLarge
	}
	return n, err
}

// SearchDocuments searches the documents the model named embedded
func (s *DocumentsServer) SearchDocuments(ctx context.Context, req *gogiv1.SearchDocumentsRequest) (*gogiv1.SearchDocumentsResponse, error) {

	if req.GetEmbeddingModel() == "" {
		return nil, status.Error(codes.InvalidArgument, "name the embedding model to search with")
	}

	// an unknown index is not found; an index with nothing ingested yet has nothing to find
	exists, err := s.documentsRepo.IndexExists(ctx, req.GetIndexName())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to find index %s: %v", req.GetIndexName(), err)
	}
	if !exists {
		return nil, status.Errorf(codes.NotFound, "index %s not found", req.GetIndexName())
	}
	models, err := s.embeddingModels(ctx, req.GetIndexName(), []string{req.GetEmbeddingModel()})
	if err != nil {
		return nil, err
	}

	config := workflows.SearchDocumentsWorkflowConfig{
		IndexName:      req.GetIndexName(),
		Query:          req.GetQuery(),
		TopK:           int(req.GetTopK()),
		Application:    applications.FromContext(ctx),
		EmbeddingModel: models[0].Model,
		Collection:     models[0].Collection,
		DocumentIds:    req.GetDocumentIds(),
		MetadataFilter: req.GetMetadataFilter(),
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

// DeleteDocument cancels the document's ingestion, deletes its chunks from the vector store, then the
// document and its file
func (s *DocumentsServer) DeleteDocument(ctx context.Context, req *gogiv1.DeleteDocumentRequest) (*gogiv1.DeleteDocumentResponse, error) {
	indexName := req.GetIndexName()
	documentID := req.GetDocumentId()
	log.Infof("Deleting document %s from index %s", documentID, indexName)

	document, err := s.getDocument(ctx, indexName, documentID)
	if err != nil {
		return nil, err
	}

	// An ingestion still running would store chunks after we delete them. Cancelling its job first
	// makes the worker stop, and remove any chunks it stores from now on
	if document.JobID != "" {
		if err := s.jobsRepo.Cancel(ctx, document.JobID); err != nil {
			return nil, status.Errorf(codes.Internal, "failed to cancel the ingestion of document %s: %v", documentID, err)
		}
	}

	chunksDeleted, err := s.deleteChunks(ctx, indexName, documentID, document.EmbeddingModels)
	if err != nil {
		return nil, err
	}

	if err := s.documentsRepo.Delete(ctx, indexName, documentID); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to delete document %s: %v", documentID, err)
	}
	if document.ObjectKey != "" {
		s.removeObject(document.ObjectKey)
	}

	return &gogiv1.DeleteDocumentResponse{
		Response:      "Document deleted",
		ChunksDeleted: int32(chunksDeleted),
	}, nil
}

// ListDocumentSections lists the document's sections in order; a document has none until it's processed
func (s *DocumentsServer) ListDocumentSections(ctx context.Context, req *gogiv1.ListDocumentSectionsRequest) (*gogiv1.ListDocumentSectionsResponse, error) {
	if _, err := s.getDocument(ctx, req.GetIndexName(), req.GetDocumentId()); err != nil {
		return nil, err
	}
	sections, err := s.sectionsRepo.List(ctx, req.GetIndexName(), req.GetDocumentId())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list the sections of document %s: %v", req.GetDocumentId(), err)
	}

	response := &gogiv1.ListDocumentSectionsResponse{Sections: make([]*gogiv1.DocumentSection, 0, len(sections))}
	for _, section := range sections {
		response.Sections = append(response.Sections, toDocumentSection(section))
	}
	return response, nil
}

// GetDocumentSection returns a section's text, followed by its subsections' if asked for
func (s *DocumentsServer) GetDocumentSection(ctx context.Context, req *gogiv1.GetDocumentSectionRequest) (*gogiv1.GetDocumentSectionResponse, error) {
	if _, err := s.getDocument(ctx, req.GetIndexName(), req.GetDocumentId()); err != nil {
		return nil, err
	}
	sections, err := s.sectionsRepo.Read(ctx, req.GetIndexName(), req.GetDocumentId(), req.GetPosition(), req.GetIncludeSubsections())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to read section %d of document %s: %v", req.GetPosition(), req.GetDocumentId(), err)
	}
	if len(sections) == 0 {
		return nil, status.Errorf(codes.NotFound, "document %s has no section %d", req.GetDocumentId(), req.GetPosition())
	}

	response := &gogiv1.GetDocumentSectionResponse{Sections: make([]*gogiv1.DocumentSectionContent, 0, len(sections))}
	for _, section := range sections {
		response.Sections = append(response.Sections, &gogiv1.DocumentSectionContent{
			Section: toDocumentSection(section),
			Content: section.Content,
		})
	}
	return response, nil
}

// the size of the parts a download is streamed in, well under gRPC's 4 MB message limit
const downloadChunkBytes = 1024 * 1024

// DownloadDocument streams the document's uploaded file: the document first, then the file in parts
func (s *DocumentsServer) DownloadDocument(req *gogiv1.DownloadDocumentRequest,
	stream grpc.ServerStreamingServer[gogiv1.DownloadDocumentResponse]) error {

	ctx := stream.Context()
	document, err := s.getDocument(ctx, req.GetIndexName(), req.GetDocumentId())
	if err != nil {
		return err
	}
	if document.ObjectKey == "" {
		return status.Errorf(codes.FailedPrecondition, "document %s was ingested before uploads were kept; it has no file", document.ID)
	}

	if req.GetOffset() < 0 || req.GetLength() < 0 {
		return status.Error(codes.InvalidArgument, "offset and length must not be negative")
	}
	if req.GetOffset() > 0 && req.GetOffset() >= document.SizeBytes {
		return status.Errorf(codes.OutOfRange, "offset %d is past the end of document %s (%d bytes)",
			req.GetOffset(), document.ID, document.SizeBytes)
	}
	file, err := s.objects.Get(ctx, documents.Bucket, document.ObjectKey, req.GetOffset(), req.GetLength())
	if err != nil {
		return status.Errorf(codes.Internal, "failed to open the file of document %s: %v", document.ID, err)
	}
	defer file.Close()

	if err := stream.Send(&gogiv1.DownloadDocumentResponse{Part: &gogiv1.DownloadDocumentResponse_Document{
		Document: toDocumentMetadata(document)}}); err != nil {
		return err
	}
	buffer := make([]byte, downloadChunkBytes)
	for {
		n, readErr := io.ReadFull(file, buffer)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buffer[:n])
			if err := stream.Send(&gogiv1.DownloadDocumentResponse{Part: &gogiv1.DownloadDocumentResponse_Chunk{Chunk: chunk}}); err != nil {
				return err
			}
		}
		if readErr == io.EOF || readErr == io.ErrUnexpectedEOF {
			return nil
		}
		if readErr != nil {
			return status.Errorf(codes.Internal, "failed to read the file of document %s: %v", document.ID, readErr)
		}
	}
}

// deleteChunks deletes every chunk of the document from the vector store: those of each model that
// embedded it
func (s *DocumentsServer) deleteChunks(ctx context.Context, indexName string, documentID string, models []string) (int, error) {
	if len(models) == 0 {
		return 0, nil
	}
	options := client.StartWorkflowOptions{
		ID:        uuid.New().String(),
		TaskQueue: utils.GetIngestionDocumentQueueName(),
	}
	config := workflows.DeleteDocumentWorkflowConfig{IndexName: indexName, DocumentID: documentID,
		Collections: collections(indexName, models)}
	we, err := s.temporalClient.ExecuteWorkflow(ctx, options, workflows.DeleteDocumentWorkflow, config)
	if err != nil {
		return 0, status.Errorf(codes.Internal, "failed to start the delete workflow: %v", err)
	}

	var result workflows.DeleteDocumentWorkflowResult
	if err := we.Get(ctx, &result); err != nil {
		return 0, status.Errorf(codes.Internal, "failed to delete the chunks of document %s: %v", documentID, err)
	}
	return result.ChunksDeleted, nil
}

func toDocumentSection(section postgres.DocumentSection) *gogiv1.DocumentSection {
	return &gogiv1.DocumentSection{
		Position:  section.Position,
		Title:     section.Title,
		Level:     section.Level,
		PageStart: section.PageStart,
		PageEnd:   section.PageEnd,
	}
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
		DocumentId:      document.ID,
		IndexName:       document.IndexName,
		Filename:        document.Filename,
		ChunkCount:      document.ChunkCount,
		PageCount:       document.PageCount,
		WordCount:       document.WordCount,
		Metadata:        document.Metadata,
		ContentType:     document.ContentType,
		Status:          document.Status,
		JobId:           document.JobID,
		CreatedAt:       document.CreatedAt.Format(time.RFC3339),
		SizeBytes:       document.SizeBytes,
		EmbeddingModels: document.EmbeddingModels,
	}
}
