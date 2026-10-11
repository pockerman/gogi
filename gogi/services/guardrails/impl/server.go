package impl

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	gogiv1 "gogi/gogi/gogi/v1"
	"gogi/gogi/guardrails"
	"time"

	Log "github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// GuardrailsServer implements the GuardrailsService server.
// The server provides the following endpoints
//   - ValidateInput checks content against input policies before it reaches a model
//   - FilterOutput redacts or blocks a response against output policies before it reaches a user
//   - CheckPolicy checks an action, e.g. a tool call, against tool validation, behavioural
//     and human approval policies before it runs
//   - ReportViolation records a violation an application found
//   - AddPolicy, GetPolicy, ListPolicies and DeletePolicy manage the policies
//   - ListViolations returns the reported violations, for audit
//
// Every evaluation is recorded, whatever its outcome, with a hash of the content rather
// than the content itself.

const (
	defaultViolationsLimit = 100
	maxViolationsLimit     = 1000
	defaultSeverity        = "low"
)

var severities = []string{"low", "medium", "high", "critical", "blocked"}

type GuardrailsServer struct {
	gogiv1.UnimplementedGuardrailsServiceServer
	store guardrails.Store
	now   func() time.Time
}

func NewGuardrailsServer(store guardrails.Store) *GuardrailsServer {
	return &GuardrailsServer{store: store, now: time.Now}
}

// resolve returns the compiled policies with the given names, and those of the
// application and of every application, all of which must be of one of the given types
func (s *GuardrailsServer) resolve(ctx context.Context, names []string, applicationID string,
	types []string, rpc string) ([]*guardrails.CompiledPolicy, error) {

	selected := make([]guardrails.Policy, 0)
	seen := map[string]bool{}
	for _, name := range names {
		if seen[name] {
			continue
		}
		policy, err := s.store.GetPolicy(ctx, name)
		if errors.Is(err, guardrails.ErrPolicyNotFound) {
			return nil, status.Errorf(codes.NotFound, "policy %q not found", name)
		}
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to get policy %q: %v", name, err)
		}
		if !contains(types, policy.Type) {
			return nil, status.Errorf(codes.InvalidArgument, "policy %q is a %s policy; %s applies %v policies",
				name, policy.Type, rpc, types)
		}
		seen[name] = true
		selected = append(selected, policy)
	}

	if applicationID != "" {
		all, err := s.store.ListPolicies(ctx, "", "")
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to list policies: %v", err)
		}
		for _, policy := range all {
			global := policy.ApplicationID == ""
			if seen[policy.Name] || !contains(types, policy.Type) || (!global && policy.ApplicationID != applicationID) {
				continue
			}
			seen[policy.Name] = true
			selected = append(selected, policy)
		}
	}

	compiled := make([]*guardrails.CompiledPolicy, 0, len(selected))
	for _, policy := range selected {
		policyCompiled, _, err := guardrails.Compile(policy)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "stored policy %q is invalid: %v", policy.Name, err)
		}
		compiled = append(compiled, policyCompiled)
	}
	guardrails.SortPolicies(compiled)
	return compiled, nil
}

func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}

func policyNames(policies []*guardrails.CompiledPolicy) []string {
	names := make([]string, 0, len(policies))
	for _, policy := range policies {
		names = append(names, policy.Name)
	}
	return names
}

func contentHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// record records the evaluation; an audit failure is logged, not returned, so it does not
// take the guardrails down with the database
func (s *GuardrailsServer) record(ctx context.Context, evaluation guardrails.Evaluation) string {
	evaluation.SessionID = evaluation.Context[guardrails.ContextSessionID]
	evaluation.TurnID = evaluation.Context[guardrails.ContextTurnID]
	evaluation.UserID = evaluation.Context[guardrails.ContextUserID]

	id, err := s.store.RecordEvaluation(ctx, evaluation)
	if err != nil {
		Log.Errorf("failed to record the %s evaluation: %v", evaluation.InspectionPoint, err)
		return ""
	}
	return id
}

