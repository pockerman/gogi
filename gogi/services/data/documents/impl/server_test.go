package impl

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"testing"

	"gogi/gogi/documents/ingestion"
	gogiv1 "gogi/gogi/gogi/v1"
	"gogi/gogi/storage/postgres"
	"gogi/gogi/storage/vector_storage/models"
	"gogi/gogi/utils"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

// fakeObjects keeps objects in memory
type fakeObjects struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func (f *fakeObjects) Put(_ context.Context, bucket, object string, content io.Reader, _ int64, _ string) (int64, error) {
	data, err := io.ReadAll(content)
	if err != nil {
		return 0, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.objects[bucket+"/"+object] = data
	return int64(len(data)), nil
}

func (f *fakeObjects) Remove(_ context.Context, bucket, object string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.objects, bucket+"/"+object)
	return nil
}

func (f *fakeObjects) Get(_ context.Context, bucket, object string, offset, length int64) (io.ReadCloser, error) {
	data, ok := f.get(bucket + "/" + object)
	if !ok {
		return nil, errors.New("no such object")
	}
	data = data[offset:]
	if length > 0 && length < int64(len(data)) {
		data = data[:length]
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (f *fakeObjects) get(key string) ([]byte, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	data, ok := f.objects[key]
	return data, ok
}

// fakeQueue records the ingestions handed to the worker
type fakeQueue struct {
	mu       sync.Mutex
	messages []ingestion.Message
	// what enqueueing fails with, if anything
	err error
}

func (f *fakeQueue) Enqueue(_ context.Context, message ingestion.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.messages = append(f.messages, message)
	return nil
}

// vectorCall is a query or delete of a collection
type vectorCall struct {
	collection string
	embedding  []float32
	topK       int
	where      map[string]any
}

// fakeVectors records the queries and deletes; a query finds the chunks given it
type fakeVectors struct {
	mu      sync.Mutex
	found   []models.VectorSearchResult
	queries []vectorCall
	deletes []vectorCall
}

func (f *fakeVectors) Query(_ context.Context, collection string, embedding []float32, topK int,
	where map[string]any) ([]models.VectorSearchResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queries = append(f.queries, vectorCall{collection: collection, embedding: embedding, topK: topK, where: where})
	return f.found, nil
}

func (f *fakeVectors) DeleteWhere(_ context.Context, collection string, where map[string]any) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deletes = append(f.deletes, vectorCall{collection: collection, where: where})
	return 3, nil
}

func (f *fakeVectors) deletedFrom() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	collections := make([]string, 0, len(f.deletes))
	for _, call := range f.deletes {
		collections = append(collections, call.collection)
	}
	return collections
}

// fakeModels are the models gogi provides; the application has no OpenAI key
type fakeModels struct{}

func (fakeModels) ListEmbeddingModels(context.Context) ([]*gogiv1.EmbeddingModel, error) {
	return []*gogiv1.EmbeddingModel{
		{Name: "bge-small-en-v1.5", Dimensions: 384, MaxInputTokens: 512, Available: true},
		{Name: "all-MiniLM-L6-v2", Dimensions: 384, MaxInputTokens: 256, Available: true},
		{Name: "text-embedding-3-small", Dimensions: 1536, MaxInputTokens: 8191, CredentialRef: "openai"},
	}, nil
}

// Embed gives every text the same vector
func (fakeModels) Embed(_ context.Context, _ string, texts []string) ([][]float32, error) {
	vectors := make([][]float32, len(texts))
	for i := range texts {
		vectors[i] = []float32{0.5, 0.5}
	}
	return vectors, nil
}

type testService struct {
	client  gogiv1.DocumentServerClient
	objects *fakeObjects
	queue   *fakeQueue
	vectors *fakeVectors
	jobs    *postgres.JobsRepository
	pool    *pgxpool.Pool
}

