package impl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"gogi/gogi/credentials"
	gogiv1 "gogi/gogi/gogi/v1"
	"gogi/gogi/storage/postgres"
	"gogi/gogi/tools"
	"gogi/gogi/utils"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/santhosh-tekuri/jsonschema/v6"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// asyncTaskTimeout bounds the bookkeeping of an asynchronous call, on top of the
// tool's own timeout
const asyncTaskTimeout = 10 * time.Second

// ToolStore is the tool registry and the record of asynchronous tool calls
type ToolStore interface {
	InsertTool(ctx context.Context, tool *postgres.GogiTool) (*postgres.GogiTool, error)
	ListTools(ctx context.Context, namePrefix string) ([]*postgres.GogiTool, error)
	ListToolVersions(ctx context.Context, name string) ([]*postgres.GogiTool, error)
	CreateToolTask(ctx context.Context, task *postgres.GogiToolTask) (*postgres.GogiToolTask, error)
	GetToolTask(ctx context.Context, id string) (*postgres.GogiToolTask, error)
	UpdateToolTask(ctx context.Context, id, status string, resultJson *string, errorMessage string) error
}

// MCPToolLister lists the tools of an MCP server
type MCPToolLister interface {
	ListTools(ctx context.Context, serverURL, credential string) ([]*mcp.Tool, error)
}

// ToolServer implements the ToolServer service: the platform's registry of tools and
// the place where applications call them.
//
//   - RegisterTool adds a version of a tool to the registry; versions are immutable
//   - DiscoverTools finds tools by namespace, capabilities, tags and version constraint
//   - ValidateTool checks arguments against a tool's parameters without calling it
//   - ExecuteTool calls a tool, injecting its credential; ExecuteToolAsync starts the
//     call in the background, and GetTask reports its progress
//   - RegisterMcpServer imports the tools of an MCP server into the registry
type ToolServer struct {
	gogiv1.UnimplementedToolServerServer
	store           ToolStore
	executor        *tools.Executor
	credentialStore credentials.CredentialStore
	mcpLister       MCPToolLister
	// compiled parameter schemas by tool version; versions are immutable
	schemas sync.Map
	// asynchronous calls in progress
	tasks sync.WaitGroup
}

// NewToolServer creates the server. credentialStore holds the credentials of tools; it
// is nil if none is configured, in which case tools cannot reference credentials
func NewToolServer(dbClient *pgxpool.Pool, credentialStore credentials.CredentialStore,
	breaker *tools.CircuitBreaker, config tools.ExecutorConfig) *ToolServer {

	httpClient := &http.Client{}
	mcpAdapter := tools.NewMCPAdapter(httpClient)
	executor := tools.NewExecutor(credentialStore, breaker, tools.NewRateLimiter(),
		tools.NewHTTPAdapter(httpClient), mcpAdapter, config)
	return newToolServer(postgres.NewGogiToolsRepository(dbClient), executor, credentialStore, mcpAdapter)
}

func newToolServer(store ToolStore, executor *tools.Executor, credentialStore credentials.CredentialStore,
	mcpLister MCPToolLister) *ToolServer {
	return &ToolServer{
		store:           store,
		executor:        executor,
		credentialStore: credentialStore,
		mcpLister:       mcpLister,
	}
}

// Wait waits for the asynchronous calls in progress to finish
func (s *ToolServer) Wait() {
	s.tasks.Wait()
}

func (s *ToolServer) RegisterTool(ctx context.Context, req *gogiv1.RegisterToolRequest) (*gogiv1.RegisterToolResponse, error) {
	if req.GetTool() == nil {
		return nil, status.Error(codes.InvalidArgument, "a tool definition is required")
	}

	tool := toolFromProto(req.GetTool())
	if err := tools.NormalizeDefinition(tool); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}
	if err := s.checkCredential(ctx, tool.CredentialRef); err != nil {
		return nil, err
	}

	if err := s.insertTool(ctx, tool); err != nil {
		return nil, err
	}

	log.Infof("registered tool %s", tools.CircuitKey(tool))
	return &gogiv1.RegisterToolResponse{Name: tool.Name, Version: tool.Version, Status: "registered"}, nil
}

