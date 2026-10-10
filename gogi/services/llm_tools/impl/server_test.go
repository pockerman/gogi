package impl

import (
	"context"
	"encoding/json"
	"errors"
	"gogi/gogi/credentials"
	gogiv1 "gogi/gogi/gogi/v1"
	"gogi/gogi/storage/postgres"
	"gogi/gogi/tools"
	"gogi/gogi/utils"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// memoryToolStore is an in-memory ToolStore
type memoryToolStore struct {
	mu    sync.Mutex
	tools []*postgres.GogiTool
	tasks map[string]*postgres.GogiToolTask
}

func newMemoryToolStore() *memoryToolStore {
	return &memoryToolStore{tasks: make(map[string]*postgres.GogiToolTask)}
}

func (s *memoryToolStore) InsertTool(ctx context.Context, tool *postgres.GogiTool) (*postgres.GogiTool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.tools {
		if existing.Name == tool.Name && existing.Version == tool.Version {
			return nil, utils.ErrToolVersionExists
		}
	}
	tool.ID, tool.CreatedAt, tool.UpdatedAt = utils.NewUUIDString(), time.Now(), time.Now()
	stored := *tool
	s.tools = append(s.tools, &stored)
	return tool, nil
}

func (s *memoryToolStore) ListTools(ctx context.Context, namePrefix string) ([]*postgres.GogiTool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	found := make([]*postgres.GogiTool, 0)
	for _, tool := range s.tools {
		if strings.HasPrefix(tool.Name, namePrefix) {
			stored := *tool
			found = append(found, &stored)
		}
	}
	return found, nil
}

func (s *memoryToolStore) ListToolVersions(ctx context.Context, name string) ([]*postgres.GogiTool, error) {
	versions, _ := s.ListTools(ctx, name)
	found := make([]*postgres.GogiTool, 0)
	for _, tool := range versions {
		if tool.Name == name {
			found = append(found, tool)
		}
	}
	if len(found) == 0 {
		return nil, utils.ErrToolNotFound
	}
	return found, nil
}

func (s *memoryToolStore) CreateToolTask(ctx context.Context, task *postgres.GogiToolTask) (*postgres.GogiToolTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	task.ID = utils.NewUUIDString()
	stored := *task
	s.tasks[task.ID] = &stored
	return task, nil
}

func (s *memoryToolStore) GetToolTask(ctx context.Context, id string) (*postgres.GogiToolTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.tasks[id]
	if !ok {
		return nil, utils.ErrToolTaskNotFound
	}
	stored := *task
	return &stored, nil
}

func (s *memoryToolStore) UpdateToolTask(ctx context.Context, id, taskStatus string, resultJson *string, errorMessage string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.tasks[id]
	if !ok {
		return utils.ErrToolTaskNotFound
	}
	task.Status, task.ResultJson, task.Error = taskStatus, resultJson, errorMessage
	return nil
}

func newTestToolServer(t *testing.T, credentialStore credentials.CredentialStore) *ToolServer {
	t.Helper()
	config := tools.DefaultExecutorConfig()
	config.DefaultTimeout = 2 * time.Second
	mcpAdapter := tools.NewMCPAdapter(nil)
	executor := tools.NewExecutor(credentialStore, tools.NewCircuitBreaker(2, time.Minute, 1),
		tools.NewRateLimiter(), tools.NewHTTPAdapter(nil), mcpAdapter, config)
	return newToolServer(newMemoryToolStore(), executor, credentialStore, mcpAdapter)
}

func expectCode(t *testing.T, err error, code codes.Code) {
	t.Helper()
	if status.Code(err) != code {
		t.Errorf("expected %s, got %v", code, err)
	}
}

const bookingParameters = `{"type":"object","properties":{"patient_id":{"type":"string"},"slot_id":{"type":"string"}},
	"required":["patient_id","slot_id"]}`

func bookAppointment(version, endpoint string) *gogiv1.ToolServiceDefinition {
	return &gogiv1.ToolServiceDefinition{
		Name:           "healthcare.scheduling.book_appointment",
		Version:        version,
		Owner:          "scheduling-team",
		Description:    "Book an appointment slot for a patient",
		ParametersJson: bookingParameters,
		Capabilities:   []string{"appointments", "booking"},
		Tags:           []string{"patient-facing"},
		Endpoint:       endpoint,
	}
}

func discoveredNames(response *gogiv1.DiscoverToolsResponse) []string {
	names := make([]string, 0, len(response.GetTools()))
	for _, tool := range response.GetTools() {
		names = append(names, tool.GetName()+"@"+tool.GetVersion())
	}
	return names
}

func TestRegisterAndDiscoverTools(t *testing.T) {
	ctx := context.Background()
	server := newTestToolServer(t, nil)

	register := func(tool *gogiv1.ToolServiceDefinition) error {
		_, err := server.RegisterTool(ctx, &gogiv1.RegisterToolRequest{Tool: tool})
		return err
	}

	response, err := server.RegisterTool(ctx, &gogiv1.RegisterToolRequest{Tool: bookAppointment("1.0.0", "https://scheduler/v1/bookings")})
	if err != nil || response.GetStatus() != "registered" || response.GetVersion() != "1.0.0" {
		t.Fatalf("registration failed %v, %v", response, err)
	}

	// versions are immutable
	expectCode(t, register(bookAppointment("1.0.0", "https://scheduler/v2/bookings")), codes.AlreadyExists)

	// a minor version cannot add a required parameter; a major version can
	v2 := bookAppointment("1.1.0", "https://scheduler/v2/bookings")
	v2.ParametersJson = `{"type":"object","properties":{"patient_id":{"type":"string"},"slot_id":{"type":"string"},
		"appointment_type":{"type":"string"}},"required":["patient_id","slot_id","appointment_type"]}`
	expectCode(t, register(v2), codes.FailedPrecondition)
	v2.Version = "2.0.0"
	if err := register(v2); err != nil {
		t.Fatal(err)
	}

	checkAvailability := &gogiv1.ToolServiceDefinition{
		Name: "healthcare.scheduling.check_availability", Description: "List the free slots of a clinic",
		Behavior: &gogiv1.ToolBehavior{IsReadOnly: true, IsIdempotent: true}, Capabilities: []string{"appointments"},
		Endpoint: "https://scheduler/v1/availability",
	}
	verifyInsurance := &gogiv1.ToolServiceDefinition{
		Name: "healthcare.billing.verify_insurance", Description: "Verify insurance coverage",
		Capabilities: []string{"insurance", "verification"}, Tags: []string{"patient-facing"},
		Endpoint: "https://billing/v1/verify",
	}
	for _, tool := range []*gogiv1.ToolServiceDefinition{checkAvailability, verifyInsurance} {
		if err := register(tool); err != nil {
			t.Fatal(err)
		}
	}

	// invalid registrations
	expectCode(t, register(nil), codes.InvalidArgument)
	expectCode(t, register(&gogiv1.ToolServiceDefinition{Name: "no endpoint", Description: "x"}), codes.InvalidArgument)
	withCredential := bookAppointment("3.0.0", "https://scheduler/v3/bookings")
	withCredential.CredentialRef = "scheduling-api-prod"
	expectCode(t, register(withCredential), codes.FailedPrecondition)

	discover := func(req *gogiv1.DiscoverToolsRequest) *gogiv1.DiscoverToolsResponse {
		t.Helper()
		response, err := server.DiscoverTools(ctx, req)
		if err != nil {
			t.Fatal(err)
		}
		return response
	}

	cases := []struct {
		request  *gogiv1.DiscoverToolsRequest
		expected []string
	}{
		{&gogiv1.DiscoverToolsRequest{}, []string{"healthcare.billing.verify_insurance@1.0.0",
			"healthcare.scheduling.book_appointment@2.0.0", "healthcare.scheduling.check_availability@1.0.0"}},
		{&gogiv1.DiscoverToolsRequest{Namespace: "healthcare.scheduling.*"}, []string{
			"healthcare.scheduling.book_appointment@2.0.0", "healthcare.scheduling.check_availability@1.0.0"}},
		{&gogiv1.DiscoverToolsRequest{Namespace: "healthcare.scheduling", VersionConstraint: "^1.0.0"}, []string{
			"healthcare.scheduling.book_appointment@1.0.0", "healthcare.scheduling.check_availability@1.0.0"}},
		{&gogiv1.DiscoverToolsRequest{Namespace: "healthcare", ReadOnly: true}, []string{
			"healthcare.scheduling.check_availability@1.0.0"}},
		{&gogiv1.DiscoverToolsRequest{Namespace: "internal.*"}, []string{}},
		// ranked by the fraction of requested capabilities and tags
		{&gogiv1.DiscoverToolsRequest{Capabilities: []string{"Appointments", "booking"}}, []string{
			"healthcare.scheduling.book_appointment@2.0.0", "healthcare.scheduling.check_availability@1.0.0"}},
		{&gogiv1.DiscoverToolsRequest{Capabilities: []string{"insurance", "appointments"}, Tags: []string{"patient-facing"}},
			[]string{"healthcare.billing.verify_insurance@1.0.0", "healthcare.scheduling.book_appointment@2.0.0"}},
	}
	for _, c := range cases {
		names := discoveredNames(discover(c.request))
		if strings.Join(names, ",") != strings.Join(c.expected, ",") {
			t.Errorf("%v: expected %v, got %v", c.request, c.expected, names)
		}
	}

	scored := discover(&gogiv1.DiscoverToolsRequest{Capabilities: []string{"insurance", "appointments"}, Tags: []string{"patient-facing"}})
	scores := scored.GetRelevanceScores()
	if scores["healthcare.billing.verify_insurance"] < 0.66 || scores["healthcare.scheduling.book_appointment"] < 0.66 {
		t.Errorf("wrong scores %v", scores)
	}

	// discovery returns the definition for function calling, but not the endpoint
	tool := discover(&gogiv1.DiscoverToolsRequest{Namespace: "healthcare.scheduling", ReadOnly: true}).GetTools()[0]
	if tool.GetEndpoint() != "" || tool.GetParametersJson() != tools.DefaultParametersJSON || !tool.GetBehavior().GetIsIdempotent() {
		t.Errorf("wrong discovered definition %v", tool)
	}

	_, err = server.DiscoverTools(ctx, &gogiv1.DiscoverToolsRequest{VersionConstraint: "latest"})
	expectCode(t, err, codes.InvalidArgument)
}

func TestRegisterToolWithCredential(t *testing.T) {
	ctx := context.Background()
	store := credentials.NewMemoryCredentialStore()
	_ = store.Store(ctx, "scheduling-api-prod", "s3cr3t")
	server := newTestToolServer(t, store)

	tool := bookAppointment("1.0.0", "https://scheduler/v1/bookings")
	tool.CredentialRef = "unknown"
	_, err := server.RegisterTool(ctx, &gogiv1.RegisterToolRequest{Tool: tool})
	expectCode(t, err, codes.InvalidArgument)

	tool.CredentialRef = "scheduling-api-prod"
	if _, err := server.RegisterTool(ctx, &gogiv1.RegisterToolRequest{Tool: tool}); err != nil {
		t.Fatal(err)
	}
}

// newSchedulerServer serves the scheduling tools
func newSchedulerServer(t *testing.T) *httptest.Server {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer s3cr3t" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var arguments map[string]string
		_ = json.NewDecoder(r.Body).Decode(&arguments)
		switch r.URL.Path {
		case "/bookings":
			_ = json.NewEncoder(w).Encode(map[string]string{"booking_id": "b-" + arguments["slot_id"],
				"version": r.Header.Get("X-Gogi-Tool-Version")})
		case "/broken":
			http.Error(w, "down", http.StatusInternalServerError)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func TestExecuteTool(t *testing.T) {
	ctx := context.Background()
	store := credentials.NewMemoryCredentialStore()
	_ = store.Store(ctx, "scheduling-api-prod", "s3cr3t")
	server := newTestToolServer(t, store)
	scheduler := newSchedulerServer(t)

	for _, version := range []string{"1.0.0", "1.1.0"} {
		tool := bookAppointment(version, scheduler.URL+"/bookings")
		tool.CredentialRef = "scheduling-api-prod"
		if _, err := server.RegisterTool(ctx, &gogiv1.RegisterToolRequest{Tool: tool}); err != nil {
			t.Fatal(err)
		}
	}

	validation, err := server.ValidateTool(ctx, &gogiv1.ValidateToolRequest{
		ToolName: "healthcare.scheduling.book_appointment", ArgumentsJson: `{"patient_id":"p-1"}`})
	if err != nil || validation.GetValid() || len(validation.GetErrors()) != 1 || !strings.Contains(validation.GetErrors()[0], "slot_id") {
		t.Errorf("expected a missing slot_id, got %v, %v", validation, err)
	}

	execute := func(req *gogiv1.ExecuteToolRequest) *gogiv1.ExecuteToolResponse {
		t.Helper()
		response, err := server.ExecuteTool(ctx, req)
		if err != nil {
			t.Fatal(err)
		}
		return response
	}

	// the latest version runs unless one is given
	response := execute(&gogiv1.ExecuteToolRequest{ToolName: "healthcare.scheduling.book_appointment",
		ArgumentsJson: `{"patient_id":"p-1","slot_id":"s-9"}`, SessionId: "session-1"})
	if !response.GetSuccess() || response.GetToolVersion() != "1.1.0" ||
		response.GetResultJson() != `{"booking_id":"b-s-9","version":"1.1.0"}` {
		t.Errorf("wrong response %v", response)
	}
	response = execute(&gogiv1.ExecuteToolRequest{ToolName: "healthcare.scheduling.book_appointment", Version: "1.0.0",
		ArgumentsJson: `{"patient_id":"p-1","slot_id":"s-9"}`})
	if !response.GetSuccess() || response.GetToolVersion() != "1.0.0" || !strings.Contains(response.GetResultJson(), `"1.0.0"`) {
		t.Errorf("wrong response %v", response)
	}

	// invalid arguments are reported to the model, the tool is not called
	response = execute(&gogiv1.ExecuteToolRequest{ToolName: "healthcare.scheduling.book_appointment",
		ArgumentsJson: `{"patient_id":"p-1"}`})
	if response.GetSuccess() || !strings.HasPrefix(response.GetError(), "invalid arguments") {
		t.Errorf("expected invalid arguments, got %v", response)
	}

	_, err = server.ExecuteTool(ctx, &gogiv1.ExecuteToolRequest{ToolName: "healthcare.scheduling.unknown"})
	expectCode(t, err, codes.NotFound)
	_, err = server.ExecuteTool(ctx, &gogiv1.ExecuteToolRequest{ToolName: "healthcare.scheduling.book_appointment", Version: "9.0.0"})
	expectCode(t, err, codes.NotFound)
	_, err = server.ExecuteTool(ctx, &gogiv1.ExecuteToolRequest{})
	expectCode(t, err, codes.InvalidArgument)

	// a tool that requires confirmation runs only once the call is confirmed
	cancel := &gogiv1.ToolServiceDefinition{
		Name: "healthcare.scheduling.cancel_appointment", Description: "Cancel an appointment",
		ParametersJson: bookingParameters, Behavior: &gogiv1.ToolBehavior{RequiresConfirmation: true},
		Endpoint: scheduler.URL + "/bookings", CredentialRef: "scheduling-api-prod",
	}
	if _, err := server.RegisterTool(ctx, &gogiv1.RegisterToolRequest{Tool: cancel}); err != nil {
		t.Fatal(err)
	}
	request := &gogiv1.ExecuteToolRequest{ToolName: cancel.GetName(), ArgumentsJson: `{"patient_id":"p-1","slot_id":"s-9"}`}
	if response := execute(request); response.GetSuccess() || !strings.Contains(response.GetError(), "requires confirmation") {
		t.Errorf("expected a confirmation request, got %v", response)
	}
	request.Confirmed = true
	if response := execute(request); !response.GetSuccess() {
		t.Errorf("expected success, got %v", response)
	}
}

func TestExecuteToolAsync(t *testing.T) {
	ctx := context.Background()
	store := credentials.NewMemoryCredentialStore()
	_ = store.Store(ctx, "scheduling-api-prod", "s3cr3t")
	server := newTestToolServer(t, store)
	scheduler := newSchedulerServer(t)

	for name, path := range map[string]string{"healthcare.scheduling.book_appointment": "/bookings",
		"healthcare.scheduling.broken": "/broken"} {
		tool := bookAppointment("1.0.0", scheduler.URL+path)
		tool.Name, tool.CredentialRef = name, "scheduling-api-prod"
		if _, err := server.RegisterTool(ctx, &gogiv1.RegisterToolRequest{Tool: tool}); err != nil {
			t.Fatal(err)
		}
	}

	run := func(name, arguments string) *gogiv1.GetTaskResponse {
		t.Helper()
		started, err := server.ExecuteToolAsync(ctx, &gogiv1.ExecuteToolRequest{ToolName: name, ArgumentsJson: arguments})
		if err != nil || started.GetStatus() != "pending" || started.GetTaskId() == "" {
			t.Fatalf("failed to start %v, %v", started, err)
		}
		server.Wait()
		task, err := server.GetTask(ctx, &gogiv1.GetTaskRequest{TaskId: started.GetTaskId()})
		if err != nil {
			t.Fatal(err)
		}
		return task
	}

	task := run("healthcare.scheduling.book_appointment", `{"patient_id":"p-1","slot_id":"s-2"}`)
	if task.GetStatus() != "succeeded" || task.GetResultJson() != `{"booking_id":"b-s-2","version":"1.0.0"}` {
		t.Errorf("wrong task %v", task)
	}

	task = run("healthcare.scheduling.broken", `{"patient_id":"p-1","slot_id":"s-2"}`)
	if task.GetStatus() != "failed" || task.GetError() != "tool endpoint returned 500 Internal Server Error" || task.GetResultJson() != "" {
		t.Errorf("wrong task %v", task)
	}

	task = run("healthcare.scheduling.book_appointment", `{}`)
	if task.GetStatus() != "failed" || !strings.HasPrefix(task.GetError(), "invalid arguments") {
		t.Errorf("wrong task %v", task)
	}

	_, err := server.ExecuteToolAsync(ctx, &gogiv1.ExecuteToolRequest{ToolName: "healthcare.unknown"})
	expectCode(t, err, codes.NotFound)
	_, err = server.GetTask(ctx, &gogiv1.GetTaskRequest{TaskId: "unknown"})
	expectCode(t, err, codes.NotFound)
}

func TestFailingToolsAreHiddenFromDiscovery(t *testing.T) {
	ctx := context.Background()
	store := credentials.NewMemoryCredentialStore()
	_ = store.Store(ctx, "scheduling-api-prod", "s3cr3t")
	server := newTestToolServer(t, store)
	scheduler := newSchedulerServer(t)

	tool := bookAppointment("1.0.0", scheduler.URL+"/broken")
	tool.CredentialRef = "scheduling-api-prod"
	if _, err := server.RegisterTool(ctx, &gogiv1.RegisterToolRequest{Tool: tool}); err != nil {
		t.Fatal(err)
	}

	// the test circuit breaker opens after 2 failures
	for range 2 {
		_, _ = server.ExecuteTool(ctx, &gogiv1.ExecuteToolRequest{ToolName: tool.GetName(),
			ArgumentsJson: `{"patient_id":"p-1","slot_id":"s-2"}`})
	}
	discovered, err := server.DiscoverTools(ctx, &gogiv1.DiscoverToolsRequest{})
	if err != nil || len(discovered.GetTools()) != 0 {
		t.Errorf("expected the failing tool to be hidden, got %v, %v", discoveredNames(discovered), err)
	}
}

type issueArguments struct {
	Repo  string `json:"repo" jsonschema:"the repository"`
	Title string `json:"title" jsonschema:"the title of the issue"`
}

type issue struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
}

// newGitHubMCPServer serves an MCP server with GitHub-like tools that requires the
// bearer token gh-token. describeCreateIssue is the description of create_issue
func newGitHubMCPServer(t *testing.T, describeCreateIssue *string) *httptest.Server {
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		server := mcp.NewServer(&mcp.Implementation{Name: "github", Version: "1.0.0"}, nil)
		mcp.AddTool(server, &mcp.Tool{Name: "create_issue", Description: *describeCreateIssue},
			func(ctx context.Context, req *mcp.CallToolRequest, args issueArguments) (*mcp.CallToolResult, issue, error) {
				return nil, issue{Number: 42, Title: args.Title}, nil
			})
		server.AddTool(&mcp.Tool{Name: "list_repos", Description: "List the repositories",
			InputSchema: map[string]any{"type": "object"},
			Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true}},
			func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: `["gogi","gogi-python"]`}}}, nil
			})
		server.AddTool(&mcp.Tool{Name: "delete_repo", Description: "Delete a repository",
			InputSchema: map[string]any{"type": "object"}},
			func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "permission denied"}}}, nil
			})
		server.AddTool(&mcp.Tool{Name: "bad name!", Description: "A tool the platform cannot name",
			InputSchema: map[string]any{"type": "object"}},
			func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return &mcp.CallToolResult{}, nil
			})
		return server
	}, nil)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer gh-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	return server
}