// The test runs against a migrated database, e.g.
// GOGI_TEST_POSTGRES_DSN=postgres://postgres:test@localhost:5432/gogi?sslmode=disable
func newTestService(t *testing.T, maxUploadBytes int64) testService {
	dsn := os.Getenv("GOGI_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("GOGI_TEST_POSTGRES_DSN is not set")
	}
	pool, err := postgres.NewPool(dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	// deleting the indexes deletes their documents
	clear := func() {
		if _, err := pool.Exec(context.Background(), "DELETE FROM gogi_indexes"); err != nil {
			t.Fatal(err)
		}
	}
	clear()
	t.Cleanup(clear)
	indexes := postgres.NewGogiIndexesRepository(pool)
	if err := indexes.Create(context.Background(), postgres.GogiIndex{Id: uuid.New().String(), Name: "maria--aeronautics", Owner: "maria"}); err != nil {
		t.Fatal(err)
	}

	service := testService{objects: &fakeObjects{objects: map[string][]byte{}}, queue: &fakeQueue{}, vectors: &fakeVectors{},
		jobs: postgres.NewJobsRepository(pool), pool: pool}
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	gogiv1.RegisterDocumentServerServer(server, NewDocumentsServer(service.queue, service.vectors, pool, service.objects, fakeModels{}, maxUploadBytes))
	go server.Serve(listener)
	t.Cleanup(server.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	service.client = gogiv1.NewDocumentServerClient(conn)
	return service
}

func info(documentID string) *gogiv1.UploadDocumentRequest {
	return &gogiv1.UploadDocumentRequest{Part: &gogiv1.UploadDocumentRequest_Info{Info: &gogiv1.IngestDocumentRequest{
		IndexName:       "maria--aeronautics",
		DocumentId:      documentID,
		Filename:        "fe-basics.txt",
		ContentType:     "text/plain",
		Metadata:        map[string]string{"subject": "numerical-methods"},
		EmbeddingModels: []string{"bge-small-en-v1.5"},
	}}}
}

func chunk(content string) *gogiv1.UploadDocumentRequest {
	return &gogiv1.UploadDocumentRequest{Part: &gogiv1.UploadDocumentRequest_Chunk{Chunk: []byte(content)}}
}

func upload(t *testing.T, client gogiv1.DocumentServerClient, parts ...*gogiv1.UploadDocumentRequest) (*gogiv1.IngestDocumentJobResponse, error) {
	t.Helper()
	stream, err := client.UploadDocument(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range parts {
		// io.EOF means the server ended the upload early; CloseAndRecv returns why
		if err := stream.Send(part); err != nil {
			break
		}
	}
	return stream.CloseAndRecv()
}

func TestUploadDocumentStoresTheContentAndHandsItsKeyToTheWorker(t *testing.T) {
	service := newTestService(t, 1024)

	response, err := upload(t, service.client, info("fe-basics"), chunk("The finite element method "), chunk("divides a domain "), chunk("into elements."))
	if err != nil {
		t.Fatal(err)
	}

	content, ok := service.objects.get("gogi-documents/maria--aeronautics/fe-basics")
	if !ok || string(content) != "The finite element method divides a domain into elements." {
		t.Errorf("stored content = %q", content)
	}
	if len(service.queue.messages) != 1 {
		t.Fatalf("ingestions queued = %d, want 1", len(service.queue.messages))
	}
	config := service.queue.messages[0]
	if config.Bucket != "gogi-documents" || config.ObjectKey != "maria--aeronautics/fe-basics" || config.JobID != response.GetJobId() {
		t.Errorf("message = %+v", config)
	}
	// the document is chunked to fit the model, into the index's collection for it
	want := ingestion.EmbeddingModel{Model: "bge-small-en-v1.5", Collection: "maria--aeronautics.bge-small-en-v1.5",
		ChunkSize: 307, ChunkOverlap: 30}
	if len(config.EmbeddingModels) != 1 || config.EmbeddingModels[0] != want || config.Application != "default" {
		t.Errorf("workflow models = %+v for %q, want %+v", config.EmbeddingModels, config.Application, want)
	}

	document, err := service.client.GetDocument(context.Background(), &gogiv1.GetDocumentRequest{IndexName: "maria--aeronautics", DocumentId: "fe-basics"})
	if err != nil {
		t.Fatal(err)
	}
	if document.GetDocument().GetSizeBytes() != int64(len(content)) || document.GetDocument().GetMetadata()["subject"] != "numerical-methods" {
		t.Errorf("document = %+v", document.GetDocument())
	}
}

func TestIngestDocumentStoresTheContentToo(t *testing.T) {
	service := newTestService(t, 1024)

	_, err := service.client.IngestDocument(context.Background(), &gogiv1.IngestDocumentRequest{
		IndexName: "maria--aeronautics", DocumentId: "fd-schemes", Filename: "fd-schemes.txt", Content: []byte("forward differences"),
		EmbeddingModels: []string{"all-MiniLM-L6-v2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if content, _ := service.objects.get("gogi-documents/maria--aeronautics/fd-schemes"); string(content) != "forward differences" {
		t.Errorf("stored content = %q", content)
	}
}

func TestUploadDocumentRefusals(t *testing.T) {
	tests := []struct {
		name  string
		parts []*gogiv1.UploadDocumentRequest
		code  codes.Code
		// part of the message
		message string
	}{
		{"larger than the limit", []*gogiv1.UploadDocumentRequest{info("big"), chunk(strings.Repeat("x", 600)), chunk(strings.Repeat("x", 600))},
			codes.InvalidArgument, "larger than the upload limit"},
		{"content before the details", []*gogiv1.UploadDocumentRequest{chunk("text"), info("doc")},
			codes.InvalidArgument, "first message"},
		{"the details twice", []*gogiv1.UploadDocumentRequest{info("doc"), chunk("text"), info("doc")},
			codes.InvalidArgument, "sent once"},
		{"a missing index", []*gogiv1.UploadDocumentRequest{
			{Part: &gogiv1.UploadDocumentRequest_Info{Info: &gogiv1.IngestDocumentRequest{IndexName: "missing", DocumentId: "doc", Filename: "x.txt"}}},
			chunk("text")}, codes.NotFound, "index missing not found"},
		{"a model gogi neither runs nor has registered", []*gogiv1.UploadDocumentRequest{
			{Part: &gogiv1.UploadDocumentRequest_Info{Info: &gogiv1.IngestDocumentRequest{IndexName: "maria--aeronautics", DocumentId: "doc",
				Filename: "x.txt", EmbeddingModels: []string{"bge-small-en-v1.5", "clip"}}}},
			chunk("text")}, codes.NotFound, `there is no embedding model "clip"`},
		{"OpenAI's model without the application's key", []*gogiv1.UploadDocumentRequest{
			{Part: &gogiv1.UploadDocumentRequest_Info{Info: &gogiv1.IngestDocumentRequest{IndexName: "maria--aeronautics", DocumentId: "doc",
				Filename: "x.txt", EmbeddingModels: []string{"text-embedding-3-small"}}}},
			chunk("text")}, codes.FailedPrecondition, `store one as the credential "openai"`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := newTestService(t, 1024)

			_, err := upload(t, service.client, test.parts...)

			if status.Code(err) != test.code || !strings.Contains(status.Convert(err).Message(), test.message) {
				t.Errorf("err = %v, want %s containing %q", err, test.code, test.message)
			}
			if len(service.objects.objects) != 0 || len(service.queue.messages) != 0 {
				t.Errorf("objects = %d, ingestions = %d; the refused upload left them behind",
					len(service.objects.objects), len(service.queue.messages))
			}
			listed, _ := service.client.ListDocuments(context.Background(), &gogiv1.ListDocumentsRequest{IndexName: "maria--aeronautics"})
			if len(listed.GetDocuments()) != 0 {
				t.Errorf("the refused upload was registered: %v", listed.GetDocuments())
			}
		})
	}
}

func TestUploadDocumentRefusesAnExistingDocument(t *testing.T) {
	service := newTestService(t, 1024)
	if _, err := upload(t, service.client, info("fe-basics"), chunk("first")); err != nil {
		t.Fatal(err)
	}

	_, err := upload(t, service.client, info("fe-basics"), chunk("second"))

	if status.Code(err) != codes.AlreadyExists {
		t.Errorf("err = %v, want AlreadyExists", err)
	}
	if content, _ := service.objects.get("gogi-documents/maria--aeronautics/fe-basics"); string(content) != "first" {
		t.Errorf("the existing document's content was replaced with %q", content)
	}
}

func TestDeleteDocumentRemovesItsFile(t *testing.T) {
	service := newTestService(t, 1024)
	if _, err := upload(t, service.client, info("fe-basics"), chunk("text")); err != nil {
		t.Fatal(err)
	}

	_, err := service.client.DeleteDocument(context.Background(), &gogiv1.DeleteDocumentRequest{IndexName: "maria--aeronautics", DocumentId: "fe-basics"})

	if err != nil {
		t.Fatal(err)
	}
	if _, ok := service.objects.get("gogi-documents/maria--aeronautics/fe-basics"); ok {
		t.Error("the document's file was not removed")
	}
}

func TestDeleteDocumentCancelsItsIngestion(t *testing.T) {
	tests := []struct {
		name string
		// the job's status when the document is deleted
		status utils.JobStatus
		want   utils.JobStatus
	}{
		{"still running", utils.JobPending, utils.JobCancelled},
		{"finished", utils.JobCompleted, utils.JobCompleted},
		{"failed", utils.JobFailed, utils.JobFailed},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := newTestService(t, 1024)
			response, err := upload(t, service.client, info("fe-basics"), chunk("text"))
			if err != nil {
				t.Fatal(err)
			}
			if err := service.jobs.UpdateStatus(context.Background(), response.GetJobId(), test.status); err != nil {
				t.Fatal(err)
			}

			if _, err := service.client.DeleteDocument(context.Background(), &gogiv1.DeleteDocumentRequest{IndexName: "maria--aeronautics", DocumentId: "fe-basics"}); err != nil {
				t.Fatal(err)
			}

			job, err := service.client.GetDocumentIngestJob(context.Background(), &gogiv1.GetIngestDocumentJobRequest{JobId: response.GetJobId()})
			if err != nil {
				t.Fatal(err)
			}
			if job.GetStatus() != string(test.want) {
				t.Errorf("job status = %s, want %s", job.GetStatus(), test.want)
			}
		})
	}
}

func TestReadADocumentBySection(t *testing.T) {
	service := newTestService(t, 1024)
	if _, err := upload(t, service.client, info("fem-notes"), chunk("text")); err != nil {
		t.Fatal(err)
	}
	// the ingestion worker stores the sections
	for position, section := range []struct {
		title   string
		level   int
		content string
	}{{"1 Introduction", 1, "intro"}, {"2 Shape functions", 1, "shape"}, {"2.1 Linear elements", 2, "linear"}, {"3 Assembly", 1, "assembly"}} {
		_, err := service.pool.Exec(context.Background(), `INSERT INTO gogi_document_sections (index_id, document_id, position, title, level, content)
			SELECT id, 'fem-notes', $1, $2, $3, $4 FROM gogi_indexes WHERE name = 'maria--aeronautics'`,
			position+1, section.title, section.level, section.content)
		if err != nil {
			t.Fatal(err)
		}
	}

	listed, err := service.client.ListDocumentSections(context.Background(), &gogiv1.ListDocumentSectionsRequest{IndexName: "maria--aeronautics", DocumentId: "fem-notes"})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.GetSections()) != 4 || listed.GetSections()[2].GetTitle() != "2.1 Linear elements" || listed.GetSections()[2].GetLevel() != 2 {
		t.Errorf("sections = %v", listed.GetSections())
	}

	read, err := service.client.GetDocumentSection(context.Background(), &gogiv1.GetDocumentSectionRequest{
		IndexName: "maria--aeronautics", DocumentId: "fem-notes", Position: 2, IncludeSubsections: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(read.GetSections()) != 2 || read.GetSections()[0].GetContent() != "shape" || read.GetSections()[1].GetContent() != "linear" {
		t.Errorf("section 2 with its subsections = %v", read.GetSections())
	}

	_, err = service.client.GetDocumentSection(context.Background(), &gogiv1.GetDocumentSectionRequest{
		IndexName: "maria--aeronautics", DocumentId: "fem-notes", Position: 9})
	if status.Code(err) != codes.NotFound {
		t.Errorf("a missing section: err = %v, want NotFound", err)
	}
	_, err = service.client.ListDocumentSections(context.Background(), &gogiv1.ListDocumentSectionsRequest{IndexName: "maria--aeronautics", DocumentId: "missing"})
	if status.Code(err) != codes.NotFound {
		t.Errorf("a missing document: err = %v, want NotFound", err)
	}
}

func TestDownloadADocument(t *testing.T) {
	service := newTestService(t, 4*1024*1024)
	content := strings.Repeat("0123456789", 250*1024) // 2.5 MB, so it comes in three parts
	if _, err := upload(t, service.client, info("fem-notes"), chunk(content)); err != nil {
		t.Fatal(err)
	}

	stream, err := service.client.DownloadDocument(context.Background(), &gogiv1.DownloadDocumentRequest{IndexName: "maria--aeronautics", DocumentId: "fem-notes"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if first.GetDocument().GetFilename() != "fe-basics.txt" {
		t.Errorf("first message = %v, want the document", first)
	}
	var downloaded bytes.Buffer
	parts := 0
	for {
		message, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		downloaded.Write(message.GetChunk())
		parts++
	}
	if downloaded.String() != content || parts != 3 {
		t.Errorf("downloaded %d bytes in %d parts, want %d in 3", downloaded.Len(), parts, len(content))
	}
}

func TestADocumentStoredWithoutModelsIsReadyAtOnceWithoutIngestion(t *testing.T) {
	service := newTestService(t, 1024)
	video := &gogiv1.UploadDocumentRequest{Part: &gogiv1.UploadDocumentRequest_Info{Info: &gogiv1.IngestDocumentRequest{
		IndexName: "maria--aeronautics", DocumentId: "lecture-3", Filename: "lecture-3.mp4", ContentType: "video/mp4"}}}

	response, err := upload(t, service.client, video, chunk("not really a video"))
	if err != nil {
		t.Fatal(err)
	}

	if response.GetStatus() != string(utils.JobCompleted) || len(service.queue.messages) != 0 {
		t.Errorf("status = %s, ingestions = %d; want COMPLETED without ingestion", response.GetStatus(), len(service.queue.messages))
	}
	document, err := service.client.GetDocument(context.Background(), &gogiv1.GetDocumentRequest{IndexName: "maria--aeronautics", DocumentId: "lecture-3"})
	if err != nil {
		t.Fatal(err)
	}
	if document.GetDocument().GetStatus() != string(utils.JobCompleted) || len(document.GetDocument().GetEmbeddingModels()) != 0 {
		t.Errorf("document = %+v, want COMPLETED and embedded by no model", document.GetDocument())
	}
	if content, _ := service.objects.get("gogi-documents/maria--aeronautics/lecture-3"); string(content) != "not really a video" {
		t.Errorf("stored content = %q", content)
	}
}

func TestDownloadPartOfADocument(t *testing.T) {
	service := newTestService(t, 1024)
	if _, err := upload(t, service.client, info("fem-notes"), chunk("0123456789")); err != nil {
		t.Fatal(err)
	}
	download := func(offset, length int64) (string, error) {
		stream, err := service.client.DownloadDocument(context.Background(), &gogiv1.DownloadDocumentRequest{
			IndexName: "maria--aeronautics", DocumentId: "fem-notes", Offset: offset, Length: length})
		if err != nil {
			return "", err
		}
		var content bytes.Buffer
		for {
			message, err := stream.Recv()
			if err == io.EOF {
				return content.String(), nil
			}
			if err != nil {
				return "", err
			}
			content.Write(message.GetChunk())
		}
	}

	tests := []struct {
		offset, length int64
		want           string
	}{{0, 0, "0123456789"}, {3, 4, "3456"}, {7, 0, "789"}, {8, 10, "89"}}
	for _, test := range tests {
		if got, err := download(test.offset, test.length); err != nil || got != test.want {
			t.Errorf("download(%d, %d) = %q, %v; want %q", test.offset, test.length, got, err, test.want)
		}
	}
	if _, err := download(10, 0); status.Code(err) != codes.OutOfRange {
		t.Errorf("an offset past the end: err = %v, want OutOfRange", err)
	}
}

func TestReplaceADocumentsContent(t *testing.T) {
	service := newTestService(t, 1024)
	first, err := upload(t, service.client, info("fem-notes"), chunk("old text"))
	if err != nil {
		t.Fatal(err)
	}
	replacing := info("fem-notes")
	replacing.GetInfo().Replace = true
	replacing.GetInfo().Filename = "fem-notes.md"

	second, err := upload(t, service.client, replacing, chunk("new text"))
	if err != nil {
		t.Fatal(err)
	}

	if second.GetJobId() == first.GetJobId() || second.GetDocumentId() != "fem-notes" {
		t.Errorf("replacing gave job %s for document %s, want a new job for fem-notes", second.GetJobId(), second.GetDocumentId())
	}
	document, err := service.client.GetDocument(context.Background(), &gogiv1.GetDocumentRequest{IndexName: "maria--aeronautics", DocumentId: "fem-notes"})
	if err != nil {
		t.Fatal(err)
	}
	if document.GetDocument().GetJobId() != second.GetJobId() || document.GetDocument().GetFilename() != "fem-notes.md" ||
		document.GetDocument().GetSizeBytes() != int64(len("new text")) {
		t.Errorf("document = %+v", document.GetDocument())
	}
	// the old content's ingestion is stopped, and the new content is ingested
	old, _ := service.client.GetDocumentIngestJob(context.Background(), &gogiv1.GetIngestDocumentJobRequest{JobId: first.GetJobId()})
	if old.GetStatus() != string(utils.JobCancelled) {
		t.Errorf("the old job is %s, want CANCELLED", old.GetStatus())
	}
	latest := service.queue.messages[len(service.queue.messages)-1]
	if content, _ := service.objects.get("gogi-documents/" + latest.ObjectKey); string(content) != "new text" || latest.JobID != second.GetJobId() {
		t.Errorf("the new ingestion reads %q for job %s", content, latest.JobID)
	}
	// only the new content is kept
	if _, kept := service.objects.get("gogi-documents/maria--aeronautics/fem-notes"); kept || len(service.objects.objects) != 1 {
		t.Errorf("objects = %d; the old content was kept", len(service.objects.objects))
	}
}

func TestReplacingNeedsAnExistingDocument(t *testing.T) {
	service := newTestService(t, 1024)
	replacing := info("missing")
	replacing.GetInfo().Replace = true

	_, err := upload(t, service.client, replacing, chunk("text"))

	if status.Code(err) != codes.NotFound || len(service.objects.objects) != 0 {
		t.Errorf("err = %v, objects = %d; want NotFound and nothing stored", err, len(service.objects.objects))
	}
}

func TestSearchingAnUnknownIndexIsNotFound(t *testing.T) {
	service := newTestService(t, 1024)

	_, err := service.client.SearchDocuments(context.Background(), &gogiv1.SearchDocumentsRequest{
		IndexName: "nobody--nothing", Query: "stiffness matrix", TopK: 3, EmbeddingModel: "bge-small-en-v1.5",
	})

	if status.Code(err) != codes.NotFound {
		t.Errorf("err = %v; want NotFound", err)
	}
}

func TestSearchingAnIndexWithNothingIngestedFindsNothing(t *testing.T) {
	service := newTestService(t, 1024)

	response, err := service.client.SearchDocuments(context.Background(), &gogiv1.SearchDocumentsRequest{
		IndexName: "maria--aeronautics", Query: "stiffness matrix", TopK: 3, EmbeddingModel: "bge-small-en-v1.5",
	})

	if err != nil || len(response.GetChunks()) != 0 {
		t.Errorf("chunks = %v, err = %v; want none", response.GetChunks(), err)
	}
}

func TestSearchingNamesAModelTheApplicationCanUse(t *testing.T) {
	service := newTestService(t, 1024)

	tests := []struct {
		model string
		code  codes.Code
	}{{"", codes.InvalidArgument}, {"clip", codes.NotFound}, {"text-embedding-3-small", codes.FailedPrecondition}}
	for _, test := range tests {
		_, err := service.client.SearchDocuments(context.Background(), &gogiv1.SearchDocumentsRequest{
			IndexName: "maria--aeronautics", Query: "stiffness matrix", TopK: 3, EmbeddingModel: test.model,
		})
		if status.Code(err) != test.code {
			t.Errorf("searching with %q: err = %v, want %s", test.model, err, test.code)
		}
	}
}

// embedded records that the model embedded the document, as the ingestion worker does
func embedded(t *testing.T, service testService, documentID, model string) {
	t.Helper()
	_, err := service.pool.Exec(context.Background(), `INSERT INTO gogi_document_embeddings (index_id, document_id, model, chunk_count)
		SELECT id, $1, $2, 3 FROM gogi_indexes WHERE name = 'maria--aeronautics'`, documentID, model)
	if err != nil {
		t.Fatal(err)
	}
}

func TestAStoredDocumentIsIngestedLater(t *testing.T) {
	service := newTestService(t, 1024)
	stored := info("fem-notes")
	stored.GetInfo().EmbeddingModels = nil
	first, err := upload(t, service.client, stored, chunk("text"))
	if err != nil {
		t.Fatal(err)
	}

	_, err = service.client.IngestStoredDocument(context.Background(), &gogiv1.IngestStoredDocumentRequest{
		IndexName: "maria--aeronautics", DocumentId: "fem-notes"})
	if status.Code(err) != codes.InvalidArgument {
		t.Errorf("ingesting without a model: err = %v, want InvalidArgument", err)
	}

	response, err := service.client.IngestStoredDocument(context.Background(), &gogiv1.IngestStoredDocumentRequest{
		IndexName: "maria--aeronautics", DocumentId: "fem-notes", EmbeddingModels: []string{"all-MiniLM-L6-v2", "bge-small-en-v1.5"}})
	if err != nil {
		t.Fatal(err)
	}

	if len(service.queue.messages) != 1 {
		t.Fatalf("ingestions queued = %d, want 1", len(service.queue.messages))
	}
	config := service.queue.messages[0]
	if config.JobID != response.GetJobId() || config.JobID == first.GetJobId() || config.ObjectKey != "maria--aeronautics/fem-notes" ||
		config.Metadata["subject"] != "numerical-methods" || len(config.EmbeddingModels) != 2 {
		t.Fatalf("message = %+v", config)
	}
	// MiniLM reads less, so its chunks are shorter
	if config.EmbeddingModels[0].Model != "all-MiniLM-L6-v2" || config.EmbeddingModels[0].ChunkSize >= config.EmbeddingModels[1].ChunkSize {
		t.Errorf("models = %+v", config.EmbeddingModels)
	}
	document, err := service.client.GetDocument(context.Background(), &gogiv1.GetDocumentRequest{IndexName: "maria--aeronautics", DocumentId: "fem-notes"})
	if err != nil {
		t.Fatal(err)
	}
	if document.GetDocument().GetJobId() != response.GetJobId() || document.GetDocument().GetStatus() != string(utils.JobPending) {
		t.Errorf("document = %+v, want its new job", document.GetDocument())
	}
}

func TestIngestingAStoredDocumentStopsItsRunningIngestion(t *testing.T) {
	service := newTestService(t, 1024)
	first, err := upload(t, service.client, info("fem-notes"), chunk("text"))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.client.IngestStoredDocument(context.Background(), &gogiv1.IngestStoredDocumentRequest{
		IndexName: "maria--aeronautics", DocumentId: "fem-notes", EmbeddingModels: []string{"all-MiniLM-L6-v2"}}); err != nil {
		t.Fatal(err)
	}

	job, _ := service.client.GetDocumentIngestJob(context.Background(), &gogiv1.GetIngestDocumentJobRequest{JobId: first.GetJobId()})
	if job.GetStatus() != string(utils.JobCancelled) {
		t.Errorf("the running ingestion is %s, want CANCELLED", job.GetStatus())
	}
}

func TestDeletingADocumentDeletesWhatEveryModelEmbedded(t *testing.T) {
	service := newTestService(t, 1024)
	if _, err := upload(t, service.client, info("fem-notes"), chunk("text")); err != nil {
		t.Fatal(err)
	}
	embedded(t, service, "fem-notes", "bge-small-en-v1.5")
	embedded(t, service, "fem-notes", "all-MiniLM-L6-v2")

	document, err := service.client.GetDocument(context.Background(), &gogiv1.GetDocumentRequest{IndexName: "maria--aeronautics", DocumentId: "fem-notes"})
	if err != nil {
		t.Fatal(err)
	}
	if got := document.GetDocument().GetEmbeddingModels(); len(got) != 2 || got[0] != "all-MiniLM-L6-v2" || got[1] != "bge-small-en-v1.5" {
		t.Errorf("embedding models = %v", got)
	}

	if _, err := service.client.DeleteDocument(context.Background(), &gogiv1.DeleteDocumentRequest{IndexName: "maria--aeronautics", DocumentId: "fem-notes"}); err != nil {
		t.Fatal(err)
	}

	got := service.vectors.deletedFrom()
	if len(got) != 2 || got[0] != "maria--aeronautics.all-MiniLM-L6-v2" || got[1] != "maria--aeronautics.bge-small-en-v1.5" {
		t.Errorf("collections deleted from = %v", got)
	}
}

func TestReplacingADocumentsContentDeletesWhatModelsEmbedded(t *testing.T) {
	service := newTestService(t, 1024)
	if _, err := upload(t, service.client, info("fem-notes"), chunk("old text")); err != nil {
		t.Fatal(err)
	}
	embedded(t, service, "fem-notes", "bge-small-en-v1.5")
	replacing := info("fem-notes")
	replacing.GetInfo().Replace = true
	replacing.GetInfo().EmbeddingModels = nil

	if _, err := upload(t, service.client, replacing, chunk("new text")); err != nil {
		t.Fatal(err)
	}

	if got := service.vectors.deletedFrom(); len(got) != 1 || got[0] != "maria--aeronautics.bge-small-en-v1.5" {
		t.Errorf("chunks deleted from %v, want the old content's", got)
	}
	document, _ := service.client.GetDocument(context.Background(), &gogiv1.GetDocumentRequest{IndexName: "maria--aeronautics", DocumentId: "fem-notes"})
	if len(document.GetDocument().GetEmbeddingModels()) != 0 {
		t.Errorf("the new content is embedded by %v before it's ingested", document.GetDocument().GetEmbeddingModels())
	}
}

func TestASearchEmbedsTheQueryAndSearchesTheModelsCollection(t *testing.T) {
	service := newTestService(t, 1024)
	service.vectors.found = []models.VectorSearchResult{{ChunkID: "c-1", DocumentID: "fem-notes", Text: "shape functions",
		Score: 0.25, Metadata: map[string]string{"document_id": "fem-notes", "page": "3"}}}

	response, err := service.client.SearchDocuments(context.Background(), &gogiv1.SearchDocumentsRequest{
		IndexName: "maria--aeronautics", Query: "shape functions", TopK: 3, EmbeddingModel: "all-MiniLM-L6-v2",
		DocumentIds: []string{"fem-notes"}, MetadataFilter: map[string]string{"subject": "numerical-methods"},
	})

	if err != nil {
		t.Fatal(err)
	}
	chunks := response.GetChunks()
	if len(chunks) != 1 || chunks[0].GetContent() != "shape functions" || chunks[0].GetIndexName() != "maria--aeronautics" ||
		chunks[0].GetMetadata()["page"] != "3" || chunks[0].GetScore() != 0.25 {
		t.Errorf("chunks = %v", chunks)
	}
	query := service.vectors.queries[0]
	if query.collection != "maria--aeronautics.all-MiniLM-L6-v2" || query.topK != 3 || len(query.embedding) != 2 {
		t.Errorf("query = %+v", query)
	}
	conditions, ok := query.where["$and"].([]map[string]any)
	if !ok || len(conditions) != 2 || conditions[0]["subject"] != "numerical-methods" {
		t.Errorf("where = %v, want the metadata and the documents", query.where)
	}
}

func TestAnIngestionThatCannotBeQueuedFailsItsJob(t *testing.T) {
	service := newTestService(t, 1024)
	service.queue.err = errors.New("the queue is down")

	_, err := upload(t, service.client, info("fem-notes"), chunk("text"))

	if status.Code(err) != codes.Unavailable {
		t.Fatalf("err = %v, want Unavailable", err)
	}
	document, err := service.client.GetDocument(context.Background(), &gogiv1.GetDocumentRequest{IndexName: "maria--aeronautics", DocumentId: "fem-notes"})
	if err != nil {
		t.Fatal(err)
	}
	if document.GetDocument().GetStatus() != string(utils.JobFailed) {
		t.Errorf("the document's ingestion is %s, want FAILED", document.GetDocument().GetStatus())
	}
}
