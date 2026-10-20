package impl

import (
	"context"
	gogiv1 "gogi/gogi/gogi/v1"

	"google.golang.org/grpc"
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

func (p *DocumentsProxy) IngestStoredDocument(ctx context.Context,
	req *gogiv1.IngestStoredDocumentRequest) (*gogiv1.IngestDocumentJobResponse, error) {
	return p.proxy.ForwardIngestStoredDocument(ctx, req)
}

func (p *DocumentsProxy) UploadDocument(stream grpc.ClientStreamingServer[gogiv1.UploadDocumentRequest,
	gogiv1.IngestDocumentJobResponse]) error {
	return p.proxy.ForwardUploadDocument(stream)
}

func (p *DocumentsProxy) GetDocumentIngestJob(ctx context.Context, req *gogiv1.GetIngestDocumentJobRequest) (*gogiv1.IngestDocumentJobResponse, error) {
	return p.proxy.ForwardGetDocumentIngestJob(ctx, req)
}

func (p *DocumentsProxy) SearchDocuments(ctx context.Context, req *gogiv1.SearchDocumentsRequest) (*gogiv1.SearchDocumentsResponse, error) {
	return p.proxy.ForwardSearchDocuments(ctx, req)
}

func (p *DocumentsProxy) ListDocumentSections(ctx context.Context, req *gogiv1.ListDocumentSectionsRequest) (*gogiv1.ListDocumentSectionsResponse, error) {
	return p.proxy.ForwardListDocumentSections(ctx, req)
}

func (p *DocumentsProxy) GetDocumentSection(ctx context.Context, req *gogiv1.GetDocumentSectionRequest) (*gogiv1.GetDocumentSectionResponse, error) {
	return p.proxy.ForwardGetDocumentSection(ctx, req)
}

func (p *DocumentsProxy) DownloadDocument(req *gogiv1.DownloadDocumentRequest, stream grpc.ServerStreamingServer[gogiv1.DownloadDocumentResponse]) error {
	return p.proxy.ForwardDownloadDocument(req, stream)
}
