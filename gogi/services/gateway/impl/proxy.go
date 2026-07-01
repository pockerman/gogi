package impl

import (
	"context"
	gogiv1 "gogi/gogi/gogi/v1"
	"io"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

//
// Generic proxy that routes gRPC calls to platform services.
// Routes based on x-target-service metadata from request context.
// Maintains Protocol Buffer efficiency by forwarding binary data.

type GenericGRPCProxy struct {
	registry      *ServiceRegistry
	stubFactories map[string]func() (interface{}, error) // Map of service names to functions that create gRPC client stubs``
}

func NewGenericProxy(registry *ServiceRegistry) *GenericGRPCProxy {
	return &GenericGRPCProxy{registry: registry}
}

func (p *GenericGRPCProxy) buildConnection(clientName string) (*grpc.ClientConn, error) {

	target, err := p.registry.ResolveService(clientName)
	if err != nil {
		return nil, err
	}

	conn, err := grpc.Dial(
		target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)

	if err != nil {
		return nil, err
	}

	return conn, nil
}

//======= Documents ==========

func (p *GenericGRPCProxy) ForwardListDocuments(
	ctx context.Context,
	req *gogiv1.ListDocumentsRequest,
) (*gogiv1.ListDocumentsResponse, error) {

	target, err := p.registry.ResolveService("documents")
	if err != nil {
		return nil, err
	}

	conn, err := grpc.Dial(
		target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)

	if err != nil {
		return nil, err
	}

	defer conn.Close()

	client := gogiv1.NewDocumentServerClient(conn)
	return client.ListDocuments(ctx, req)
}

func (p *GenericGRPCProxy) ForwardGetDocument(
	ctx context.Context,
	req *gogiv1.GetDocumentRequest,
) (*gogiv1.GetDocumentResponse, error) {

	target, err := p.registry.ResolveService("documents")
	if err != nil {
		return nil, err
	}

	conn, err := grpc.Dial(
		target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)

	if err != nil {
		return nil, err
	}

	defer conn.Close()

	client := gogiv1.NewDocumentServerClient(conn)
	return client.GetDocument(ctx, req)
}

func (p *GenericGRPCProxy) ForwardDeleteDocument(
	ctx context.Context,
	req *gogiv1.DeleteDocumentRequest,
) (*gogiv1.DeleteDocumentResponse, error) {

	target, err := p.registry.ResolveService("documents")
	if err != nil {
		return nil, err
	}

	conn, err := grpc.Dial(
		target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)

	if err != nil {
		return nil, err
	}

	defer conn.Close()

	client := gogiv1.NewDocumentServerClient(conn)
	return client.DeleteDocument(ctx, req)
}

func (p *GenericGRPCProxy) ForwardIngestDocument(ctx context.Context, req *gogiv1.IngestDocumentRequest) (*gogiv1.IngestDocumentJobResponse, error) {

	target, err := p.registry.ResolveService("documents")
	if err != nil {
		return nil, err
	}

	conn, err := grpc.Dial(
		target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)

	if err != nil {
		return nil, err
	}

	defer conn.Close()

	client := gogiv1.NewDocumentServerClient(conn)
	return client.IngestDocument(ctx, req)
}

func (p *GenericGRPCProxy) ForwardGetDocumentIngestJob(ctx context.Context, req *gogiv1.GetIngestDocumentJobRequest) (*gogiv1.IngestDocumentJobResponse, error) {
	target, err := p.registry.ResolveService("documents")
	if err != nil {
		return nil, err
	}

	conn, err := grpc.Dial(
		target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)

	if err != nil {
		return nil, err
	}

	defer conn.Close()

	client := gogiv1.NewDocumentServerClient(conn)
	return client.GetDocumentIngestJob(ctx, req)
}

//======= Indexes ==========

func (p *GenericGRPCProxy) ForwardCreateIndex(
	ctx context.Context,
	req *gogiv1.CreateIndexRequest,
) (*gogiv1.IndexResponse, error) {

	conn, _ := p.buildConnection("indexes")
	defer conn.Close()

	client := gogiv1.NewIndexServiceClient(conn)
	return client.CreateIndex(ctx, req)
}

func (p *GenericGRPCProxy) ForwardGetIndexByName(
	ctx context.Context,
	req *gogiv1.GetIndexByNameRequest,
) (*gogiv1.IndexResponse, error) {

	conn, _ := p.buildConnection("indexes")
	defer conn.Close()

	client := gogiv1.NewIndexServiceClient(conn)
	return client.GetIndexByName(ctx, req)
}

func (p *GenericGRPCProxy) ForwardGetIndexById(
	ctx context.Context,
	req *gogiv1.GetIndexByIdRequest,
) (*gogiv1.IndexResponse, error) {

	conn, _ := p.buildConnection("indexes")
	defer conn.Close()

	client := gogiv1.NewIndexServiceClient(conn)
	return client.GetIndexById(ctx, req)
}

func (p *GenericGRPCProxy) ForwardListIndexes(
	ctx context.Context,
	req *gogiv1.ListIndexesRequest,
) (*gogiv1.ListIndexesResponse, error) {

	conn, _ := p.buildConnection("indexes")
	defer conn.Close()

	client := gogiv1.NewIndexServiceClient(conn)
	return client.ListIndexes(ctx, req)
}

func (p *GenericGRPCProxy) ForwardDeleteIndexById(
	ctx context.Context,
	req *gogiv1.DeleteIndexByIdRequest,
) (*gogiv1.DeleteIndexResponse, error) {

	conn, _ := p.buildConnection("indexes")
	defer conn.Close()

	client := gogiv1.NewIndexServiceClient(conn)
	return client.DeleteIndexById(ctx, req)
}

func (p *GenericGRPCProxy) ForwardDeleteIndexByName(
	ctx context.Context,
	req *gogiv1.DeleteIndexByNameRequest,
) (*gogiv1.DeleteIndexResponse, error) {

	conn, _ := p.buildConnection("indexes")
	defer conn.Close()

	client := gogiv1.NewIndexServiceClient(conn)
	return client.DeleteIndexByName(ctx, req)
}

func (p *GenericGRPCProxy) ForwardDeleteOwnerIndexes(
	ctx context.Context,
	req *gogiv1.DeleteOwnerIndexesRequest,
) (*gogiv1.DeleteIndexResponse, error) {

	conn, _ := p.buildConnection("indexes")
	defer conn.Close()

	client := gogiv1.NewIndexServiceClient(conn)
	return client.DeleteOwnerIndexes(ctx, req)
}

// ======= LLMs ==========

func (p *GenericGRPCProxy) ForwardLLMRun(ctx context.Context, req *gogiv1.LLMRunRequest) (*gogiv1.LLMRunResponse, error) {

	conn, _ := p.buildConnection("llms")
	defer conn.Close()

	client := gogiv1.NewLLMModelServerClient(conn)
	return client.Run(ctx, req)
}

func (p *GenericGRPCProxy) ForwardLLMRunStream(ctx context.Context, req *gogiv1.LLMRunRequest,
	stream grpc.ServerStreamingServer[gogiv1.LLMStreamChunkResponse]) error {

	conn, err := p.buildConnection("llms")
	if err != nil {
		return err
	}
	defer conn.Close()

	client := gogiv1.NewLLMModelServerClient(conn)

	clientStream, err := client.RunStream(ctx, req)
	if err != nil {
		return err
	}

	for {
		chunk, err := clientStream.Recv()

		if err == io.EOF {
			return nil
		}

		if err != nil {
			return err
		}

		if err := stream.Send(chunk); err != nil {
			return err
		}
	}
}

func (p *GenericGRPCProxy) ForwardGetLLMProviders(ctx context.Context, req *gogiv1.GetLLMProvidersRequest) (*gogiv1.LLMProvidersResponse, error) {

	conn, _ := p.buildConnection("llms")
	defer conn.Close()

	client := gogiv1.NewLLMModelServerClient(conn)
	return client.GetLLMProviders(ctx, req)
}

func (p *GenericGRPCProxy) ForwardRegisterLLM(ctx context.Context, req *gogiv1.RegisterLLMRequest) (*gogiv1.RegisterLLMResponse, error) {

	conn, _ := p.buildConnection("llms")
	defer conn.Close()

	client := gogiv1.NewLLMModelServerClient(conn)
	return client.RegisterLLM(ctx, req)
}

func (p *GenericGRPCProxy) ForwardListRegisteredLLMs(ctx context.Context, req *gogiv1.ListRegisteredLLMsRequest) (*gogiv1.ListRegisteredLLMsResponse, error) {

	conn, _ := p.buildConnection("llms")
	defer conn.Close()

	client := gogiv1.NewLLMModelServerClient(conn)
	return client.ListRegisteredLLMs(ctx, req)
}

func (p *GenericGRPCProxy) ForwardGetLLMStatus(ctx context.Context, req *gogiv1.GetLLMStatusRequest) (*gogiv1.LLMStatusResponse, error) {
	conn, _ := p.buildConnection("llms")
	defer conn.Close()

	client := gogiv1.NewLLMModelServerClient(conn)
	return client.GetLLMStatus(ctx, req)
}

func (p *GenericGRPCProxy) ForwardGetLLMCapabilities(ctx context.Context, req *gogiv1.GetLLMCapabilitiesRequest) (*gogiv1.LLMCapabilitiesResponse, error) {
	conn, _ := p.buildConnection("llms")
	defer conn.Close()

	client := gogiv1.NewLLMModelServerClient(conn)
	return client.GetLLMCapabilities(ctx, req)
}

func (p *GenericGRPCProxy) ForwardListLLMs(ctx context.Context, req *gogiv1.ListLLMsRequest) (*gogiv1.ListLLMsResponse, error) {
	conn, _ := p.buildConnection("llms")
	defer conn.Close()

	client := gogiv1.NewLLMModelServerClient(conn)
	return client.ListLLMs(ctx, req)
}

// ======= Prompts ==========

func (p *GenericGRPCProxy) ForwardRegisterPrompt(ctx context.Context, req *gogiv1.PromptRegistrationRequest) (*gogiv1.PromptRegistrationResponse, error) {
	conn, _ := p.buildConnection("prompts")
	defer conn.Close()

	client := gogiv1.NewPromptServerClient(conn)
	return client.RegisterPrompt(ctx, req)
}

func (p *GenericGRPCProxy) ForwardGetPrompt(ctx context.Context, req *gogiv1.PromptGetRequest) (*gogiv1.PromptGetResponse, error) {
	conn, _ := p.buildConnection("prompts")
	defer conn.Close()

	client := gogiv1.NewPromptServerClient(conn)
	return client.GetPrompt(ctx, req)
}

func (p *GenericGRPCProxy) ForwardDeletePrompt(ctx context.Context, req *gogiv1.PromptDeleteRequest) (*gogiv1.PromptDeleteResponse, error) {
	conn, _ := p.buildConnection("prompts")
	defer conn.Close()

	client := gogiv1.NewPromptServerClient(conn)
	return client.DeletePrompt(ctx, req)
}

// ======= llm sessions ==========

func (p *GenericGRPCProxy) ForwardGetOrCreateSession(ctx context.Context, req *gogiv1.GetOrCreateSessionRequest) (*gogiv1.GetOrCreateSessionResponse, error) {
	conn, _ := p.buildConnection("llm-sessions")
	defer conn.Close()

	client := gogiv1.NewLLMSessionServerClient(conn)
	return client.GetOrCreateSession(ctx, req)
}

func (p *GenericGRPCProxy) ForwardAddMessages(ctx context.Context, req *gogiv1.AddMessagesRequest) (*gogiv1.AddMessagesResponse, error) {
	conn, _ := p.buildConnection("llm-sessions")
	defer conn.Close()

	client := gogiv1.NewLLMSessionServerClient(conn)
	return client.AddMessages(ctx, req)

}

func (p *GenericGRPCProxy) ForwardDeleteSession(ctx context.Context, req *gogiv1.DeleteSessionRequest) (*gogiv1.DeleteSessionResponse, error) {
	conn, _ := p.buildConnection("llm-sessions")
	defer conn.Close()

	client := gogiv1.NewLLMSessionServerClient(conn)
	return client.DeleteSession(ctx, req)
}

func (p *GenericGRPCProxy) ForwardDeleteMemory(ctx context.Context, req *gogiv1.DeleteMemoryRequest) (*gogiv1.DeleteMemoryResponse, error) {
	conn, _ := p.buildConnection("llm-sessions")
	defer conn.Close()

	client := gogiv1.NewLLMSessionServerClient(conn)
	return client.DeleteMemory(ctx, req)
}

func (p *GenericGRPCProxy) ForwardClearUserMemory(ctx context.Context, req *gogiv1.ClearUserMemoryRequest) (*gogiv1.ClearUserMemoryResponse, error) {
	conn, _ := p.buildConnection("llm-sessions")
	defer conn.Close()

	client := gogiv1.NewLLMSessionServerClient(conn)
	return client.ClearUserMemory(ctx, req)
}
