package impl

import (
	"bytes"
	"context"
	"io"

	"gogi/gogi/applications"
	gogiv1 "gogi/gogi/gogi/v1"
	"gogi/gogi/llm/providers"
	"gogi/gogi/utils"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// DocumentImageFetcher reads the images that messages refer to by document from the documents service
func DocumentImageFetcher(documentsAddress string) (providers.ImageFetcher, error) {
	conn, err := grpc.NewClient(documentsAddress,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(utils.MaxMessageBytes())))
	if err != nil {
		return nil, err
	}
	documents := gogiv1.NewDocumentServerClient(conn)

	return func(ctx context.Context, indexName, documentID string) ([]byte, string, error) {
		// the document is one of the application's whose call this is
		ctx = applications.OutgoingContext(ctx, applications.FromContext(ctx))
		stream, err := documents.DownloadDocument(ctx, &gogiv1.DownloadDocumentRequest{IndexName: indexName, DocumentId: documentID})
		if err != nil {
			return nil, "", err
		}
		var content bytes.Buffer
		contentType := ""
		for {
			message, err := stream.Recv()
			if err == io.EOF {
				return content.Bytes(), contentType, nil
			}
			if err != nil {
				return nil, "", err
			}
			if document := message.GetDocument(); document != nil {
				contentType = document.GetContentType()
			}
			content.Write(message.GetChunk())
		}
	}, nil
}
