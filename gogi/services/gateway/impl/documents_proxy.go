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