func toGRPCTriggeredRules(rules []guardrails.TriggeredRule) []*gogiv1.TriggeredRule {
	converted := make([]*gogiv1.TriggeredRule, 0, len(rules))
	for _, rule := range rules {
		converted = append(converted, &gogiv1.TriggeredRule{PolicyName: rule.PolicyName, RuleType: rule.RuleType,
			Action: rule.Action, Severity: rule.Severity, Detail: rule.Detail, Shadow: rule.Shadow})
	}
	return converted
}

func (s *GuardrailsServer) ValidateInput(ctx context.Context, req *gogiv1.ValidateInputRequest) (*gogiv1.ValidateInputResponse, error) {

	policies, err := s.resolve(ctx, req.GetPolicyNames(), req.GetApplicationId(),
		[]string{guardrails.TypeInput}, "ValidateInput")
	if err != nil {
		return nil, err
	}
	if len(req.GetChecks()) > 0 {
		builtin, err := guardrails.BuiltinInputPolicy(req.GetChecks())
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "%v", err)
		}
		policies = append(policies, builtin)
	}

	result := guardrails.EvaluateInput(policies, req.GetContent())

	outcome := guardrails.OutcomeAllowed
	if !result.Allowed {
		outcome = guardrails.OutcomeBlocked
	}
	evaluationID := s.record(ctx, guardrails.Evaluation{InspectionPoint: guardrails.PointInput,
		ApplicationID: req.GetApplicationId(), PolicyNames: policyNames(policies), Outcome: outcome,
		Triggered: result.Triggered, ContentSHA256: contentHash(req.GetContent()), Context: req.GetContext()})

	return &gogiv1.ValidateInputResponse{
		Allowed:         result.Allowed,
		DenialReason:    result.DenialReason,
		TriggeredChecks: guardrails.RuleTypes(result.Triggered),
		TriggeredRules:  toGRPCTriggeredRules(result.Triggered),
		UserMessage:     result.UserMessage,
		EvaluationId:    evaluationID,
	}, nil
}

func (s *GuardrailsServer) FilterOutput(ctx context.Context, req *gogiv1.FilterOutputRequest) (*gogiv1.FilterOutputResponse, error) {

	policies, err := s.resolve(ctx, req.GetPolicyNames(), req.GetApplicationId(),
		[]string{guardrails.TypeOutput}, "FilterOutput")
	if err != nil {
		return nil, err
	}
	if len(req.GetFilters()) > 0 {
		builtin, err := guardrails.BuiltinOutputPolicy(req.GetFilters())
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "%v", err)
		}
		policies = append(policies, builtin)
	}

	result := guardrails.FilterOutput(policies, req.GetContent())

	outcome := guardrails.OutcomeAllowed
	switch {
	case result.Blocked:
		outcome = guardrails.OutcomeBlocked
	case result.Modified:
		outcome = guardrails.OutcomeModified
	}
	evaluationID := s.record(ctx, guardrails.Evaluation{InspectionPoint: guardrails.PointOutput,
		ApplicationID: req.GetApplicationId(), PolicyNames: policyNames(policies), Outcome: outcome,
		Triggered: result.Triggered, ContentSHA256: contentHash(req.GetContent()), Context: req.GetContext()})

	return &gogiv1.FilterOutputResponse{
		Content:        result.Content,
		Modified:       result.Modified,
		AppliedFilters: result.Applied,
		Blocked:        result.Blocked,
		TriggeredRules: toGRPCTriggeredRules(result.Triggered),
		EvaluationId:   evaluationID,
	}, nil
}

