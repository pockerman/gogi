package impl

import (
	"context"
	gogiv1 "gogi/gogi/gogi/v1"
)

type DocumentsProxy struct {
	gogiv1.UnimplementedDocumentServerServer
	proxy *GenericGRPCProxy
}

func (p *DocumentsProxy) ListDocuments(
	ctx context.Context,
	req *gogiv1.ListDocumentsRequest,
) (*gogiv1.ListDocumentsResponse, error) {

	return p.proxy.ForwardListDocuments(ctx, req)
}

func (p *DocumentsProxy) GetDocument(ctx context.Context, req *gogiv1.GetDocumentRequest) (*gogiv1.GetDocumentResponse, error) {
	return p.proxy.ForwardGetDocument(ctx, req)
}

func (p *DocumentsProxy) DeleteDocument(ctx context.Context, req *gogiv1.DeleteDocumentRequest) (*gogiv1.DeleteDocumentResponse, error) {
	return p.proxy.ForwardDeleteDocument(ctx, req)
}

func (p *DocumentsProxy) IngestDocument(ctx context.Context,
	req *gogiv1.IngestDocumentRequest) (*gogiv1.IngestDocumentJobResponse, error) {
	return p.proxy.ForwardIngestDocument(ctx, req)
}

func (p *DocumentsProxy) GetDocumentIngestJob(ctx context.Context, req *gogiv1.GetIngestDocumentJobRequest) (*gogiv1.IngestDocumentJobResponse, error) {
	return p.proxy.ForwardGetDocumentIngestJob(ctx, req)
}
