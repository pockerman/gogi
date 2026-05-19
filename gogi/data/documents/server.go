package documents

import (
	"context"
	gogiv1 "gogi/gogi/gogi/v1"

	log "github.com/sirupsen/logrus"
)

type DocumentsServer struct {
	gogiv1.UnimplementedDocumentServerServer
}

func (s *DocumentsServer) ListDocuments(ctx context.Context, req *gogiv1.ListDocumentsRequest) (*gogiv1.ListDocumentsResponse, error) {
	indexName := req.GetIndexName()

	// Your logic here
	log.Infof("Listing documents for %s", indexName)

	return &gogiv1.ListDocumentsResponse{
		Documents: []*gogiv1.DocumentMetadata{{Name: "example"}},
	}, nil
}

func (s *DocumentsServer) GetDocument(ctx context.Context, req *gogiv1.GetDocumentRequest) (*gogiv1.GetDocumentResponse, error) {
	indexName := req.GetIndexName()
	documentID := req.GetDocumentId()
	log.Infof("Getting document %s for index %s", documentID, indexName)

	// Your logic here

	return &gogiv1.GetDocumentResponse{
		Document: &gogiv1.DocumentMetadata{Name: "example"},
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
