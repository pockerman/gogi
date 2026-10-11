package guardrails

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Keys of the context that action rules use
const (
	ContextUserID      = "user_id"
	ContextSessionID   = "session_id"
	ContextTurnID      = "turn_id"
	ContextPermissions = "permissions"
)

// --- tool_validation ---

// toolRule constrains the arguments of the tools it names (listing 6.22)
type toolRule struct {
	Tool        string           `json:"tool"`
	Tools       []string         `json:"tools"`
	Severity    string           `json:"severity"`
	Constraints []toolConstraint `json:"constraints"`

	patterns []string
}

type toolConstraint struct {
	Field  string      `json:"field"`
	Checks []toolCheck `json:"checks"`
}

// toolCheck is a check of an argument. Only the fields of its type are used
type toolCheck struct {
	Type         string   `json:"type"`
	Values       []any    `json:"values"`
	Min          *float64 `json:"min"`
	Max          *float64 `json:"max"`
	Value        *float64 `json:"value"`
	Pattern      string   `json:"pattern"`
	MinDaysAhead int      `json:"min_days_ahead"`
	Days         int      `json:"days"`
	Key          string   `json:"key"`
	Message      string   `json:"message"`

	compiled *regexp.Regexp
}

// unsupportedChecks need data the guardrails service does not have, e.g. a registry to look up
var unsupportedChecks = map[string]bool{"exists_in_system": true, "custom": true, "business_hours": true}

var toolCheckTypes = []string{"required", "in_list", "not_in_list", "range", "min_length", "max_length",
	"pattern", "future_date", "max_days_ahead", "matches_session_user", "matches_context"}

func compileToolRule(raw json.RawMessage, at string, found *problems) (toolRule, bool) {

	var rule toolRule
	if !decodeRule(raw, &rule, at, found, "tool", "tools", "severity", "constraints") {
		return rule, false
	}
	rule.patterns = rule.Tools
	if rule.Tool != "" {
		rule.patterns = append([]string{rule.Tool}, rule.patterns...)
	}

	ok := true
	if len(rule.patterns) == 0 {
		found.errorf("%s: needs a tool, or tools", at)
		ok = false
	} else if err := validPatterns(rule.patterns); err != nil {
		found.errorf("%s: %v", at, err)
		ok = false
	}
	if len(rule.Constraints) == 0 {
		found.errorf("%s: needs constraints", at)
		ok = false
	}

	for i := range rule.Constraints {
		constraint := &rule.Constraints[i]
		where := fmt.Sprintf("%s, constraint %d", at, i+1)
		if constraint.Field == "" {
			found.errorf("%s: needs a field", where)
			ok = false
		}
		kept := make([]toolCheck, 0, len(constraint.Checks))
		for _, check := range constraint.Checks {
			if keep, valid := compileToolCheck(&check, where, found); keep {
				kept = append(kept, check)
			} else if !valid {
				ok = false
			}
		}
		constraint.Checks = kept
	}
	return rule, ok
}

// compileToolCheck returns whether to keep the check, and whether it is valid; checks that
// are valid but not supported are dropped with a warning
func compileToolCheck(check *toolCheck, at string, found *problems) (bool, bool) {
	switch check.Type {
	case "in_list", "not_in_list":
		if len(check.Values) == 0 {
			// the book's in_list can name a source of values, which needs a lookup
			found.warnf("%s: %s without values is not supported yet; the check is not enforced", at, check.Type)
			return false, true
		}
	case "range":
		if check.Min == nil && check.Max == nil {
			found.errorf("%s: range needs a min, a max or both", at)
			return false, false
		}
	case "min_length", "max_length":
		if check.Value == nil || *check.Value < 0 {
			found.errorf("%s: %s needs a non-negative value", at, check.Type)
			return false, false
		}
	case "pattern":
		compiled, err := regexp.Compile(check.Pattern)
		if err != nil || check.Pattern == "" {
			found.errorf("%s: pattern needs a valid pattern", at)
			return false, false
		}
		check.compiled = compiled
	case "max_days_ahead":
		if check.Days <= 0 {
			found.errorf("%s: max_days_ahead needs a positive days", at)
			return false, false
		}
	case "matches_context":
		if check.Key == "" {
			found.errorf("%s: matches_context needs a key", at)
			return false, false
		}
	case "required", "future_date", "matches_session_user":
	default:
		if unsupportedChecks[check.Type] {
			found.warnf("%s: %s is not supported yet; the check is not enforced", at, check.Type)
			return false, true
		}
		found.errorf("%s: unknown check %q; supported checks are %s", at, check.Type,
			strings.Join(toolCheckTypes, ", "))
		return false, false
	}
	return true, true
}

