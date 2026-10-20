package impl

import (
	"context"
	gogiv1 "gogi/gogi/gogi/v1"
	"gogi/gogi/observability"
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
		// the services called continue the trace of the request
		grpc.WithChainUnaryInterceptor(observability.UnaryClientInterceptor()),
		grpc.WithChainStreamInterceptor(observability.StreamClientInterceptor()),
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
		// the services called continue the trace of the request
		grpc.WithChainUnaryInterceptor(observability.UnaryClientInterceptor()),
		grpc.WithChainStreamInterceptor(observability.StreamClientInterceptor()),
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
		// the services called continue the trace of the request
		grpc.WithChainUnaryInterceptor(observability.UnaryClientInterceptor()),
		grpc.WithChainStreamInterceptor(observability.StreamClientInterceptor()),
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
		// the services called continue the trace of the request
		grpc.WithChainUnaryInterceptor(observability.UnaryClientInterceptor()),
		grpc.WithChainStreamInterceptor(observability.StreamClientInterceptor()),
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
		// the services called continue the trace of the request
		grpc.WithChainUnaryInterceptor(observability.UnaryClientInterceptor()),
		grpc.WithChainStreamInterceptor(observability.StreamClientInterceptor()),
	)

	if err != nil {
		return nil, err
	}

	defer conn.Close()

	client := gogiv1.NewDocumentServerClient(conn)
	return client.IngestDocument(ctx, req)
}

// ForwardUploadDocument relays the upload's parts as they arrive, without buffering the document
func (p *GenericGRPCProxy) ForwardUploadDocument(stream grpc.ClientStreamingServer[gogiv1.UploadDocumentRequest,
	gogiv1.IngestDocumentJobResponse]) error {

	conn, err := p.buildConnection("documents")
	if err != nil {
		return err
	}
	defer conn.Close()

	client := gogiv1.NewDocumentServerClient(conn)
	upstream, err := client.UploadDocument(stream.Context())
	if err != nil {
		return err
	}

	for {
		part, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		// io.EOF means the service ended the upload; CloseAndRecv returns why
		if err := upstream.Send(part); err == io.EOF {
			break
		} else if err != nil {
			return err
		}
	}

	response, err := upstream.CloseAndRecv()
	if err != nil {
		return err
	}
	return stream.SendAndClose(response)
}

func (p *GenericGRPCProxy) ForwardGetDocumentIngestJob(ctx context.Context, req *gogiv1.GetIngestDocumentJobRequest) (*gogiv1.IngestDocumentJobResponse, error) {
	target, err := p.registry.ResolveService("documents")
	if err != nil {
		return nil, err
	}

	conn, err := grpc.Dial(
		target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		// the services called continue the trace of the request
		grpc.WithChainUnaryInterceptor(observability.UnaryClientInterceptor()),
		grpc.WithChainStreamInterceptor(observability.StreamClientInterceptor()),
	)

	if err != nil {
		return nil, err
	}

	defer conn.Close()

	client := gogiv1.NewDocumentServerClient(conn)
	return client.GetDocumentIngestJob(ctx, req)
}

func (p *GenericGRPCProxy) ForwardSearchDocuments(ctx context.Context, req *gogiv1.SearchDocumentsRequest) (*gogiv1.SearchDocumentsResponse, error) {
	conn, err := p.buildConnection("documents")
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	client := gogiv1.NewDocumentServerClient(conn)
	return client.SearchDocuments(ctx, req)
}

func (p *GenericGRPCProxy) ForwardIngestStoredDocument(ctx context.Context, req *gogiv1.IngestStoredDocumentRequest) (*gogiv1.IngestDocumentJobResponse, error) {
	conn, err := p.buildConnection("documents")
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	return gogiv1.NewDocumentServerClient(conn).IngestStoredDocument(ctx, req)
}

func (p *GenericGRPCProxy) ForwardListDocumentSections(ctx context.Context, req *gogiv1.ListDocumentSectionsRequest) (*gogiv1.ListDocumentSectionsResponse, error) {
	conn, err := p.buildConnection("documents")
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	return gogiv1.NewDocumentServerClient(conn).ListDocumentSections(ctx, req)
}

func (p *GenericGRPCProxy) ForwardGetDocumentSection(ctx context.Context, req *gogiv1.GetDocumentSectionRequest) (*gogiv1.GetDocumentSectionResponse, error) {
	conn, err := p.buildConnection("documents")
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	return gogiv1.NewDocumentServerClient(conn).GetDocumentSection(ctx, req)
}

