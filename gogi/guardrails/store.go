package guardrails

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"gogi/gogi/utils"
)

// ErrPolicyNotFound is returned for a policy that does not exist
var ErrPolicyNotFound = errors.New("policy not found")

// Inspection points of evaluations
const (
	PointInput  = "input"
	PointOutput = "output"
	PointAction = "action"
)

// Outcomes of evaluations
const (
	OutcomeAllowed          = "allowed"
	OutcomeBlocked          = "blocked"
	OutcomeModified         = "modified"
	OutcomeApprovalRequired = "approval_required"
)

// Evaluation is the audit record of a guardrail evaluation. It holds a hash of the
// content, never the content itself
type Evaluation struct {
	ID              string
	InspectionPoint string
	ApplicationID   string
	PolicyNames     []string
	Action          string
	Outcome         string
	Triggered       []TriggeredRule
	ContentSHA256   string
	SessionID       string
	TurnID          string
	UserID          string
	Context         map[string]string
	CreatedAt       time.Time
}

// Violation is a policy violation an application reported
type Violation struct {
	ID           string
	PolicyName   string
	Action       string
	Severity     string
	Context      map[string]string
	Details      string
	EvaluationID string
	CreatedAt    time.Time
}

// ViolationFilter selects violations; zero values match everything
type ViolationFilter struct {
	PolicyName string
	Severity   string
	Since      time.Time
	Limit      int
}

// Store stores policies and the audit records of evaluations and violations
type Store interface {
	// UpsertPolicy adds the policy, or replaces the one with the same name; the revision
	// is incremented only if the policy changed
	UpsertPolicy(ctx context.Context, policy Policy) (Policy, error)
	// GetPolicy returns the policy, or an error wrapping ErrPolicyNotFound
	GetPolicy(ctx context.Context, name string) (Policy, error)
	// ListPolicies returns the policies of the application and type, ordered by name; an
	// empty application or type matches every one
	ListPolicies(ctx context.Context, applicationID, policyType string) ([]Policy, error)
	DeletePolicy(ctx context.Context, name string) (bool, error)

	RecordEvaluation(ctx context.Context, evaluation Evaluation) (string, error)
	// SessionActions returns the actions allowed in the session, oldest first
	SessionActions(ctx context.Context, sessionID string) ([]PastAction, error)

	RecordViolation(ctx context.Context, violation Violation) (Violation, error)
	// ListViolations returns the matching violations, newest first
	ListViolations(ctx context.Context, filter ViolationFilter) ([]Violation, error)
}

// MemoryStore is a Store that keeps everything in memory, for tests and development
type MemoryStore struct {
	mu          sync.Mutex
	policies    map[string]Policy
	evaluations []Evaluation
	violations  []Violation
	now         func() time.Time
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{policies: map[string]Policy{}, now: time.Now}
}

func (s *MemoryStore) UpsertPolicy(ctx context.Context, policy Policy) (Policy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	existing, ok := s.policies[policy.Name]
	switch {
	case !ok:
		policy.Revision, policy.CreatedAt, policy.UpdatedAt = 1, now, now
	case samePolicy(existing, policy):
		return existing, nil
	default:
		policy.Revision, policy.CreatedAt, policy.UpdatedAt = existing.Revision+1, existing.CreatedAt, now
	}
	s.policies[policy.Name] = policy
	return policy, nil
}

// samePolicy returns true if the policies' definitions are the same
func samePolicy(a, b Policy) bool {
	return a.Type == b.Type && a.ApplicationID == b.ApplicationID && a.Mode == b.Mode &&
		a.RulesJSON == b.RulesJSON && a.Description == b.Description && a.Version == b.Version &&
		a.Source == b.Source
}

func (s *MemoryStore) GetPolicy(ctx context.Context, name string) (Policy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	policy, ok := s.policies[name]
	if !ok {
		return Policy{}, ErrPolicyNotFound
	}
	return policy, nil
}

func (s *MemoryStore) ListPolicies(ctx context.Context, applicationID, policyType string) ([]Policy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	policies := make([]Policy, 0)
	for _, policy := range s.policies {
		if (applicationID == "" || policy.ApplicationID == applicationID) &&
			(policyType == "" || policy.Type == policyType) {
			policies = append(policies, policy)
		}
	}
	sort.Slice(policies, func(i, j int) bool { return policies[i].Name < policies[j].Name })
	return policies, nil
}

func (s *MemoryStore) DeletePolicy(ctx context.Context, name string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, ok := s.policies[name]
	delete(s.policies, name)
	return ok, nil
}

func (s *MemoryStore) RecordEvaluation(ctx context.Context, evaluation Evaluation) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	evaluation.ID, evaluation.CreatedAt = utils.NewUUIDString(), s.now()
	s.evaluations = append(s.evaluations, evaluation)
	return evaluation.ID, nil
}

// Evaluations returns the recorded evaluations, oldest first
func (s *MemoryStore) Evaluations() []Evaluation {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Evaluation(nil), s.evaluations...)
}

func (s *MemoryStore) SessionActions(ctx context.Context, sessionID string) ([]PastAction, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	actions := make([]PastAction, 0)
	for _, evaluation := range s.evaluations {
		if evaluation.InspectionPoint == PointAction && evaluation.SessionID == sessionID &&
			evaluation.Outcome == OutcomeAllowed {
			actions = append(actions, PastAction{Action: evaluation.Action, TurnID: evaluation.TurnID})
		}
	}
	return actions, nil
}

func (s *MemoryStore) RecordViolation(ctx context.Context, violation Violation) (Violation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	violation.ID, violation.CreatedAt = utils.NewUUIDString(), s.now()
	s.violations = append(s.violations, violation)
	return violation, nil
}

func (s *MemoryStore) ListViolations(ctx context.Context, filter ViolationFilter) ([]Violation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	violations := make([]Violation, 0)
	for i := len(s.violations) - 1; i >= 0; i-- {
		violation := s.violations[i]
		if (filter.PolicyName == "" || violation.PolicyName == filter.PolicyName) &&
			(filter.Severity == "" || violation.Severity == filter.Severity) &&
			!violation.CreatedAt.Before(filter.Since) {
			violations = append(violations, violation)
		}
		if filter.Limit > 0 && len(violations) == filter.Limit {
			break
		}
	}
	return violations, nil
}