// violations returns what is wrong with the arguments, one entry per failed check
func (r *toolRule) violations(arguments map[string]any, context map[string]string, now time.Time) []string {
	failed := make([]string, 0)
	for _, constraint := range r.Constraints {
		value, present := lookup(arguments, constraint.Field)
		for _, check := range constraint.Checks {
			if check.Type != "required" && !present {
				continue
			}
			if problem := check.problem(value, present, context, now); problem != "" {
				if check.Message != "" {
					problem = check.Message
				}
				failed = append(failed, constraint.Field+": "+problem)
			}
		}
	}
	return failed
}

// lookup returns the value at a dotted path, e.g. traveller.id
func lookup(arguments map[string]any, field string) (any, bool) {
	var current any = arguments
	for _, part := range strings.Split(field, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		if current, ok = object[part]; !ok {
			return nil, false
		}
	}
	return current, true
}

// problem returns what is wrong with the value, or "" if the check passes
func (c *toolCheck) problem(value any, present bool, context map[string]string, now time.Time) string {
	switch c.Type {
	case "required":
		if !present || value == nil || value == "" {
			return "is required"
		}
	case "in_list":
		if !inValues(c.Values, value) {
			return fmt.Sprintf("must be one of %s", formatValues(c.Values))
		}
	case "not_in_list":
		if inValues(c.Values, value) {
			return fmt.Sprintf("must not be %v", value)
		}
	case "range":
		number, ok := value.(float64)
		if !ok {
			return "must be a number"
		}
		if c.Min != nil && number < *c.Min {
			return fmt.Sprintf("must be at least %v", *c.Min)
		}
		if c.Max != nil && number > *c.Max {
			return fmt.Sprintf("must be at most %v", *c.Max)
		}
	case "min_length", "max_length":
		text, ok := value.(string)
		if !ok {
			return "must be a string"
		}
		length := float64(len([]rune(text)))
		if c.Type == "min_length" && length < *c.Value {
			return fmt.Sprintf("must be at least %v characters", *c.Value)
		}
		if c.Type == "max_length" && length > *c.Value {
			return fmt.Sprintf("must be at most %v characters", *c.Value)
		}
	case "pattern":
		text, ok := value.(string)
		if !ok || !c.compiled.MatchString(text) {
			return "has the wrong format"
		}
	case "future_date", "max_days_ahead":
		date, ok := parseDate(value)
		if !ok {
			return "must be a date, e.g. 2026-11-02"
		}
		ahead := daysBetween(now, date)
		if c.Type == "future_date" && ahead < c.MinDaysAhead {
			if c.MinDaysAhead == 0 {
				return "must not be in the past"
			}
			return fmt.Sprintf("must be at least %s ahead", days(c.MinDaysAhead))
		}
		if c.Type == "max_days_ahead" && ahead > c.Days {
			return fmt.Sprintf("must be at most %s ahead", days(c.Days))
		}
	case "matches_session_user", "matches_context":
		key := c.Key
		if c.Type == "matches_session_user" {
			key = ContextUserID
		}
		expected, ok := context[key]
		if !ok || expected == "" {
			return fmt.Sprintf("cannot be checked: %s is missing from the context", key)
		}
		if fmt.Sprint(value) != expected {
			return fmt.Sprintf("must match the context's %s", key)
		}
	}
	return ""
}