// insertTool adds a version of a tool to the registry, if it is compatible with the
// registered versions
func (s *ToolServer) insertTool(ctx context.Context, tool *postgres.GogiTool) error {
	registered, err := s.toolVersions(ctx, tool.Name)
	if err != nil {
		return err
	}
	if err := tools.CheckCompatibility(tool, registered); err != nil {
		if errors.Is(err, tools.ErrIncompatibleVersion) {
			return status.Errorf(codes.FailedPrecondition, "%v", err)
		}
		return status.Errorf(codes.Internal, "failed to check tool %s: %v", tool.Name, err)
	}

	_, err = s.store.InsertTool(ctx, tool)
	if errors.Is(err, utils.ErrToolVersionExists) {
		return status.Errorf(codes.AlreadyExists,
			"version %s of tool %s is already registered; registered versions cannot change, register a new version",
			tool.Version, tool.Name)
	}
	if err != nil {
		return status.Errorf(codes.Internal, "failed to register tool %s: %v", tool.Name, err)
	}
	return nil
}

// toolVersions returns the registered versions of a tool; none if it is not registered
func (s *ToolServer) toolVersions(ctx context.Context, name string) ([]*postgres.GogiTool, error) {
	versions, err := s.store.ListToolVersions(ctx, name)
	if errors.Is(err, utils.ErrToolNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get tool %s: %v", name, err)
	}
	return versions, nil
}

// checkCredential checks that the credential store holds the credential
func (s *ToolServer) checkCredential(ctx context.Context, credentialRef string) error {
	if credentialRef == "" {
		return nil
	}
	if s.credentialStore == nil {
		return status.Errorf(codes.FailedPrecondition,
			"credential %q is referenced but no credential store is configured", credentialRef)
	}

	_, err := s.credentialStore.Retrieve(ctx, credentialRef)
	if errors.Is(err, credentials.ErrCredentialNotFound) {
		return status.Errorf(codes.InvalidArgument, "credential %q not found in the credential store", credentialRef)
	}
	if err != nil {
		return status.Errorf(codes.Unavailable, "failed to check credential %q: %v", credentialRef, err)
	}
	return nil
}

func (s *ToolServer) DiscoverTools(ctx context.Context, req *gogiv1.DiscoverToolsRequest) (*gogiv1.DiscoverToolsResponse, error) {
	registered, err := s.store.ListTools(ctx, tools.NamespacePrefix(req.GetNamespace()))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list tools: %v", err)
	}

	versionsByName := make(map[string][]*postgres.GogiTool)
	for _, tool := range registered {
		versionsByName[tool.Name] = append(versionsByName[tool.Name], tool)
	}

	type match struct {
		tool  *postgres.GogiTool
		score float32
	}
	matches := make([]match, 0, len(versionsByName))
	for _, versions := range versionsByName {
		tool, err := tools.SelectVersion(versions, req.GetVersionConstraint())
		if errors.Is(err, utils.ErrToolNotFound) {
			continue
		}
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "%v", err)
		}

		if req.GetReadOnly() && !tool.Behavior.IsReadOnly {
			continue
		}
		// a tool that keeps failing is hidden, so that models do not keep selecting it
		if !s.executor.Available(tool) {
			continue
		}
		score, ok := relevance(tool, req.GetCapabilities(), req.GetTags())
		if !ok {
			continue
		}
		matches = append(matches, match{tool: tool, score: score})
	}

	sort.Slice(matches, func(i, j int) bool {
		if matches[i].score != matches[j].score {
			return matches[i].score > matches[j].score
		}
		return matches[i].tool.Name < matches[j].tool.Name
	})

	response := &gogiv1.DiscoverToolsResponse{
		Tools:           make([]*gogiv1.ToolServiceDefinition, 0, len(matches)),
		RelevanceScores: make(map[string]float32, len(matches)),
	}
	for _, m := range matches {
		response.Tools = append(response.Tools, toolToProto(m.tool))
		response.RelevanceScores[m.tool.Name] = m.score
	}
	return response, nil
}

// relevance scores a tool for a search by capabilities and tags: the fraction of the
// requested capabilities and tags it has. A tool matches if it has at least one of the
// requested capabilities, if any are requested, and at least one of the requested tags,
// if any are requested. Without capabilities and tags every tool matches with score 1
func relevance(tool *postgres.GogiTool, capabilities, tags []string) (float32, bool) {
	if len(capabilities) == 0 && len(tags) == 0 {
		return 1, true
	}

	matchedCapabilities := countMatches(tool.Capabilities, capabilities)
	matchedTags := countMatches(tool.Tags, tags)
	if (len(capabilities) > 0 && matchedCapabilities == 0) || (len(tags) > 0 && matchedTags == 0) {
		return 0, false
	}
	return float32(matchedCapabilities+matchedTags) / float32(len(capabilities)+len(tags)), true
}

