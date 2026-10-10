package impl

import (
	gogiv1 "gogi/gogi/gogi/v1"
	"gogi/gogi/storage/postgres"
)

// toolFromProto converts a tool definition of a registration request
func toolFromProto(definition *gogiv1.ToolServiceDefinition) *postgres.GogiTool {
	behavior := definition.GetBehavior()
	rateLimits := definition.GetRateLimits()
	cost := definition.GetCost()
	limits := definition.GetExecutionLimits()

	return &postgres.GogiTool{
		Name:           definition.GetName(),
		Version:        definition.GetVersion(),
		Owner:          definition.GetOwner(),
		Description:    definition.GetDescription(),
		ParametersJSON: definition.GetParametersJson(),
		ReturnsJSON:    definition.GetReturnsJson(),
		Behavior: postgres.GogiToolBehavior{
			IsReadOnly:           behavior.GetIsReadOnly(),
			IsIdempotent:         behavior.GetIsIdempotent(),
			RequiresConfirmation: behavior.GetRequiresConfirmation(),
			TypicalLatencyMs:     behavior.GetTypicalLatencyMs(),
			SideEffects:          behavior.GetSideEffects(),
		},
		RateLimits: postgres.GogiToolRateLimits{
			RequestsPerMinute:  rateLimits.GetRequestsPerMinute(),
			RequestsPerSession: rateLimits.GetRequestsPerSession(),
			DailyLimit:         rateLimits.GetDailyLimit(),
		},
		Cost: postgres.GogiToolCost{
			EstimatedCostUSD: cost.GetEstimatedCostUsd(),
			BillingCategory:  cost.GetBillingCategory(),
		},
		ExecutionLimits: postgres.GogiToolExecutionLimits{
			TimeoutSeconds:     limits.GetTimeoutSeconds(),
			MemoryLimitMB:      limits.GetMemoryLimitMb(),
			CPULimitMillicores: limits.GetCpuLimitMillicores(),
			MaxResponseSizeKB:  limits.GetMaxResponseSizeKb(),
			MaxRetries:         limits.GetMaxRetries(),
		},
		RequiredPermissions: definition.GetRequiredPermissions(),
		Capabilities:        definition.GetCapabilities(),
		Tags:                definition.GetTags(),
		Endpoint:            definition.GetEndpoint(),
		CredentialRef:       definition.GetCredentialRef(),
		MCPServerURL:        definition.GetMcpServerUrl(),
	}
}

// toolToProto converts a registered tool for discovery. Applications call tools through
// the platform, so the endpoint and MCP server of a tool are not disclosed
func toolToProto(tool *postgres.GogiTool) *gogiv1.ToolServiceDefinition {
	return &gogiv1.ToolServiceDefinition{
		Name:           tool.Name,
		Version:        tool.Version,
		Owner:          tool.Owner,
		Description:    tool.Description,
		ParametersJson: tool.ParametersJSON,
		ReturnsJson:    tool.ReturnsJSON,
		Behavior: &gogiv1.ToolBehavior{
			IsReadOnly:           tool.Behavior.IsReadOnly,
			IsIdempotent:         tool.Behavior.IsIdempotent,
			RequiresConfirmation: tool.Behavior.RequiresConfirmation,
			TypicalLatencyMs:     tool.Behavior.TypicalLatencyMs,
			SideEffects:          tool.Behavior.SideEffects,
		},
		RateLimits: &gogiv1.RateLimits{
			RequestsPerMinute:  tool.RateLimits.RequestsPerMinute,
			RequestsPerSession: tool.RateLimits.RequestsPerSession,
			DailyLimit:         tool.RateLimits.DailyLimit,
		},
		Cost: &gogiv1.CostMetadata{
			EstimatedCostUsd: tool.Cost.EstimatedCostUSD,
			BillingCategory:  tool.Cost.BillingCategory,
		},
		RequiredPermissions: tool.RequiredPermissions,
		Capabilities:        tool.Capabilities,
		Tags:                tool.Tags,
		CredentialRef:       tool.CredentialRef,
		ExecutionLimits: &gogiv1.ExecutionLimits{
			TimeoutSeconds:     tool.ExecutionLimits.TimeoutSeconds,
			MemoryLimitMb:      tool.ExecutionLimits.MemoryLimitMB,
			CpuLimitMillicores: tool.ExecutionLimits.CPULimitMillicores,
			MaxResponseSizeKb:  tool.ExecutionLimits.MaxResponseSizeKB,
			MaxRetries:         tool.ExecutionLimits.MaxRetries,
		},
	}
}