func inValues(values []any, value any) bool {
	for _, v := range values {
		if v == value || fmt.Sprint(v) == fmt.Sprint(value) {
			return true
		}
	}
	return false
}

func formatValues(values []any) string {
	parts := make([]string, 0, len(values))
	for _, v := range values {
		parts = append(parts, fmt.Sprint(v))
	}
	return strings.Join(parts, ", ")
}

func parseDate(value any) (time.Time, bool) {
	text, ok := value.(string)
	if !ok {
		return time.Time{}, false
	}
	for _, layout := range []string{time.DateOnly, time.RFC3339} {
		if date, err := time.Parse(layout, text); err == nil {
			return date, true
		}
	}
	return time.Time{}, false
}

func days(count int) string {
	if count == 1 {
		return "1 day"
	}
	return fmt.Sprintf("%d days", count)
}

// daysBetween counts the calendar days from now to date, in UTC
func daysBetween(now, date time.Time) int {
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	day := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)
	return int(math.Round(day.Sub(today).Hours() / 24))
}

// --- behavioral ---

// behaviorRule controls whether an action may run, given its context and the actions
// allowed earlier in its session (listing 6.24)
type behaviorRule struct {
	Type            string   `json:"type"`
	Action          string   `json:"action"`
	Actions         []string `json:"actions"`
	Fields          []string `json:"fields"`
	Field           string   `json:"field"`
	Permissions     []string `json:"permissions"`
	Limit           int      `json:"limit"`
	Scope           string   `json:"scope"`
	Severity        string   `json:"severity"`
	Message         string   `json:"message"`
	SuggestedAction string   `json:"suggested_action"`
}

var behaviorTypes = []string{"blocked_action", "allowed_actions", "required_context", "requires_permission",
	"max_per_session", "conflicting_actions"}

func compileBehaviorRule(raw json.RawMessage, at string, found *problems) (behaviorRule, bool) {

	var rule behaviorRule
	if !decodeRule(raw, &rule, at, found, "type", "action", "actions", "fields", "field", "permissions",
		"limit", "scope", "severity", "message", "suggested_action") {
		return rule, false
	}
	if rule.Action != "" {
		rule.Actions = append([]string{rule.Action}, rule.Actions...)
	}
	// the book's reference implementation calls required_context required_field
	if rule.Type == "required_field" {
		rule.Type = "required_context"
	}
	if rule.Field != "" {
		rule.Fields = append([]string{rule.Field}, rule.Fields...)
	}

	ok := true
	if err := validPatterns(rule.Actions); err != nil {
		found.errorf("%s: %v", at, err)
		ok = false
	}
	switch rule.Type {
	case "blocked_action", "allowed_actions":
		if len(rule.Actions) == 0 {
			found.errorf("%s: %s needs actions", at, rule.Type)
			ok = false
		}
	case "required_context":
		if len(rule.Fields) == 0 {
			found.errorf("%s: required_context needs fields", at)
			ok = false
		}
	case "requires_permission":
		if len(rule.Permissions) == 0 {
			found.errorf("%s: requires_permission needs permissions", at)
			ok = false
		}
	case "max_per_session":
		if rule.Limit <= 0 || len(rule.Actions) == 0 {
			found.errorf("%s: max_per_session needs actions and a positive limit", at)
			ok = false
		}
	case "conflicting_actions":
		if len(rule.Actions) < 2 {
			found.errorf("%s: conflicting_actions needs at least two actions", at)
			ok = false
		}
		if rule.Scope == "" {
			rule.Scope = "turn"
		}
		if rule.Scope != "turn" && rule.Scope != "session" {
			found.errorf("%s: scope %q must be turn or session", at, rule.Scope)
			ok = false
		}
	default:
		found.errorf("%s: unknown type %q; supported types are %s", at, rule.Type, strings.Join(behaviorTypes, ", "))
		ok = false
	}
	return rule, ok
}