func countMatches(values, wanted []string) int {
	has := make(map[string]bool, len(values))
	for _, value := range values {
		has[strings.ToLower(value)] = true
	}
	count := 0
	for _, value := range wanted {
		if has[strings.ToLower(value)] {
			count++
		}
	}
	return count
}

// resolveTool returns the given version of a registered tool, or its latest version
func (s *ToolServer) resolveTool(ctx context.Context, name, version string) (*postgres.GogiTool, error) {
	if name == "" {
		return nil, status.Error(codes.InvalidArgument, "a tool name is required")
	}
	versions, err := s.toolVersions(ctx, name)
	if err != nil {
		return nil, err
	}

	tool, err := tools.FindVersion(versions, version)
	if err != nil {
		if version == "" {
			return nil, status.Errorf(codes.NotFound, "tool %s not found", name)
		}
		return nil, status.Errorf(codes.NotFound, "version %s of tool %s not found", version, name)
	}
	return tool, nil
}

// parametersSchema returns the compiled parameters schema of the tool
func (s *ToolServer) parametersSchema(tool *postgres.GogiTool) (*jsonschema.Schema, error) {
	key := tools.CircuitKey(tool)
	if schema, ok := s.schemas.Load(key); ok {
		return schema.(*jsonschema.Schema), nil
	}
	schema, err := tools.CompileSchema(tool.ParametersJSON)
	if err != nil {
		return nil, err
	}
	s.schemas.Store(key, schema)
	return schema, nil
}

// validateArguments returns the problems with the arguments of a call to the tool
func (s *ToolServer) validateArguments(tool *postgres.GogiTool, argumentsJSON string) ([]string, error) {
	schema, err := s.parametersSchema(tool)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "invalid parameters schema of tool %s: %v", tools.CircuitKey(tool), err)
	}
	return tools.ValidateArguments(schema, argumentsJSON), nil
}

func (s *ToolServer) ValidateTool(ctx context.Context, req *gogiv1.ValidateToolRequest) (*gogiv1.ValidateToolResponse, error) {
	tool, err := s.resolveTool(ctx, req.GetToolName(), req.GetVersion())
	if err != nil {
		return nil, err
	}

	problems, err := s.validateArguments(tool, req.GetArgumentsJson())
	if err != nil {
		return nil, err
	}
	return &gogiv1.ValidateToolResponse{Valid: len(problems) == 0, Errors: problems}, nil
}

// call validates a call to the tool and executes it
func (s *ToolServer) call(ctx context.Context, tool *postgres.GogiTool, req *gogiv1.ExecuteToolRequest) (tools.Result, error) {
	problems, err := s.validateArguments(tool, req.GetArgumentsJson())
	if err != nil {
		return tools.Result{}, err
	}
	if len(problems) > 0 {
		return tools.Result{Status: tools.StatusFailed,
			Error: "invalid arguments: " + strings.Join(problems, "; ")}, nil
	}

	if tool.Behavior.RequiresConfirmation && !req.GetConfirmed() {
		return tools.Result{Status: tools.StatusFailed,
			Error: fmt.Sprintf("tool %s requires confirmation: ask the user to approve the call, "+
				"then execute it again with confirmed set", tool.Name)}, nil
	}

	argumentsJSON := req.GetArgumentsJson()
	if strings.TrimSpace(argumentsJSON) == "" {
		argumentsJSON = "{}"
	}
	return s.executor.Execute(ctx, tool, argumentsJSON, req.GetSessionId()), nil
}

func (s *ToolServer) ExecuteTool(ctx context.Context, req *gogiv1.ExecuteToolRequest) (*gogiv1.ExecuteToolResponse, error) {
	tool, err := s.resolveTool(ctx, req.GetToolName(), req.GetVersion())
	if err != nil {
		return nil, err
	}

	result, err := s.call(ctx, tool, req)
	if err != nil {
		return nil, err
	}

	return &gogiv1.ExecuteToolResponse{
		Success:         result.Status == tools.StatusSucceeded,
		ResultJson:      result.ResultJSON,
		Error:           result.Error,
		ExecutionTimeMs: int32(result.Duration.Milliseconds()),
		ToolVersion:     tool.Version,
	}, nil
}

