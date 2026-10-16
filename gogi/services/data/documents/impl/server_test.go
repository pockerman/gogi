package impl

import (
	"context"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"testing"

	"gogi/gogi/documents/workflows"
	gogiv1 "gogi/gogi/gogi/v1"
	"gogi/gogi/storage/postgres"

	"github.com/google/uuid"
	"go.temporal.io/sdk/client"
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

func (f *fakeObjects) get(key string) ([]byte, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	data, ok := f.objects[key]
	return data, ok
}

// fakeWorkflows records the ingestion workflows started and completes the others at once
type fakeWorkflows struct {
	mu        sync.Mutex
	ingestion []workflows.IngestDocumentWorkflowConfig
}

func (f *fakeWorkflows) ExecuteWorkflow(_ context.Context, options client.StartWorkflowOptions, _ interface{},
	args ...interface{}) (client.WorkflowRun, error) {
	if config, ok := args[0].(workflows.IngestDocumentWorkflowConfig); ok {
		f.mu.Lock()
		f.ingestion = append(f.ingestion, config)
		f.mu.Unlock()
	}
	return fakeRun{id: options.ID}, nil
}

type fakeRun struct{ id string }

func (r fakeRun) GetID() string                          { return r.id }
func (r fakeRun) GetRunID() string                       { return r.id }
func (r fakeRun) Get(context.Context, interface{}) error { return nil }
func (r fakeRun) GetWithOptions(context.Context, interface{}, client.WorkflowRunGetOptions) error {
	return nil
}

type testService struct {
	client    gogiv1.DocumentServerClient
	objects   *fakeObjects
	workflows *fakeWorkflows
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

	service := testService{objects: &fakeObjects{objects: map[string][]byte{}}, workflows: &fakeWorkflows{}}
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	gogiv1.RegisterDocumentServerServer(server, NewDocumentsServer(service.workflows, pool, service.objects, maxUploadBytes))
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
		IndexName:   "maria--aeronautics",
		DocumentId:  documentID,
		Filename:    "fe-basics.txt",
		ContentType: "text/plain",
		Metadata:    map[string]string{"subject": "numerical-methods"},
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

func TestUploadDocumentStoresTheContentAndPassesItsKeyToTheWorkflow(t *testing.T) {
	service := newTestService(t, 1024)

	response, err := upload(t, service.client, info("fe-basics"), chunk("The finite element method "), chunk("divides a domain "), chunk("into elements."))
	if err != nil {
		t.Fatal(err)
	}

	content, ok := service.objects.get("gogi-documents/maria--aeronautics/fe-basics")
	if !ok || string(content) != "The finite element method divides a domain into elements." {
		t.Errorf("stored content = %q", content)
	}
	if len(service.workflows.ingestion) != 1 {
		t.Fatalf("workflows started = %d, want 1", len(service.workflows.ingestion))
	}
	config := service.workflows.ingestion[0]
	if config.Bucket != "gogi-documents" || config.ObjectKey != "maria--aeronautics/fe-basics" || config.JobId != response.GetJobId() {
		t.Errorf("workflow config = %+v", config)
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
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := newTestService(t, 1024)

			_, err := upload(t, service.client, test.parts...)

			if status.Code(err) != test.code || !strings.Contains(status.Convert(err).Message(), test.message) {
				t.Errorf("err = %v, want %s containing %q", err, test.code, test.message)
			}
			if len(service.objects.objects) != 0 || len(service.workflows.ingestion) != 0 {
				t.Errorf("objects = %d, workflows = %d; the refused upload left them behind",
					len(service.objects.objects), len(service.workflows.ingestion))
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