// PastAction is an action allowed earlier in a session
type PastAction struct {
	Action string
	TurnID string
}

// violation returns why the action is not allowed, or "" if the rule allows it
func (r *behaviorRule) violation(action string, context map[string]string, history []PastAction) string {
	// rules other than the allow list apply to the actions they name, or to every action
	applies := len(r.Actions) == 0 || matchesAny(r.Actions, action)

	switch r.Type {
	case "blocked_action":
		if applies {
			return fmt.Sprintf("%s is not allowed", action)
		}
	case "allowed_actions":
		if !matchesAny(r.Actions, action) {
			return fmt.Sprintf("%s is not one of the allowed actions", action)
		}
	case "required_context":
		if !applies {
			return ""
		}
		missing := make([]string, 0)
		for _, field := range r.Fields {
			if context[field] == "" {
				missing = append(missing, field)
			}
		}
		if len(missing) > 0 {
			return "missing from the context: " + strings.Join(missing, ", ")
		}
	case "requires_permission":
		if !applies {
			return ""
		}
		granted := strings.Split(context[ContextPermissions], ",")
		for i := range granted {
			granted[i] = strings.TrimSpace(granted[i])
		}
		missing := make([]string, 0)
		for _, permission := range r.Permissions {
			if !contains(granted, permission) {
				missing = append(missing, permission)
			}
		}
		if len(missing) > 0 {
			return "missing permissions: " + strings.Join(missing, ", ")
		}
	case "max_per_session":
		if !applies {
			return ""
		}
		if context[ContextSessionID] == "" {
			return "cannot be checked: session_id is missing from the context"
		}
		count := 0
		for _, past := range history {
			if matchesAny(r.Actions, past.Action) {
				count++
			}
		}
		if count >= r.Limit {
			return fmt.Sprintf("the session already ran %d of at most %d", count, r.Limit)
		}
	case "conflicting_actions":
		if !contains(r.Actions, action) {
			return ""
		}
		if context[ContextSessionID] == "" || (r.Scope == "turn" && context[ContextTurnID] == "") {
			missing := ContextSessionID
			if context[ContextSessionID] != "" {
				missing = ContextTurnID
			}
			return fmt.Sprintf("cannot be checked: %s is missing from the context", missing)
		}
		for _, past := range history {
			if past.Action == action || !contains(r.Actions, past.Action) {
				continue
			}
			if r.Scope == "session" || past.TurnID == context[ContextTurnID] {
				return fmt.Sprintf("conflicts with %s earlier in this %s", past.Action, r.Scope)
			}
		}
	}
	return ""
}

// --- human_gate ---

// gateRule makes the actions it names, under its condition, wait for a person's approval (listing 6.25)
type gateRule struct {
	Tool         string   `json:"tool"`
	Tools        []string `json:"tools"`
	Condition    string   `json:"condition"`
	ApprovalRole string   `json:"approval_role"`
	TimeoutHours float64  `json:"timeout_hours"`
	OnTimeout    string   `json:"on_timeout"`
	Severity     string   `json:"severity"`

	patterns   []string
	conditions []comparison
}

func compileGateRule(raw json.RawMessage, at string, found *problems) (gateRule, bool) {

	var rule gateRule
	if !decodeRule(raw, &rule, at, found, "tool", "tools", "condition", "approval_role", "timeout_hours",
		"on_timeout", "severity") {
		return rule, false
	}
	rule.patterns = rule.Tools
	if rule.Tool != "" {
		rule.patterns = append([]string{rule.Tool}, rule.patterns...)
	}

	ok := true
	if len(rule.patterns) == 0 {
		found.errorf("%s: needs a tool, or tools", at)
		ok = false
	} else if err := validPatterns(rule.patterns); err != nil {
		found.errorf("%s: %v", at, err)
		ok = false
	}
	if rule.ApprovalRole == "" {
		found.errorf("%s: needs an approval_role", at)
		ok = false
	}
	if rule.Condition != "" {
		conditions, err := parseCondition(rule.Condition)
		if err != nil {
			found.errorf("%s: %v", at, err)
			ok = false
		}
		rule.conditions = conditions
	}
	if rule.TimeoutHours > 0 || rule.OnTimeout != "" {
		found.warnf("%s: timeout_hours and on_timeout are for the approval workflow, which is not "+
			"part of the guardrails service yet", at)
	}
	return rule, ok
}