func TestRegisterMcpServer(t *testing.T) {
	ctx := context.Background()
	store := credentials.NewMemoryCredentialStore()
	_ = store.Store(ctx, "github-api-token", "gh-token")
	_ = store.Store(ctx, "wrong-token", "nope")
	server := newTestToolServer(t, store)

	description := "Create an issue in a repository"
	github := newGitHubMCPServer(t, &description)
	request := &gogiv1.RegisterMcpServerRequest{
		ServerUrl:          github.URL,
		Namespace:          "devtools.github",
		CredentialRef:      "github-api-token",
		PolicyOverrides:    &gogiv1.ToolBehavior{RequiresConfirmation: true},
		RateLimitOverrides: &gogiv1.RateLimits{RequestsPerMinute: 30},
	}

	response, err := server.RegisterMcpServer(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	imported := response.GetImportedToolNames()
	sort.Strings(imported)
	if strings.Join(imported, ",") != "devtools.github.create_issue,devtools.github.delete_repo,devtools.github.list_repos" {
		t.Fatalf("wrong imported tools %v", imported)
	}

	discovered, err := server.DiscoverTools(ctx, &gogiv1.DiscoverToolsRequest{Namespace: "devtools.github"})
	if err != nil {
		t.Fatal(err)
	}
	byName := make(map[string]*gogiv1.ToolServiceDefinition)
	for _, tool := range discovered.GetTools() {
		byName[tool.GetName()] = tool
	}
	listRepos := byName["devtools.github.list_repos"]
	if !listRepos.GetBehavior().GetIsReadOnly() || !listRepos.GetBehavior().GetRequiresConfirmation() ||
		listRepos.GetRateLimits().GetRequestsPerMinute() != 30 || listRepos.GetMcpServerUrl() != "" ||
		listRepos.GetVersion() != "1.0.0" || listRepos.GetOwner() != "devtools.github" {
		t.Errorf("wrong imported tool %v", listRepos)
	}
	if !strings.Contains(byName["devtools.github.create_issue"].GetParametersJson(), `"title"`) {
		t.Errorf("the input schema was not imported %v", byName["devtools.github.create_issue"])
	}

	execute := func(name, arguments string) *gogiv1.ExecuteToolResponse {
		t.Helper()
		response, err := server.ExecuteTool(ctx, &gogiv1.ExecuteToolRequest{ToolName: name, ArgumentsJson: arguments, Confirmed: true})
		if err != nil {
			t.Fatal(err)
		}
		return response
	}

	// structured content
	created := execute("devtools.github.create_issue", `{"repo":"gogi","title":"Add tools"}`)
	var createdIssue issue
	if !created.GetSuccess() || json.Unmarshal([]byte(created.GetResultJson()), &createdIssue) != nil ||
		createdIssue != (issue{Number: 42, Title: "Add tools"}) {
		t.Errorf("wrong result %v", created)
	}
	// text content that is JSON
	if listed := execute("devtools.github.list_repos", `{}`); !listed.GetSuccess() || listed.GetResultJson() != `["gogi","gogi-python"]` {
		t.Errorf("wrong result %v", listed)
	}
	// a tool error
	if deleted := execute("devtools.github.delete_repo", `{}`); deleted.GetSuccess() || deleted.GetError() != "permission denied" {
		t.Errorf("expected the tool's error, got %v", deleted)
	}

	// importing the same server again changes nothing
	if _, err := server.RegisterMcpServer(ctx, request); err != nil {
		t.Fatal(err)
	}
	versions, _ := server.store.ListToolVersions(ctx, "devtools.github.create_issue")
	if len(versions) != 1 {
		t.Errorf("expected one version, got %d", len(versions))
	}

	// a tool whose definition changed on the server gets a new major version, so that
	// the definition approved before is kept
	description = "Create an issue, or a pull request"
	if _, err := server.RegisterMcpServer(ctx, request); err != nil {
		t.Fatal(err)
	}
	versions, _ = server.store.ListToolVersions(ctx, "devtools.github.create_issue")
	latest, _ := tools.SelectVersion(versions, "")
	if len(versions) != 2 || latest.Version != "2.0.0" || latest.Description != description {
		t.Errorf("expected version 2.0.0 with the new description, got %d versions, latest %+v", len(versions), latest)
	}

	// the MCP server rejects the wrong credential
	_, err = server.RegisterMcpServer(ctx, &gogiv1.RegisterMcpServerRequest{ServerUrl: github.URL,
		Namespace: "devtools.github2", CredentialRef: "wrong-token"})
	expectCode(t, err, codes.Unavailable)

	_, err = server.RegisterMcpServer(ctx, &gogiv1.RegisterMcpServerRequest{ServerUrl: github.URL})
	expectCode(t, err, codes.InvalidArgument)
	_, err = server.RegisterMcpServer(ctx, &gogiv1.RegisterMcpServerRequest{ServerUrl: github.URL,
		Namespace: "devtools.github", CredentialRef: "unknown"})
	expectCode(t, err, codes.InvalidArgument)
}

// TestToolServerRepository runs the server against PostgreSQL, e.g.
// GOGI_TEST_POSTGRES_DSN=postgres://postgres:test@localhost:5432/gogi?sslmode=disable
func TestToolServerRepository(t *testing.T) {
	dsn := os.Getenv("GOGI_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("GOGI_TEST_POSTGRES_DSN is not set")
	}
	pool, err := postgres.NewPool(dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	clear := func() {
		for _, table := range []string{postgres.GOGI_TOOL_TASKS_TABLE_NAME, postgres.GOGI_TOOLS_TABLE_NAME} {
			if _, err := pool.Exec(context.Background(), "DELETE FROM "+table); err != nil {
				t.Fatal(err)
			}
		}
	}
	clear()
	t.Cleanup(clear)

	ctx := context.Background()
	repo := postgres.NewGogiToolsRepository(pool)
	credentialStore := credentials.NewMemoryCredentialStore()
	_ = credentialStore.Store(ctx, "scheduling-api-prod", "s3cr3t")
	server := newTestToolServer(t, credentialStore)
	server.store = repo
	scheduler := newSchedulerServer(t)

	tool := bookAppointment("1.0.0", scheduler.URL+"/bookings")
	tool.CredentialRef = "scheduling-api-prod"
	tool.Behavior = &gogiv1.ToolBehavior{IsIdempotent: true, SideEffects: []string{"scheduler"}}
	tool.RateLimits = &gogiv1.RateLimits{RequestsPerSession: 3}
	tool.ExecutionLimits = &gogiv1.ExecutionLimits{TimeoutSeconds: 5}
	if _, err := server.RegisterTool(ctx, &gogiv1.RegisterToolRequest{Tool: tool}); err != nil {
		t.Fatal(err)
	}
	_, err = server.RegisterTool(ctx, &gogiv1.RegisterToolRequest{Tool: tool})
	expectCode(t, err, codes.AlreadyExists)

	stored, err := repo.ListToolVersions(ctx, tool.GetName())
	if err != nil || len(stored) != 1 {
		t.Fatalf("expected one version, got %v, %v", stored, err)
	}
	if got := stored[0]; got.Owner != "scheduling-team" || got.CredentialRef != "scheduling-api-prod" ||
		!got.Behavior.IsIdempotent || strings.Join(got.Behavior.SideEffects, ",") != "scheduler" ||
		got.RateLimits.RequestsPerSession != 3 || got.ExecutionLimits.TimeoutSeconds != 5 ||
		strings.Join(got.Capabilities, ",") != "appointments,booking" {
		t.Errorf("wrong stored tool %+v", got)
	}
	if _, err := repo.ListToolVersions(ctx, "unknown"); !errors.Is(err, utils.ErrToolNotFound) {
		t.Errorf("expected ErrToolNotFound, got %v", err)
	}

	discovered, err := server.DiscoverTools(ctx, &gogiv1.DiscoverToolsRequest{Namespace: "healthcare.scheduling.*"})
	if err != nil || len(discovered.GetTools()) != 1 {
		t.Fatalf("expected one tool, got %v, %v", discovered, err)
	}
	other, _ := repo.ListTools(ctx, "healthcare.sched_")
	if len(other) != 0 {
		t.Errorf("the namespace prefix is not a pattern: %v", other)
	}

	started, err := server.ExecuteToolAsync(ctx, &gogiv1.ExecuteToolRequest{ToolName: tool.GetName(),
		ArgumentsJson: `{"patient_id":"p-1","slot_id":"s-3"}`, SessionId: "session-1"})
	if err != nil {
		t.Fatal(err)
	}
	server.Wait()
	task, err := server.GetTask(ctx, &gogiv1.GetTaskRequest{TaskId: started.GetTaskId()})
	if err != nil || task.GetStatus() != "succeeded" || task.GetResultJson() != `{"booking_id":"b-s-3","version":"1.0.0"}` {
		t.Errorf("wrong task %v, %v", task, err)
	}
	storedTask, _ := repo.GetToolTask(ctx, started.GetTaskId())
	if storedTask.ToolVersion != "1.0.0" || storedTask.SessionID != "session-1" || *storedTask.InputJson == "" {
		t.Errorf("wrong stored task %+v", storedTask)
	}
	_, err = server.GetTask(ctx, &gogiv1.GetTaskRequest{TaskId: utils.NewUUIDString()})
	expectCode(t, err, codes.NotFound)
}
