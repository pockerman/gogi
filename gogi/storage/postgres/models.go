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

type GogiTool struct {
	ID           string
	Name         string
	Version      string
	Owner        string
	Description  string
	IsReadOnly   bool
	IsIdempotent bool
	Capabilities []string
	Tags         []string
	Endpoint     string
	SchemaJson   string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type GogiToolTask struct {
	ID         string
	ToolName   string
	Status     string
	InputJson  *string
	ResultJson *string
	CreatedAt  time.Time
	UpdatedAt  time.Time
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