// applies returns true if the action needs approval, and why. A condition on an argument
// the call does not have counts as met: approval is the safe default
func (r *gateRule) applies(action string, arguments map[string]any) (bool, string) {
	if !matchesAny(r.patterns, action) {
		return false, ""
	}
	if len(r.conditions) == 0 {
		return true, fmt.Sprintf("%s needs the approval of a %s", action, r.ApprovalRole)
	}
	for _, condition := range r.conditions {
		met, known := condition.evaluate(arguments)
		if !known {
			return true, fmt.Sprintf("%s needs the approval of a %s: %s cannot be checked, %s is missing",
				action, r.ApprovalRole, r.Condition, condition.field)
		}
		if !met {
			return false, ""
		}
	}
	return true, fmt.Sprintf("%s needs the approval of a %s, as %s", action, r.ApprovalRole, r.Condition)
}

// comparison is a condition on an argument, e.g. total > 1500
type comparison struct {
	field    string
	operator string
	value    any
}

var comparisonPattern = regexp.MustCompile(`^\s*([A-Za-z_][\w.]*)\s*(==|!=|>=|<=|>|<)\s*(.+?)\s*$`)

// parseCondition parses comparisons of an argument with a number, a quoted string or a
// boolean, joined with "and", e.g. total > 1500 and currency == 'EUR'
func parseCondition(condition string) ([]comparison, error) {
	comparisons := make([]comparison, 0)
	for _, part := range regexp.MustCompile(`\s+and\s+`).Split(condition, -1) {
		match := comparisonPattern.FindStringSubmatch(part)
		if match == nil {
			return nil, fmt.Errorf("condition %q: %q is not <argument> <operator> <value>", condition, part)
		}
		value, err := parseLiteral(match[3])
		if err != nil {
			return nil, fmt.Errorf("condition %q: %v", condition, err)
		}
		if _, isNumber := value.(float64); !isNumber && match[2] != "==" && match[2] != "!=" {
			return nil, fmt.Errorf("condition %q: %s compares numbers only", condition, match[2])
		}
		comparisons = append(comparisons, comparison{field: match[1], operator: match[2], value: value})
	}
	return comparisons, nil
}

func parseLiteral(text string) (any, error) {
	if len(text) >= 2 && (text[0] == '\'' || text[0] == '"') && text[len(text)-1] == text[0] {
		return text[1 : len(text)-1], nil
	}
	if text == "true" || text == "false" {
		return text == "true", nil
	}
	if number, err := strconv.ParseFloat(text, 64); err == nil {
		return number, nil
	}
	return nil, fmt.Errorf("%q is not a number, a quoted string or a boolean", text)
}

// evaluate returns whether the comparison holds, and whether it could be evaluated
func (c comparison) evaluate(arguments map[string]any) (bool, bool) {
	value, present := lookup(arguments, c.field)
	if !present {
		return false, false
	}

	if expected, isNumber := c.value.(float64); isNumber {
		actual, ok := value.(float64)
		if !ok {
			return false, false
		}
		switch c.operator {
		case "==":
			return actual == expected, true
		case "!=":
			return actual != expected, true
		case ">":
			return actual > expected, true
		case ">=":
			return actual >= expected, true
		case "<":
			return actual < expected, true
		case "<=":
			return actual <= expected, true
		}
	}

	equal := value == c.value
	if c.operator == "!=" {
		return !equal, true
	}
	return equal, true
}
