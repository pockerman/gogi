package guardrails

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// BuiltinPolicyName is the policy name reported for the built-in checks and filters a
// request names, which are not part of any policy
const BuiltinPolicyName = "builtin"

// builtinChecks are the checks ValidateInput can run without a policy, blocking what they find
var builtinChecks = []string{DetectorPromptInjection, DetectorPII}

// builtinFilters are the filters FilterOutput can apply without a policy, redacting what they find
var builtinFilters = []string{"pii_redaction"}

// TriggeredRule is a rule that triggered during an evaluation
type TriggeredRule struct {
	PolicyName string `json:"policy_name"`
	RuleType   string `json:"rule_type"`
	Action     string `json:"action"`
	Severity   string `json:"severity,omitempty"`
	Detail     string `json:"detail,omitempty"`
	Shadow     bool   `json:"shadow,omitempty"`
}

func (t TriggeredRule) String() string {
	text := t.RuleType
	if t.Detail != "" {
		text += ": " + t.Detail
	}
	return text
}

// enforced returns the triggered rules that are not in shadow mode
func enforced(rules []TriggeredRule) []TriggeredRule {
	kept := make([]TriggeredRule, 0, len(rules))
	for _, rule := range rules {
		if !rule.Shadow {
			kept = append(kept, rule)
		}
	}
	return kept
}

// RuleTypes returns the distinct types of the enforced rules, in the order they triggered
func RuleTypes(rules []TriggeredRule) []string {
	types := make([]string, 0)
	for _, rule := range enforced(rules) {
		if !contains(types, rule.RuleType) {
			types = append(types, rule.RuleType)
		}
	}
	return types
}

// BuiltinInputPolicy returns a policy that runs the given built-in checks, blocking what they find
func BuiltinInputPolicy(checks []string) (*CompiledPolicy, error) {
	return builtinPolicy(TypeInput, checks, builtinChecks, ActionBlock)
}

// BuiltinOutputPolicy returns a policy that applies the given built-in filters, redacting what they find
func BuiltinOutputPolicy(filters []string) (*CompiledPolicy, error) {
	return builtinPolicy(TypeOutput, filters, builtinFilters, ActionRedact)
}

func builtinPolicy(policyType string, names, supported []string, action string) (*CompiledPolicy, error) {
	rules := make([]map[string]string, 0, len(names))
	for _, name := range names {
		canonical := name
		if alias, ok := detectorAliases[name]; ok {
			canonical = alias
		}
		if !contains(supported, name) && !contains(supported, canonical) &&
			!(policyType == TypeOutput && canonical == DetectorPII) {
			return nil, fmt.Errorf("%w: unknown built-in %s %q; supported ones are %s", ErrInvalidPolicy,
				map[string]string{TypeInput: "check", TypeOutput: "filter"}[policyType], name,
				strings.Join(supported, ", "))
		}
		rules = append(rules, map[string]string{"type": canonical, "action": action})
	}
	rulesJSON, _ := json.Marshal(rules)
	compiled, _, err := Compile(Policy{Name: BuiltinPolicyName, Type: policyType, RulesJSON: string(rulesJSON)})
	return compiled, err
}

// InputResult is the outcome of checking input
type InputResult struct {
	Allowed      bool
	DenialReason string
	UserMessage  string
	Triggered    []TriggeredRule
}

// EvaluateInput checks the content against input policies. A rule that blocks or redirects
// denies the input, unless its policy is in shadow mode; the first such rule's message is
// the one to show the user
func EvaluateInput(policies []*CompiledPolicy, content string) InputResult {

	result := InputResult{Allowed: true, Triggered: make([]TriggeredRule, 0)}
	denials := make([]string, 0)
	for _, policy := range policies {
		for i := range policy.content {
			rule := &policy.content[i]
			for _, detector := range rule.detectors {
				found := rule.detect(detector, content)
				if found == nil {
					continue
				}
				triggered := TriggeredRule{PolicyName: policy.Name, RuleType: detector, Action: rule.Action,
					Severity: rule.Severity, Detail: found.detail, Shadow: policy.Shadow()}
				result.Triggered = append(result.Triggered, triggered)

				if policy.Shadow() || (rule.Action != ActionBlock && rule.Action != ActionRedirect) {
					continue
				}
				if result.Allowed {
					result.UserMessage = rule.userMessage()
				}
				result.Allowed = false
				denials = append(denials, fmt.Sprintf("%s (%s)", triggered, policy.Name))
			}
		}
	}
	if !result.Allowed {
		result.DenialReason = "blocked by " + strings.Join(denials, "; ")
	}
	return result
}

// OutputResult is the outcome of filtering output
type OutputResult struct {
	Content   string
	Modified  bool
	Blocked   bool
	Applied   []string
	Triggered []TriggeredRule
}

// defaultFallback replaces a blocked response whose rule has no fallback
const defaultFallback = "I'm sorry, I can't share that response."