// ForwardDownloadDocument relays the download's parts as they arrive
func (p *GenericGRPCProxy) ForwardDownloadDocument(req *gogiv1.DownloadDocumentRequest,
	stream grpc.ServerStreamingServer[gogiv1.DownloadDocumentResponse]) error {

	conn, err := p.buildConnection("documents")
	if err != nil {
		return err
	}
	defer conn.Close()

	upstream, err := gogiv1.NewDocumentServerClient(conn).DownloadDocument(stream.Context(), req)
	if err != nil {
		return err
	}
	for {
		part, err := upstream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if err := stream.Send(part); err != nil {
			return err
		}
	}
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

func (p *GenericGRPCProxy) ForwardSetIndexMember(
	ctx context.Context,
	req *gogiv1.SetIndexMemberRequest,
) (*gogiv1.IndexResponse, error) {

	conn, err := p.buildConnection("indexes")
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	client := gogiv1.NewIndexServiceClient(conn)
	return client.SetIndexMember(ctx, req)
}

func (p *GenericGRPCProxy) ForwardRemoveIndexMember(
	ctx context.Context,
	req *gogiv1.RemoveIndexMemberRequest,
) (*gogiv1.IndexResponse, error) {

	conn, err := p.buildConnection("indexes")
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	client := gogiv1.NewIndexServiceClient(conn)
	return client.RemoveIndexMember(ctx, req)
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

func (p *GenericGRPCProxy) ForwardRegisterEmbeddingModel(ctx context.Context, req *gogiv1.RegisterEmbeddingModelRequest) (*gogiv1.EmbeddingModel, error) {
	conn, err := p.buildConnection("llms")
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	return gogiv1.NewLLMModelServerClient(conn).RegisterEmbeddingModel(ctx, req)
}

func (p *GenericGRPCProxy) ForwardListEmbeddingModels(ctx context.Context, req *gogiv1.ListEmbeddingModelsRequest) (*gogiv1.ListEmbeddingModelsResponse, error) {
	conn, err := p.buildConnection("llms")
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	return gogiv1.NewLLMModelServerClient(conn).ListEmbeddingModels(ctx, req)
}

func (p *GenericGRPCProxy) ForwardEmbed(ctx context.Context, req *gogiv1.EmbedRequest) (*gogiv1.EmbedResponse, error) {
	conn, err := p.buildConnection("llms")
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	return gogiv1.NewLLMModelServerClient(conn).Embed(ctx, req)
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

func (p *GenericGRPCProxy) ForwardListSessions(ctx context.Context, req *gogiv1.ListSessionsRequest) (*gogiv1.ListSessionsResponse, error) {
	conn, _ := p.buildConnection("llm-sessions")
	defer conn.Close()

	client := gogiv1.NewLLMSessionServerClient(conn)
	return client.ListSessions(ctx, req)
}

func (p *GenericGRPCProxy) ForwardAddMessages(ctx context.Context, req *gogiv1.AddMessagesRequest) (*gogiv1.AddMessagesResponse, error) {
	conn, _ := p.buildConnection("llm-sessions")
	defer conn.Close()

	client := gogiv1.NewLLMSessionServerClient(conn)
	return client.AddMessages(ctx, req)

}

func (p *GenericGRPCProxy) ForwardGetMessages(ctx context.Context, req *gogiv1.GetMessagesRequest) (*gogiv1.GetMessagesResponse, error) {
	conn, _ := p.buildConnection("llm-sessions")
	defer conn.Close()

	client := gogiv1.NewLLMSessionServerClient(conn)
	return client.GetMessages(ctx, req)
}

func (p *GenericGRPCProxy) ForwardDeleteSession(ctx context.Context, req *gogiv1.DeleteSessionRequest) (*gogiv1.DeleteSessionResponse, error) {
	conn, _ := p.buildConnection("llm-sessions")
	defer conn.Close()

	client := gogiv1.NewLLMSessionServerClient(conn)
	return client.DeleteSession(ctx, req)
}

func (p *GenericGRPCProxy) ForwardSaveMemory(ctx context.Context, req *gogiv1.SaveMemoryRequest) (*gogiv1.SaveMemoryResponse, error) {
	conn, _ := p.buildConnection("llm-sessions")
	defer conn.Close()

	client := gogiv1.NewLLMSessionServerClient(conn)
	return client.SaveMemory(ctx, req)
}

func (p *GenericGRPCProxy) ForwardGetMemory(ctx context.Context, req *gogiv1.GetMemoryRequest) (*gogiv1.GetMemoryResponse, error) {
	conn, _ := p.buildConnection("llm-sessions")
	defer conn.Close()

	client := gogiv1.NewLLMSessionServerClient(conn)
	return client.GetMemory(ctx, req)
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

// ======= Workflows ==========

func (p *GenericGRPCProxy) ForwardRegisterWorkflow(ctx context.Context, req *gogiv1.RegisterWorkflowRequest) (*gogiv1.RegisterWorkflowResponse, error) {
	conn, _ := p.buildConnection("workflows")
	defer conn.Close()

	client := gogiv1.NewWorkflowServerClient(conn)
	return client.RegisterWorkflow(ctx, req)
}

func (p *GenericGRPCProxy) ForwardGetWorkflow(ctx context.Context, req *gogiv1.GetWorkflowRequest) (*gogiv1.GetWorkflowResponse, error) {
	conn, _ := p.buildConnection("workflows")
	defer conn.Close()

	client := gogiv1.NewWorkflowServerClient(conn)
	return client.GetWorkflow(ctx, req)
}

func (p *GenericGRPCProxy) ForwardListWorkflows(ctx context.Context, req *gogiv1.ListWorkflowsRequest) (*gogiv1.ListWorkflowsResponse, error) {
	conn, _ := p.buildConnection("workflows")
	defer conn.Close()

	client := gogiv1.NewWorkflowServerClient(conn)
	return client.ListWorkflows(ctx, req)
}

func (p *GenericGRPCProxy) ForwardUpdateWorkflow(ctx context.Context, req *gogiv1.UpdateWorkflowRequest) (*gogiv1.UpdateWorkflowResponse, error) {
	conn, _ := p.buildConnection("workflows")
	defer conn.Close()

	client := gogiv1.NewWorkflowServerClient(conn)
	return client.UpdateWorkflow(ctx, req)
}

func (p *GenericGRPCProxy) ForwardDeleteWorkflow(ctx context.Context, req *gogiv1.DeleteWorkflowRequest) (*gogiv1.DeleteWorkflowResponse, error) {
	conn, _ := p.buildConnection("workflows")
	defer conn.Close()

	client := gogiv1.NewWorkflowServerClient(conn)
	return client.DeleteWorkflow(ctx, req)
}

func (p *GenericGRPCProxy) ForwardDeployWorkflow(ctx context.Context, req *gogiv1.DeployWorkflowRequest) (*gogiv1.DeployWorkflowResponse, error) {
	conn, _ := p.buildConnection("workflows")
	defer conn.Close()

	client := gogiv1.NewWorkflowServerClient(conn)
	return client.DeployWorkflow(ctx, req)
}

func (p *GenericGRPCProxy) ForwardGetDeploymentStatus(ctx context.Context, req *gogiv1.GetDeploymentStatusRequest) (*gogiv1.GetDeploymentStatusResponse, error) {
	conn, _ := p.buildConnection("workflows")
	defer conn.Close()

	client := gogiv1.NewWorkflowServerClient(conn)
	return client.GetDeploymentStatus(ctx, req)
}

func (p *GenericGRPCProxy) ForwardRollbackWorkflow(ctx context.Context, req *gogiv1.RollbackWorkflowRequest) (*gogiv1.RollbackWorkflowResponse, error) {
	conn, _ := p.buildConnection("workflows")
	defer conn.Close()

	client := gogiv1.NewWorkflowServerClient(conn)
	return client.RollbackWorkflow(ctx, req)
}

func (p *GenericGRPCProxy) ForwardRegisterRoute(ctx context.Context, req *gogiv1.RegisterRouteRequest) (*gogiv1.RegisterRouteResponse, error) {
	conn, _ := p.buildConnection("workflows")
	defer conn.Close()

	client := gogiv1.NewWorkflowServerClient(conn)
	return client.RegisterRoute(ctx, req)
}

func (p *GenericGRPCProxy) ForwardListRoutes(ctx context.Context, req *gogiv1.ListRoutesRequest) (*gogiv1.ListRoutesResponse, error) {
	conn, _ := p.buildConnection("workflows")
	defer conn.Close()

	client := gogiv1.NewWorkflowServerClient(conn)
	return client.ListRoutes(ctx, req)
}

func (p *GenericGRPCProxy) ForwardCreateJob(ctx context.Context, req *gogiv1.CreateJobRequest) (*gogiv1.CreateJobResponse, error) {
	conn, _ := p.buildConnection("workflows")
	defer conn.Close()

	client := gogiv1.NewWorkflowServerClient(conn)
	return client.CreateJob(ctx, req)
}

func (p *GenericGRPCProxy) ForwardGetJobStatus(ctx context.Context, req *gogiv1.GetJobStatusRequest) (*gogiv1.GetJobStatusResponse, error) {
	conn, _ := p.buildConnection("workflows")
	defer conn.Close()

	client := gogiv1.NewWorkflowServerClient(conn)
	return client.GetJobStatus(ctx, req)
}

func (p *GenericGRPCProxy) ForwardUpdateJobProgress(ctx context.Context, req *gogiv1.UpdateJobProgressRequest) (*gogiv1.UpdateJobProgressResponse, error) {
	conn, _ := p.buildConnection("workflows")
	defer conn.Close()

	client := gogiv1.NewWorkflowServerClient(conn)
	return client.UpdateJobProgress(ctx, req)
}

func (p *GenericGRPCProxy) ForwardSaveJobCheckpoint(ctx context.Context, req *gogiv1.SaveJobCheckpointRequest) (*gogiv1.SaveJobCheckpointResponse, error) {
	conn, _ := p.buildConnection("workflows")
	defer conn.Close()

	client := gogiv1.NewWorkflowServerClient(conn)
	return client.SaveJobCheckpoint(ctx, req)
}

func (p *GenericGRPCProxy) ForwardCompleteJob(ctx context.Context, req *gogiv1.CompleteJobRequest) (*gogiv1.CompleteJobResponse, error) {
	conn, _ := p.buildConnection("workflows")
	defer conn.Close()

	client := gogiv1.NewWorkflowServerClient(conn)
	return client.CompleteJob(ctx, req)
}

func (p *GenericGRPCProxy) ForwardFailJob(ctx context.Context, req *gogiv1.FailJobRequest) (*gogiv1.FailJobResponse, error) {
	conn, _ := p.buildConnection("workflows")
	defer conn.Close()

	client := gogiv1.NewWorkflowServerClient(conn)
	return client.FailJob(ctx, req)
}

func (p *GenericGRPCProxy) ForwardCancelJob(ctx context.Context, req *gogiv1.CancelJobRequest) (*gogiv1.CancelJobResponse, error) {
	conn, _ := p.buildConnection("workflows")
	defer conn.Close()

	client := gogiv1.NewWorkflowServerClient(conn)
	return client.CancelJob(ctx, req)
}

// ======= Guardrails ==========

func (p *GenericGRPCProxy) ForwardValidateInput(ctx context.Context, req *gogiv1.ValidateInputRequest) (*gogiv1.ValidateInputResponse, error) {
	conn, _ := p.buildConnection("guardrails")
	defer conn.Close()

	client := gogiv1.NewGuardrailsServiceClient(conn)
	return client.ValidateInput(ctx, req)
}

func (p *GenericGRPCProxy) ForwardFilterOutput(ctx context.Context, req *gogiv1.FilterOutputRequest) (*gogiv1.FilterOutputResponse, error) {
	conn, _ := p.buildConnection("guardrails")
	defer conn.Close()

	client := gogiv1.NewGuardrailsServiceClient(conn)
	return client.FilterOutput(ctx, req)
}

func (p *GenericGRPCProxy) ForwardCheckPolicy(ctx context.Context, req *gogiv1.CheckPolicyRequest) (*gogiv1.CheckPolicyResponse, error) {
	conn, _ := p.buildConnection("guardrails")
	defer conn.Close()

	client := gogiv1.NewGuardrailsServiceClient(conn)
	return client.CheckPolicy(ctx, req)
}

func (p *GenericGRPCProxy) ForwardReportViolation(ctx context.Context, req *gogiv1.ReportViolationRequest) (*gogiv1.ReportViolationResponse, error) {
	conn, _ := p.buildConnection("guardrails")
	defer conn.Close()

	client := gogiv1.NewGuardrailsServiceClient(conn)
	return client.ReportViolation(ctx, req)
}