func (s *GuardrailsServer) CheckPolicy(ctx context.Context, req *gogiv1.CheckPolicyRequest) (*gogiv1.CheckPolicyResponse, error) {

	if req.GetAction() == "" {
		return nil, status.Error(codes.InvalidArgument, "action is required")
	}
	if req.GetPolicyName() == "" && req.GetApplicationId() == "" {
		return nil, status.Error(codes.InvalidArgument, "name a policy, or an application whose policies to apply")
	}

	names := make([]string, 0, 1)
	if req.GetPolicyName() != "" {
		names = append(names, req.GetPolicyName())
	}
	policies, err := s.resolve(ctx, names, req.GetApplicationId(),
		[]string{guardrails.TypeToolValidation, guardrails.TypeBehavioral, guardrails.TypeHumanGate}, "CheckPolicy")
	if err != nil {
		return nil, err
	}

	history := []guardrails.PastAction{}
	if sessionID := req.GetContext()[guardrails.ContextSessionID]; sessionID != "" && guardrails.NeedsSessionHistory(policies) {
		if history, err = s.store.SessionActions(ctx, sessionID); err != nil {
			return nil, status.Errorf(codes.Internal, "failed to get the actions of session %q: %v", sessionID, err)
		}
	}

	result, err := guardrails.CheckAction(policies, req.GetAction(), req.GetArgumentsJson(), req.GetContext(),
		history, s.now())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}

	outcome := guardrails.OutcomeAllowed
	switch {
	case result.Allowed:
	case result.SuggestedAction == guardrails.SuggestRequireApproval:
		outcome = guardrails.OutcomeApprovalRequired
	default:
		outcome = guardrails.OutcomeBlocked
	}
	evaluationID := s.record(ctx, guardrails.Evaluation{InspectionPoint: guardrails.PointAction,
		ApplicationID: req.GetApplicationId(), PolicyNames: policyNames(policies), Action: req.GetAction(),
		Outcome: outcome, Triggered: result.Triggered, ContentSHA256: contentHash(req.GetArgumentsJson()),
		Context: req.GetContext()})

	return &gogiv1.CheckPolicyResponse{
		Allowed:         result.Allowed,
		DenialReason:    result.DenialReason,
		ViolatedRules:   result.ViolatedRules,
		SuggestedAction: result.SuggestedAction,
		TriggeredRules:  toGRPCTriggeredRules(result.Triggered),
		ApprovalRole:    result.ApprovalRole,
		EvaluationId:    evaluationID,
	}, nil
}

func (s *GuardrailsServer) ReportViolation(ctx context.Context, req *gogiv1.ReportViolationRequest) (*gogiv1.ReportViolationResponse, error) {

	if req.GetPolicyName() == "" {
		return nil, status.Error(codes.InvalidArgument, "policy_name is required")
	}
	severity := req.GetSeverity()
	if severity == "" {
		severity = defaultSeverity
	}
	if !contains(severities, severity) {
		return nil, status.Errorf(codes.InvalidArgument, "severity %q must be one of %v", severity, severities)
	}

	violation, err := s.store.RecordViolation(ctx, guardrails.Violation{PolicyName: req.GetPolicyName(),
		Action: req.GetAction(), Severity: severity, Context: req.GetContext(), Details: req.GetDetails(),
		EvaluationID: req.GetEvaluationId()})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to record the violation: %v", err)
	}
	return &gogiv1.ReportViolationResponse{ViolationId: violation.ID, Recorded: true}, nil
}

func toGRPCPolicy(policy guardrails.Policy) *gogiv1.Policy {
	return &gogiv1.Policy{
		Name:          policy.Name,
		Type:          policy.Type,
		ApplicationId: policy.ApplicationID,
		Mode:          policy.Mode,
		RulesJson:     policy.RulesJSON,
		Description:   policy.Description,
		Version:       policy.Version,
		Revision:      policy.Revision,
		Source:        policy.Source,
		CreatedAt:     policy.CreatedAt.UnixMilli(),
		UpdatedAt:     policy.UpdatedAt.UnixMilli(),
	}
}

// AddPolicy adds the policy, or replaces the one with the same name. Its rules are
// checked first; every problem is returned at once
func (s *GuardrailsServer) AddPolicy(ctx context.Context, req *gogiv1.AddPolicyRequest) (*gogiv1.AddPolicyResponse, error) {

	if req.GetPolicy() == nil {
		return nil, status.Error(codes.InvalidArgument, "policy is required")
	}
	definition := req.GetPolicy()
	compiled, warnings, err := guardrails.Compile(guardrails.Policy{
		Name:          definition.GetName(),
		Type:          definition.GetType(),
		ApplicationID: definition.GetApplicationId(),
		Mode:          definition.GetMode(),
		RulesJSON:     definition.GetRulesJson(),
		Description:   definition.GetDescription(),
		Version:       definition.GetVersion(),
		Source:        guardrails.SourceAPI,
	})
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}

	stored, err := s.store.UpsertPolicy(ctx, compiled.Policy)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to store policy %q: %v", compiled.Name, err)
	}
	return &gogiv1.AddPolicyResponse{Policy: toGRPCPolicy(stored), Warnings: warnings}, nil
}

