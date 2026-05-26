package impl

import (
	"context"
	gogiv1 "gogi/gogi/gogi/v1"

	"gogi/gogi/storage/vector"

	log "github.com/sirupsen/logrus"
)

type DocumentsServer struct {
	gogiv1.UnimplementedDocumentServerServer
	vectorStore vector.VectorStore
}

func NewDocumentsServer(vectorStore vector.VectorStore) *DocumentsServer {
	return &DocumentsServer{
		vectorStore: vectorStore,
	}
}

func (s *DocumentsServer) IngestDocument(ctx context.Context, req *gogiv1.IngestDocumentRequest) (*gogiv1.IngestDocumentJob, error) {

	indexName := req.GetIndexName()

	// Your logic here
	log.Infof("Ingesting document for %s", indexName)

	// in order to ingest the document we need an embedding strategy

	// the vector store will be called asynchronously by a worker, so we just return a job here
	vectorStore.Insert(indexName, "example-doc-id", []gogi.Chunk{{Text: "example chunk text"}}, [][]float64{{0.1, 0.2, 0.3}}, map[string]string{"source": "example"})

	return &gogiv1.IngestDocumentJob{
		IndexName: indexName,
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