func (s *ToolServer) ExecuteToolAsync(ctx context.Context, req *gogiv1.ExecuteToolRequest) (*gogiv1.ExecuteToolAsyncResponse, error) {
	tool, err := s.resolveTool(ctx, req.GetToolName(), req.GetVersion())
	if err != nil {
		return nil, err
	}

	arguments := req.GetArgumentsJson()
	task, err := s.store.CreateToolTask(ctx, &postgres.GogiToolTask{
		ToolName:    tool.Name,
		ToolVersion: tool.Version,
		SessionID:   req.GetSessionId(),
		Status:      string(tools.StatusPending),
		InputJson:   &arguments,
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create the task: %v", err)
	}

	s.tasks.Add(1)
	go func() {
		defer s.tasks.Done()
		s.runTask(task.ID, tool, req)
	}()

	return &gogiv1.ExecuteToolAsyncResponse{TaskId: task.ID, Status: string(tools.StatusPending)}, nil
}

// runTask executes an asynchronous call and records its outcome. The call outlives the
// request that started it
func (s *ToolServer) runTask(taskID string, tool *postgres.GogiTool, req *gogiv1.ExecuteToolRequest) {
	update := func(taskStatus tools.Status, resultJSON *string, errorMessage string) {
		ctx, cancel := context.WithTimeout(context.Background(), asyncTaskTimeout)
		defer cancel()
		if err := s.store.UpdateToolTask(ctx, taskID, string(taskStatus), resultJSON, errorMessage); err != nil {
			log.Errorf("failed to update task %s of tool %s: %v", taskID, tools.CircuitKey(tool), err)
		}
	}

	update(tools.StatusRunning, nil, "")

	result, err := s.call(context.Background(), tool, req)
	if err != nil {
		update(tools.StatusFailed, nil, status.Convert(err).Message())
		return
	}

	var resultJSON *string
	if result.Status == tools.StatusSucceeded {
		resultJSON = &result.ResultJSON
	}
	update(result.Status, resultJSON, result.Error)
}

func (s *ToolServer) GetTask(ctx context.Context, req *gogiv1.GetTaskRequest) (*gogiv1.GetTaskResponse, error) {
	task, err := s.store.GetToolTask(ctx, req.GetTaskId())
	if errors.Is(err, utils.ErrToolTaskNotFound) {
		return nil, status.Errorf(codes.NotFound, "task %s not found", req.GetTaskId())
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get task %s: %v", req.GetTaskId(), err)
	}

	response := &gogiv1.GetTaskResponse{TaskId: task.ID, Status: task.Status, Error: task.Error}
	if task.ResultJson != nil {
		response.ResultJson = *task.ResultJson
	}
	return response, nil
}

// RegisterMcpServer imports the tools of an MCP server into the registry, named
// <namespace>.<the tool's name on the server>. Calls to them go through the server.
// A tool is imported as version 1.0.0; importing it again registers a new major
// version if its definition on the server changed, so that applications pinned to the
// version they approved are not silently given a different tool
func (s *ToolServer) RegisterMcpServer(ctx context.Context, req *gogiv1.RegisterMcpServerRequest) (*gogiv1.RegisterMcpServerResponse, error) {
	namespace := strings.TrimSuffix(req.GetNamespace(), ".*")
	if namespace == "" {
		return nil, status.Error(codes.InvalidArgument, "a namespace is required for the tools of the MCP server")
	}
	if err := tools.ValidateToolName(namespace); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid namespace: %v", err)
	}
	if req.GetServerUrl() == "" {
		return nil, status.Error(codes.InvalidArgument, "a server_url is required")
	}
	if err := s.checkCredential(ctx, req.GetCredentialRef()); err != nil {
		return nil, err
	}

	credential := ""
	if req.GetCredentialRef() != "" {
		stored, err := s.credentialStore.Retrieve(ctx, req.GetCredentialRef())
		if err != nil {
			return nil, status.Errorf(codes.Unavailable, "failed to retrieve credential %q: %v", req.GetCredentialRef(), err)
		}
		credential = stored.Value
	}

	serverTools, err := s.mcpLister.ListTools(ctx, req.GetServerUrl(), credential)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "%v", err)
	}

	imported := make([]string, 0, len(serverTools))
	for _, serverTool := range serverTools {
		tool, err := mcpTool(namespace, req, serverTool)
		if err == nil {
			err = tools.NormalizeDefinition(tool)
		}
		if err != nil {
			log.Warnf("skipping tool %q of MCP server %s: %v", serverTool.Name, req.GetServerUrl(), err)
			continue
		}

		if err := s.importTool(ctx, tool); err != nil {
			return nil, err
		}
		imported = append(imported, tool.Name)
	}

	log.Infof("imported %d tools of MCP server %s into namespace %s", len(imported), req.GetServerUrl(), namespace)
	return &gogiv1.RegisterMcpServerResponse{ImportedToolNames: imported}, nil
}