func (s *GuardrailsServer) GetPolicy(ctx context.Context, req *gogiv1.GetPolicyRequest) (*gogiv1.GetPolicyResponse, error) {

	if req.GetName() == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}
	policy, err := s.store.GetPolicy(ctx, req.GetName())
	if errors.Is(err, guardrails.ErrPolicyNotFound) {
		return nil, status.Errorf(codes.NotFound, "policy %q not found", req.GetName())
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get policy %q: %v", req.GetName(), err)
	}
	return &gogiv1.GetPolicyResponse{Policy: toGRPCPolicy(policy)}, nil
}

func (s *GuardrailsServer) ListPolicies(ctx context.Context, req *gogiv1.ListPoliciesRequest) (*gogiv1.ListPoliciesResponse, error) {

	policies, err := s.store.ListPolicies(ctx, req.GetApplicationId(), req.GetType())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list policies: %v", err)
	}
	converted := make([]*gogiv1.Policy, 0, len(policies))
	for _, policy := range policies {
		converted = append(converted, toGRPCPolicy(policy))
	}
	return &gogiv1.ListPoliciesResponse{Policies: converted}, nil
}

func (s *GuardrailsServer) DeletePolicy(ctx context.Context, req *gogiv1.DeletePolicyRequest) (*gogiv1.DeletePolicyResponse, error) {

	if req.GetName() == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}
	deleted, err := s.store.DeletePolicy(ctx, req.GetName())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to delete policy %q: %v", req.GetName(), err)
	}
	return &gogiv1.DeletePolicyResponse{Deleted: deleted}, nil
}

func (s *GuardrailsServer) ListViolations(ctx context.Context, req *gogiv1.ListViolationsRequest) (*gogiv1.ListViolationsResponse, error) {

	limit := int(req.GetLimit())
	switch {
	case limit < 0:
		return nil, status.Error(codes.InvalidArgument, "limit must not be negative")
	case limit == 0:
		limit = defaultViolationsLimit
	case limit > maxViolationsLimit:
		limit = maxViolationsLimit
	}
	filter := guardrails.ViolationFilter{PolicyName: req.GetPolicyName(), Severity: req.GetSeverity(), Limit: limit}
	if req.GetSince() > 0 {
		filter.Since = time.UnixMilli(req.GetSince())
	}

	violations, err := s.store.ListViolations(ctx, filter)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list violations: %v", err)
	}
	converted := make([]*gogiv1.Violation, 0, len(violations))
	for _, violation := range violations {
		converted = append(converted, &gogiv1.Violation{
			ViolationId:  violation.ID,
			PolicyName:   violation.PolicyName,
			Action:       violation.Action,
			Severity:     violation.Severity,
			Context:      violation.Context,
			Details:      violation.Details,
			EvaluationId: violation.EvaluationID,
			CreatedAt:    violation.CreatedAt.UnixMilli(),
		})
	}
	return &gogiv1.ListViolationsResponse{Violations: converted}, nil
}

// LoadPolicyFiles loads the policies of the YAML files in dir into the store. An invalid
// policy file fails the load, so a broken policy never goes unnoticed
func LoadPolicyFiles(ctx context.Context, store guardrails.Store, dir string) (int, error) {
	policies, warnings, err := guardrails.LoadPolicyDir(dir)
	if err != nil {
		return 0, err
	}
	for _, warning := range warnings {
		Log.Warnf("guardrail policy file %s", warning)
	}
	for _, policy := range policies {
		compiled, _, _ := guardrails.Compile(policy)
		if _, err := store.UpsertPolicy(ctx, compiled.Policy); err != nil {
			return 0, fmt.Errorf("failed to store policy %q: %w", policy.Name, err)
		}
	}
	return len(policies), nil
}
