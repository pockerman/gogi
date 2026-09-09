package impl

import (
	"context"
	gogiv1 "gogi/gogi/gogi/v1"
	"gogi/gogi/utils"

	"gogi/gogi/storage/postgres"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ToolServer implements the ToolServer service.
// The server provides the following endpoints
//  RegisterTool(RegisterToolRequest) returns (RegisterToolResponse);
//  DiscoverTools(DiscoverToolsRequest) returns (DiscoverToolsResponse);
//  ExecuteTool(ExecuteToolRequest) returns (ExecuteToolResponse);
//  ValidateTool(ValidateToolRequest) returns (ValidateToolResponse);
//
//  // long-running tools start here, callers poll GetTask.
//  ExecuteToolAsync(ExecuteToolRequest) returns (ExecuteToolAsyncResponse);
//  GetTask(GetTaskRequest) returns (GetTaskResponse);
//
//  // register an external MCP server; its tools get imported
//  // into the registry under the caller's namespace.
//  RegisterMcpServer(RegisterMcpServerRequest) returns (RegisterMcpServerResponse);

type ToolServer struct {
	gogiv1.UnimplementedToolServerServer
	gogiToolsRepo postgres.GogiToolsRepository
}

func NewToolServer(dbClient *pgxpool.Pool) *ToolServer {
	return &ToolServer{
		gogiToolsRepo: *postgres.NewGogiToolsRepository(dbClient),
	}
}

func (s *ToolServer) RegisterTool(ctx context.Context, req *gogiv1.RegisterToolRequest) (*gogiv1.RegisterToolResponse, error) {

	return &gogiv1.RegisterToolResponse{
		Name:    req.GetTool().GetName(),
		Version: req.GetTool().GetVersion(),
		Status:  "registered",
	}, nil
}

func (s *ToolServer) DiscoverTools(ctx context.Context, req *gogiv1.DiscoverToolsRequest) (*gogiv1.DiscoverToolsResponse, error) {

	tools := []*gogiv1.ToolServiceDefinition{
		{
			Name:        "web_search",
			Version:     "1.0.0",
			Owner:       "gogi",
			Description: "Searches the web for a given query.",
			Behavior: &gogiv1.ToolBehavior{
				IsReadOnly:   true,
				IsIdempotent: true,
			},
			Capabilities: []string{"search"},
			Tags:         []string{"web"},
		},
	}

	return &gogiv1.DiscoverToolsResponse{
		Tools: tools,
		RelevanceScores: map[string]float32{
			"web_search": 1.0,
		},
	}, nil
}

func (s *ToolServer) ExecuteTool(ctx context.Context, req *gogiv1.ExecuteToolRequest) (*gogiv1.ExecuteToolResponse, error) {

	return &gogiv1.ExecuteToolResponse{
		Success:         true,
		ResultJson:      "{}",
		ExecutionTimeMs: 0,
	}, nil
}

func (s *ToolServer) ValidateTool(ctx context.Context, req *gogiv1.ValidateToolRequest) (*gogiv1.ValidateToolResponse, error) {

	return &gogiv1.ValidateToolResponse{
		Valid: true,
	}, nil
}

func (s *ToolServer) ExecuteToolAsync(ctx context.Context, req *gogiv1.ExecuteToolRequest) (*gogiv1.ExecuteToolAsyncResponse, error) {

	return &gogiv1.ExecuteToolAsyncResponse{
		TaskId: utils.NewUUIDString(),
		Status: "pending",
	}, nil
}

func (s *ToolServer) GetTask(ctx context.Context, req *gogiv1.GetTaskRequest) (*gogiv1.GetTaskResponse, error) {

	return &gogiv1.GetTaskResponse{
		TaskId:     req.GetTaskId(),
		Status:     "pending",
		ResultJson: "{}",
	}, nil
}

func (s *ToolServer) RegisterMcpServer(ctx context.Context, req *gogiv1.RegisterMcpServerRequest) (*gogiv1.RegisterMcpServerResponse, error) {

	return &gogiv1.RegisterMcpServerResponse{
		ImportedToolNames: []string{},
	}, nil
}