// FilterOutput checks the content against output policies: rules that redact replace what
// they find, and a rule that blocks replaces the whole response with its fallback. Rules
// of policies in shadow mode are recorded, never applied
func FilterOutput(policies []*CompiledPolicy, content string) OutputResult {

	result := OutputResult{Content: content, Applied: make([]string, 0), Triggered: make([]TriggeredRule, 0)}
	redactions := make([]redaction, 0)
	fallback := ""
	for _, policy := range policies {
		for i := range policy.content {
			rule := &policy.content[i]
			for _, detector := range rule.detectors {
				found := rule.detect(detector, content)
				if found == nil {
					continue
				}
				result.Triggered = append(result.Triggered, TriggeredRule{PolicyName: policy.Name,
					RuleType: detector, Action: rule.Action, Severity: rule.Severity, Detail: found.detail,
					Shadow: policy.Shadow()})
				if policy.Shadow() {
					continue
				}

				switch rule.Action {
				case ActionRedact:
					replacement := rule.Replacement
					if replacement == "" {
						replacement = defaultReplacement
					}
					for _, span := range found.spans {
						redactions = append(redactions, redaction{start: span[0], end: span[1], replacement: replacement})
					}
					addApplied(&result, policyFilterName(policy, detector))
				case ActionBlock:
					if !result.Blocked {
						fallback = rule.userMessage()
						if fallback == "" {
							fallback = defaultFallback
						}
					}
					result.Blocked = true
					addApplied(&result, policyFilterName(policy, detector))
				}
			}
		}
	}

	switch {
	case result.Blocked:
		result.Content = fallback
	case len(redactions) > 0:
		result.Content = redact(content, redactions)
	}
	result.Modified = result.Content != content
	return result
}

// policyFilterName is the name of an applied filter: the built-in filter's name, or the detector's
func policyFilterName(policy *CompiledPolicy, detector string) string {
	if policy.Name == BuiltinPolicyName && detector == DetectorPII {
		return "pii_redaction"
	}
	return detector
}

func addApplied(result *OutputResult, name string) {
	if !contains(result.Applied, name) {
		result.Applied = append(result.Applied, name)
	}
}

// ActionResult is the outcome of checking an action
type ActionResult struct {
	Allowed         bool
	DenialReason    string
	ViolatedRules   []string
	SuggestedAction string
	ApprovalRole    string
	Triggered       []TriggeredRule
}

// SuggestRequireApproval is the suggested action when a human approval gate applies
const SuggestRequireApproval = "require_approval"

const defaultSuggestedAction = "fix the violations and try again"

// NeedsSessionHistory returns true if checking actions against the policies needs the
// actions allowed earlier in the session
func NeedsSessionHistory(policies []*CompiledPolicy) bool {
	for _, policy := range policies {
		if policy.needsSessionHistory() {
			return true
		}
	}
	return false
}

// CheckAction checks an action and its arguments, a JSON object, against tool validation,
// behavioural and human approval policies. history holds the actions allowed earlier in
// the session, oldest first
func CheckAction(policies []*CompiledPolicy, action, argumentsJSON string, context map[string]string,
	history []PastAction, now time.Time) (ActionResult, error) {

	arguments := map[string]any{}
	if strings.TrimSpace(argumentsJSON) != "" {
		if err := json.Unmarshal([]byte(argumentsJSON), &arguments); err != nil {
			return ActionResult{}, fmt.Errorf("arguments_json must be a JSON object: %v", err)
		}
	}

	result := ActionResult{Allowed: true, ViolatedRules: make([]string, 0), Triggered: make([]TriggeredRule, 0)}
	denials := make([]string, 0)
	deny := func(policy *CompiledPolicy, triggered TriggeredRule, suggestion string) {
		triggered.Shadow = policy.Shadow()
		result.Triggered = append(result.Triggered, triggered)
		if policy.Shadow() {
			return
		}
		result.Allowed = false
		result.ViolatedRules = append(result.ViolatedRules, triggered.String())
		denials = append(denials, fmt.Sprintf("%s (%s)", triggered, policy.Name))
		if result.SuggestedAction == "" || result.SuggestedAction == SuggestRequireApproval {
			result.SuggestedAction = suggestion
		}
	}

	for _, policy := range policies {
		for i := range policy.tools {
			rule := &policy.tools[i]
			if !matchesAny(rule.patterns, action) {
				continue
			}
			for _, problem := range rule.violations(arguments, context, now) {
				deny(policy, TriggeredRule{PolicyName: policy.Name, RuleType: TypeToolValidation,
					Action: ActionBlock, Severity: rule.Severity, Detail: problem}, defaultSuggestedAction)
			}
		}

		for i := range policy.behaviors {
			rule := &policy.behaviors[i]
			problem := rule.violation(action, context, history)
			if problem == "" {
				continue
			}
			if rule.Message != "" {
				problem = rule.Message
			}
			suggestion := rule.SuggestedAction
			if suggestion == "" {
				suggestion = defaultBehaviorSuggestion(rule.Type)
			}
			deny(policy, TriggeredRule{PolicyName: policy.Name, RuleType: rule.Type, Action: ActionBlock,
				Severity: rule.Severity, Detail: problem}, suggestion)
		}

		for i := range policy.gates {
			rule := &policy.gates[i]
			applies, why := rule.applies(action, arguments)
			if !applies {
				continue
			}
			before := result.SuggestedAction
			deny(policy, TriggeredRule{PolicyName: policy.Name, RuleType: TypeHumanGate,
				Action: ActionRequireApproval, Severity: rule.Severity, Detail: why}, SuggestRequireApproval)
			if !policy.Shadow() && result.ApprovalRole == "" {
				result.ApprovalRole = rule.ApprovalRole
			}
			// another violation's suggestion comes first: approval cannot fix it
			if before != "" && before != SuggestRequireApproval {
				result.SuggestedAction = before
			}
		}
	}

	if !result.Allowed {
		result.DenialReason = "denied by " + strings.Join(denials, "; ")
	}
	return result, nil
}

func defaultBehaviorSuggestion(ruleType string) string {
	switch ruleType {
	case "requires_permission":
		return "contact an administrator for access"
	case "max_per_session":
		return "start a new session to continue"
	case "conflicting_actions":
		return "ask the user to clarify their intent"
	case "required_context":
		return "provide the missing context"
	}
	return "do not retry the action"
}

// SortPolicies orders policies by name, so evaluations are deterministic
func SortPolicies(policies []*CompiledPolicy) {
	sort.Slice(policies, func(i, j int) bool { return policies[i].Name < policies[j].Name })
}
