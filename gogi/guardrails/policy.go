package guardrails

import (
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Policy types
const (
	// TypeInput policies check content before it reaches a model (inspection point 1)
	TypeInput = "input"
	// TypeOutput policies check, redact or block a response before it reaches a user (point 5)
	TypeOutput = "output"
	// TypeToolValidation policies check the arguments of a tool call (point 3)
	TypeToolValidation = "tool_validation"
	// TypeBehavioral policies check whether an action may run in its session (point 2)
	TypeBehavioral = "behavioral"
	// TypeHumanGate policies require a person to approve high-stakes actions (point 2)
	TypeHumanGate = "human_gate"
)

// Policy modes
const (
	ModeEnforce = "enforce"
	// ModeShadow policies are evaluated and recorded, but never block or change anything
	ModeShadow = "shadow"
)

// What a triggered rule does
const (
	ActionBlock           = "block"
	ActionRedirect        = "redirect"
	ActionWarn            = "warn"
	ActionRedact          = "redact"
	ActionLogOnly         = "log_only"
	ActionRequireApproval = "require_approval"
)

// Policy sources
const (
	SourceAPI  = "api"
	SourceFile = "file"
)

// ErrInvalidPolicy is returned for a policy whose definition or rules are invalid
var ErrInvalidPolicy = errors.New("invalid policy")

var policyNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)

// Policy is a named set of rules; the shape of its rules, RulesJSON, depends on its type
type Policy struct {
	Name          string
	Type          string
	ApplicationID string
	Mode          string
	RulesJSON     string
	Description   string
	Version       string
	Revision      int32
	Source        string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// ContentPolicyType returns true if the policy type checks content rather than actions
func ContentPolicyType(policyType string) bool {
	return policyType == TypeInput || policyType == TypeOutput
}

// ActionPolicyType returns true if the policy type checks actions rather than content
func ActionPolicyType(policyType string) bool {
	return policyType == TypeToolValidation || policyType == TypeBehavioral || policyType == TypeHumanGate
}

// CompiledPolicy is a policy whose rules are parsed and ready to evaluate
type CompiledPolicy struct {
	Policy
	content   []contentRule
	tools     []toolRule
	behaviors []behaviorRule
	gates     []gateRule
}

// Shadow returns true if the policy is evaluated without being enforced
func (p *CompiledPolicy) Shadow() bool {
	return p.Mode == ModeShadow
}

// needsSessionHistory returns true if evaluating the policy needs the actions allowed earlier
func (p *CompiledPolicy) needsSessionHistory() bool {
	for _, rule := range p.behaviors {
		if rule.Type == "max_per_session" || rule.Type == "conflicting_actions" {
			return true
		}
	}
	return false
}

// problems collects what is wrong with a policy's rules, and the warnings about rules that
// are accepted but not enforced
type problems struct {
	errors   []string
	warnings []string
}

func (p *problems) errorf(format string, args ...any) {
	p.errors = append(p.errors, fmt.Sprintf(format, args...))
}

func (p *problems) warnf(format string, args ...any) {
	p.warnings = append(p.warnings, fmt.Sprintf(format, args...))
}

// Compile checks the policy and parses its rules. It returns the warnings about rules
// that are accepted but not enforced, e.g. those that need a classifier. The error wraps
// ErrInvalidPolicy and lists every problem found
func Compile(policy Policy) (*CompiledPolicy, []string, error) {

	found := &problems{}
	if !policyNamePattern.MatchString(policy.Name) {
		found.errorf("name %q must be lowercase letters, digits, '.', '_' or '-', up to 128 characters",
			policy.Name)
	}
	if policy.Mode == "" {
		policy.Mode = ModeEnforce
	}
	if policy.Mode != ModeEnforce && policy.Mode != ModeShadow {
		found.errorf("mode %q must be %s or %s", policy.Mode, ModeEnforce, ModeShadow)
	}

	var rules []json.RawMessage
	if err := json.Unmarshal([]byte(policy.RulesJSON), &rules); err != nil {
		found.errorf("rules must be a JSON array of objects: %v", err)
	} else if len(rules) == 0 {
		found.errorf("a policy needs at least one rule")
	}

	compiled := &CompiledPolicy{Policy: policy}
	for i, raw := range rules {
		at := fmt.Sprintf("rule %d", i+1)
		switch policy.Type {
		case TypeInput, TypeOutput:
			if rule, ok := compileContentRule(raw, policy.Type, at, found); ok {
				compiled.content = append(compiled.content, rule)
			}
		case TypeToolValidation:
			if rule, ok := compileToolRule(raw, at, found); ok {
				compiled.tools = append(compiled.tools, rule)
			}
		case TypeBehavioral:
			if rule, ok := compileBehaviorRule(raw, at, found); ok {
				compiled.behaviors = append(compiled.behaviors, rule)
			}
		case TypeHumanGate:
			if rule, ok := compileGateRule(raw, at, found); ok {
				compiled.gates = append(compiled.gates, rule)
			}
		}
	}
	if !ContentPolicyType(policy.Type) && !ActionPolicyType(policy.Type) {
		found.errorf("type %q must be one of %s, %s, %s, %s or %s", policy.Type,
			TypeInput, TypeOutput, TypeToolValidation, TypeBehavioral, TypeHumanGate)
	}

	if len(found.errors) > 0 {
		return nil, found.warnings, fmt.Errorf("%w %q: %s", ErrInvalidPolicy, policy.Name,
			strings.Join(found.errors, "; "))
	}
	return compiled, found.warnings, nil
}

// decodeRule decodes a rule into target and warns about the keys it does not know
func decodeRule(raw json.RawMessage, target any, at string, found *problems, known ...string) bool {
	if err := json.Unmarshal(raw, target); err != nil {
		found.errorf("%s: must be an object with the documented fields: %v", at, err)
		return false
	}

	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		return false
	}
	unknown := make([]string, 0)
	for key := range keys {
		if !contains(known, key) {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		found.warnf("%s: ignored fields %s", at, strings.Join(unknown, ", "))
	}
	return true
}

func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}

// matchesAny returns true if name matches one of the patterns, e.g.
// "healthcare.scheduling.*"; "*" matches everything
func matchesAny(patterns []string, name string) bool {
	for _, pattern := range patterns {
		if pattern == "*" || pattern == name {
			return true
		}
		if ok, err := path.Match(pattern, name); err == nil && ok {
			return true
		}
	}
	return false
}

func validPatterns(patterns []string) error {
	for _, pattern := range patterns {
		if _, err := path.Match(pattern, ""); err != nil {
			return fmt.Errorf("invalid pattern %q", pattern)
		}
	}
	return nil
}