// importTool registers a tool of an MCP server, unless its latest registered version
// has the same definition
func (s *ToolServer) importTool(ctx context.Context, tool *postgres.GogiTool) error {
	registered, err := s.toolVersions(ctx, tool.Name)
	if err != nil {
		return err
	}

	tool.Version = tools.DefaultToolVersion
	if len(registered) > 0 {
		latest, err := tools.SelectVersion(registered, "")
		if err != nil {
			return status.Errorf(codes.Internal, "failed to get tool %s: %v", tool.Name, err)
		}
		if sameDefinition(latest, tool) {
			return nil
		}
		tool.Version = fmt.Sprintf("%d.0.0", semver.MustParse(latest.Version).Major()+1)
	}
	return s.insertTool(ctx, tool)
}

// mcpTool converts a tool of an MCP server. Its annotations give its behavior, which
// the registration's policy overrides can extend
func mcpTool(namespace string, req *gogiv1.RegisterMcpServerRequest, serverTool *mcp.Tool) (*postgres.GogiTool, error) {
	parameters, err := json.Marshal(serverTool.InputSchema)
	if err != nil || serverTool.InputSchema == nil {
		parameters = []byte(tools.DefaultParametersJSON)
	}
	returns := ""
	if serverTool.OutputSchema != nil {
		encoded, err := json.Marshal(serverTool.OutputSchema)
		if err != nil {
			return nil, fmt.Errorf("invalid output schema: %w", err)
		}
		returns = string(encoded)
	}

	description := serverTool.Description
	if description == "" {
		description = serverTool.Title
	}

	tool := &postgres.GogiTool{
		Name:           namespace + "." + serverTool.Name,
		Owner:          namespace,
		Description:    description,
		ParametersJSON: string(parameters),
		ReturnsJSON:    returns,
		CredentialRef:  req.GetCredentialRef(),
		MCPServerURL:   req.GetServerUrl(),
		MCPToolName:    serverTool.Name,
		Capabilities:   []string{},
		Tags:           []string{"mcp"},
	}

	if annotations := serverTool.Annotations; annotations != nil {
		tool.Behavior.IsReadOnly = annotations.ReadOnlyHint
		tool.Behavior.IsIdempotent = annotations.IdempotentHint
	}

	if overrides := req.GetPolicyOverrides(); overrides != nil {
		tool.Behavior.IsReadOnly = tool.Behavior.IsReadOnly || overrides.GetIsReadOnly()
		tool.Behavior.IsIdempotent = tool.Behavior.IsIdempotent || overrides.GetIsIdempotent()
		tool.Behavior.RequiresConfirmation = overrides.GetRequiresConfirmation()
		tool.Behavior.TypicalLatencyMs = overrides.GetTypicalLatencyMs()
		tool.Behavior.SideEffects = overrides.GetSideEffects()
	}
	if limits := req.GetRateLimitOverrides(); limits != nil {
		tool.RateLimits = postgres.GogiToolRateLimits{
			RequestsPerMinute:  limits.GetRequestsPerMinute(),
			RequestsPerSession: limits.GetRequestsPerSession(),
			DailyLimit:         limits.GetDailyLimit(),
		}
	}
	return tool, nil
}

// sameDefinition reports whether two versions of a tool define the same tool
func sameDefinition(a, b *postgres.GogiTool) bool {
	type definition struct {
		Description, Owner, Endpoint, CredentialRef, MCPServerURL, MCPToolName string
		Parameters, Returns                                                    any
		Behavior                                                               postgres.GogiToolBehavior
		RateLimits                                                             postgres.GogiToolRateLimits
		Capabilities, Tags                                                     []string
	}
	of := func(tool *postgres.GogiTool) definition {
		d := definition{
			Description: tool.Description, Owner: tool.Owner, Endpoint: tool.Endpoint,
			CredentialRef: tool.CredentialRef, MCPServerURL: tool.MCPServerURL, MCPToolName: tool.MCPToolName,
			Behavior: tool.Behavior, RateLimits: tool.RateLimits,
			Capabilities: emptyIfNil(tool.Capabilities), Tags: emptyIfNil(tool.Tags),
		}
		d.Behavior.SideEffects = emptyIfNil(d.Behavior.SideEffects)
		_ = json.Unmarshal([]byte(tool.ParametersJSON), &d.Parameters)
		_ = json.Unmarshal([]byte(tool.ReturnsJSON), &d.Returns)
		return d
	}
	return reflect.DeepEqual(of(a), of(b))
}

func emptyIfNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
