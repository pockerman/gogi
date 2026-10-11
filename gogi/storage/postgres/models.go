package postgres

import (
	"gogi/gogi/utils"
	"time"
)

type Job struct {
	ID           string
	DocumentID   string
	Status       utils.JobStatus
	JobType      string
	ErrorMessage string

	CreatedAt   time.Time
	StartedAt   *time.Time
	CompletedAt *time.Time
}

type GogiIndex struct {
	Id            string
	Name          string
	Owner         string
	CreatedAt     time.Time
	LastUpdatedAt time.Time
}

// LLM Sessions

type LLMSession struct {
	ID        string
	UserID    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type LLMMessage struct {
	ID        string
	SessionID string
	Role      string
	Content   *string
	Name      *string
	Timestamp int64
	CreatedAt time.Time
}

type UserMemory struct {
	ID        string
	UserID    string
	Key       string
	Value     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Prompts

type GogiPrompt struct {
	ID               string
	Name             string
	Version          string
	Owner            string
	GogiIndex        string
	MinioPath        string
	Author           string
	Model            string
	Temperature      float64
	MaxTokens        int32
	StopSequences    []string
	FrequencyPenalty float64
	PresencePenalty  float64
	TestSetID        string
	TestSetPath      string
	Metrics          map[string]float64
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// Tools

// GogiToolBehavior declares what a tool does, so the platform knows whether it can be
// called freely, retried safely, or needs a human to confirm the call
type GogiToolBehavior struct {
	IsReadOnly           bool     `json:"is_read_only"`
	IsIdempotent         bool     `json:"is_idempotent"`
	RequiresConfirmation bool     `json:"requires_confirmation"`
	TypicalLatencyMs     int32    `json:"typical_latency_ms"`
	SideEffects          []string `json:"side_effects"`
}

// GogiToolRateLimits limits the calls to a tool; zero means no limit
type GogiToolRateLimits struct {
	RequestsPerMinute  int32 `json:"requests_per_minute"`
	RequestsPerSession int32 `json:"requests_per_session"`
	DailyLimit         int32 `json:"daily_limit"`
}

type GogiToolCost struct {
	EstimatedCostUSD float32 `json:"estimated_cost_usd"`
	BillingCategory  string  `json:"billing_category"`
}

// GogiToolExecutionLimits bounds a tool call; zero means the platform default
type GogiToolExecutionLimits struct {
	TimeoutSeconds     int32 `json:"timeout_seconds"`
	MemoryLimitMB      int32 `json:"memory_limit_mb"`
	CPULimitMillicores int32 `json:"cpu_limit_millicores"`
	MaxResponseSizeKB  int32 `json:"max_response_size_kb"`
	MaxRetries         int32 `json:"max_retries"`
}

// GogiTool is one version of a tool registered with the platform. A tool is called at
// Endpoint, or through the MCP server at MCPServerURL, where it is named MCPToolName
type GogiTool struct {
	ID                  string
	Name                string
	Version             string
	Owner               string
	Description         string
	ParametersJSON      string
	ReturnsJSON         string
	Behavior            GogiToolBehavior
	RateLimits          GogiToolRateLimits
	Cost                GogiToolCost
	ExecutionLimits     GogiToolExecutionLimits
	RequiredPermissions []string
	Capabilities        []string
	Tags                []string
	Endpoint            string
	CredentialRef       string
	MCPServerURL        string
	MCPToolName         string
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// GogiRegisteredLLM is a model registered with the platform, served
// at Endpoint by a model server that AdapterType knows how to talk to
type GogiRegisteredLLM struct {
	ID                string
	Name              string
	Provider          string
	ContextWindow     int32
	SupportsVision    bool
	SupportsTools     bool
	SupportsStreaming bool
	SupportsJSONMode  bool
	Endpoint          string
	HealthCheck       string
	AdapterType       string
	CredentialRef     string
	Status            string
	LastCheckedAt     *time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// GogiToolTask is an asynchronous tool call
type GogiToolTask struct {
	ID          string
	ToolName    string
	ToolVersion string
	SessionID   string
	Status      string
	InputJson   *string
	ResultJson  *string
	Error       string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Workflows

type GogiWorkflow struct {
	ID             string
	Name           string
	ApiPath        string
	ContainerImage string
	ResponseMode   string
	Version        int32
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type GogiWorkflowDeployment struct {
	ID              string
	WorkflowID      string
	Version         int32
	Status          string
	DesiredReplicas int32
	CurrentReplicas int32
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type GogiRoute struct {
	ID         string
	WorkflowID string
	Path       string
	Method     string
	CreatedAt  time.Time
}

type GogiWorkflowJob struct {
	ID             string
	WorkflowID     *string
	Status         string
	Progress       int32
	CheckpointJson *string
	ResultJson     *string
	ErrorMessage   *string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}
