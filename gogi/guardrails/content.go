package guardrails

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Detector types of input and output rules
const (
	DetectorPromptInjection = "prompt_injection"
	DetectorPII             = "pii_detection"
	DetectorBlockedTerms    = "blocked_terms"
	DetectorRegex           = "regex"
	DetectorMaxLength       = "max_length"
)

// detectorAliases are other names of the detectors, e.g. those the book uses for output rules
var detectorAliases = map[string]string{
	"pii":           DetectorPII,
	"pii_exposure":  DetectorPII,
	"pii_redaction": DetectorPII,
}

// classifierDetectors need a classifier model; rules using them are accepted, not enforced yet
var classifierDetectors = map[string]bool{
	"topic_classifier": true,
	"medical_advice":   true,
	"factual_error":    true,
	"unverified_claim": true,
	"hallucination":    true,
}

// redactable detectors find the parts of the content to redact
var redactable = map[string]bool{DetectorPII: true, DetectorBlockedTerms: true, DetectorRegex: true}

// PII categories
const (
	PIISSN        = "ssn"
	PIICreditCard = "credit_card"
	PIIEmail      = "email"
	PIIPhone      = "phone"
	PIIIPAddress  = "ip_address"
)

// piiCategories in the order they are detected: a later category does not match text an
// earlier one did, e.g. the digits of a card number are not also a phone number
var piiCategories = []string{PIICreditCard, PIISSN, PIIEmail, PIIPhone, PIIIPAddress}

const defaultReplacement = "[REDACTED]"

// contentRule is a rule of an input or output policy
type contentRule struct {
	Type        string   `json:"type"`
	Types       []string `json:"types"`
	Action      string   `json:"action"`
	Severity    string   `json:"severity"`
	Response    string   `json:"response"`
	Message     string   `json:"message"`
	Fallback    string   `json:"fallback"`
	Categories  []string `json:"categories"`
	Terms       []string `json:"terms"`
	Patterns    []string `json:"patterns"`
	MaxChars    int      `json:"max_chars"`
	Replacement string   `json:"replacement"`

	detectors []string
	compiled  []*regexp.Regexp
}

// userMessage is the message to show the user when the rule blocks or redirects
func (r *contentRule) userMessage() string {
	for _, message := range []string{r.Response, r.Message, r.Fallback} {
		if message != "" {
			return message
		}
	}
	return ""
}

func compileContentRule(raw json.RawMessage, policyType, at string, found *problems) (contentRule, bool) {

	var rule contentRule
	if !decodeRule(raw, &rule, at, found, "type", "types", "action", "severity", "response", "message",
		"fallback", "categories", "terms", "patterns", "max_chars", "replacement", "allowed_topics") {
		return rule, false
	}

	names := rule.Types
	if rule.Type != "" {
		names = append([]string{rule.Type}, names...)
	}
	if len(names) == 0 {
		found.errorf("%s: needs a type, or types", at)
		return rule, false
	}

	ok := true
	for _, name := range names {
		if alias, isAlias := detectorAliases[name]; isAlias {
			name = alias
		}
		switch {
		case classifierDetectors[name]:
			found.warnf("%s: %s needs a classifier, which is not supported yet; the rule is not enforced", at, name)
		case name == DetectorPromptInjection || name == DetectorPII || name == DetectorBlockedTerms ||
			name == DetectorRegex || name == DetectorMaxLength:
			rule.detectors = append(rule.detectors, name)
		default:
			found.errorf("%s: unknown type %q; supported types are %s", at, name, strings.Join(supportedDetectors(), ", "))
			ok = false
		}
	}

	if rule.Action == "" {
		rule.Action = defaultContentAction(policyType, rule.detectors)
	}
	allowed := []string{ActionBlock, ActionRedirect, ActionWarn, ActionLogOnly}
	if policyType == TypeOutput {
		allowed = []string{ActionBlock, ActionRedact, ActionWarn, ActionLogOnly}
	}
	switch {
	case rule.Action == "transform":
		// the book's add_hedging_language transformation needs a model to rewrite the response
		found.warnf("%s: the transform action is not supported yet; the rule only logs", at)
		rule.Action = ActionLogOnly
	case !contains(allowed, rule.Action):
		found.errorf("%s: action %q must be one of %s for %s policies", at, rule.Action,
			strings.Join(allowed, ", "), policyType)
		ok = false
	}

	// extra patterns of prompt_injection, or the patterns of regex
	for _, pattern := range rule.Patterns {
		compiled, err := regexp.Compile(pattern)
		if err != nil {
			found.errorf("%s: invalid pattern %q: %v", at, pattern, err)
			ok = false
			continue
		}
		rule.compiled = append(rule.compiled, compiled)
	}

	for _, detector := range rule.detectors {
		ok = checkDetector(&rule, detector, at, found) && ok
	}
	return rule, ok && len(rule.detectors) > 0
}

func supportedDetectors() []string {
	names := []string{DetectorPromptInjection, DetectorPII, DetectorBlockedTerms, DetectorRegex, DetectorMaxLength}
	for name := range classifierDetectors {
		names = append(names, name)
	}
	sort.Strings(names[5:])
	return names
}

// defaultContentAction blocks input; output is redacted where the detectors can say what to redact
func defaultContentAction(policyType string, detectors []string) string {
	if policyType != TypeOutput {
		return ActionBlock
	}
	for _, detector := range detectors {
		if !redactable[detector] {
			return ActionBlock
		}
	}
	return ActionRedact
}

// checkDetector checks the fields a detector needs
func checkDetector(rule *contentRule, detector, at string, found *problems) bool {
	switch detector {
	case DetectorPII:
		for _, category := range rule.Categories {
			if !contains(piiCategories, category) {
				found.errorf("%s: unknown PII category %q; supported categories are %s", at, category,
					strings.Join(piiCategories, ", "))
				return false
			}
		}
	case DetectorBlockedTerms:
		if len(rule.Terms) == 0 {
			found.errorf("%s: blocked_terms needs terms", at)
			return false
		}
	case DetectorRegex:
		if len(rule.Patterns) == 0 {
			found.errorf("%s: regex needs patterns", at)
			return false
		}
	case DetectorMaxLength:
		if rule.MaxChars <= 0 {
			found.errorf("%s: max_length needs a positive max_chars", at)
			return false
		}
	}

	if rule.Action == ActionRedact && !redactable[detector] {
		found.errorf("%s: %s cannot redact; use block, warn or log_only", at, detector)
		return false
	}
	return true
}

// finding is what a detector found: a description that does not repeat the content, and
// the parts of the content found, as byte offsets, for detectors that can redact
type finding struct {
	detail string
	spans  [][2]int
}

// detect runs the rule's detector on the content
func (r *contentRule) detect(detector, content string) *finding {
	switch detector {
	case DetectorPromptInjection:
		return detectInjection(content, r.compiled)
	case DetectorPII:
		return detectPII(content, r.Categories)
	case DetectorBlockedTerms:
		return detectTerms(content, r.Terms)
	case DetectorRegex:
		return detectPatterns(content, r.compiled)
	case DetectorMaxLength:
		if length := len([]rune(content)); length > r.MaxChars {
			return &finding{detail: fmt.Sprintf("%d characters, more than %d", length, r.MaxChars)}
		}
	}
	return nil
}

// injectionPatterns catch common prompt injection phrasings. They are a first layer only:
// they miss paraphrases, which need a classifier
var injectionPatterns = []struct {
	name    string
	pattern *regexp.Regexp
}{
	{"ignore_instructions", regexp.MustCompile(`(?i)\b(ignore|disregard|forget|override)\s+(all\s+|any\s+)?` +
		`(of\s+)?(the\s+|your\s+|my\s+|these\s+)?(previous|prior|above|earlier|preceding|system|original)\s+` +
		`(instructions|prompts?|rules|directions|guidelines|messages)`)},
	{"mode_switch", regexp.MustCompile(`(?i)\byou\s+are\s+now\s+(in\s+)?(an?\s+)?[\w-]+\s+mode\b|` +
		`\b(developer|dan|jailbreak|god)\s+mode\b`)},
	{"reveal_prompt", regexp.MustCompile(`(?i)\b(reveal|show|print|repeat|output|tell\s+me)\s+(me\s+)?` +
		`(your|the)\s+(system\s+prompt|initial\s+instructions|hidden\s+instructions|instructions\s+above)`)},
	{"system_role", regexp.MustCompile(`(?im)^\s*system\s*:|<\s*/?\s*system\s*>|\[/?(system|inst)\]`)},
	{"no_restrictions", regexp.MustCompile(`(?i)\b(pretend|imagine)\s+(that\s+)?you\s+(are|have)\s+` +
		`(no|without)\s+(rules|restrictions|guidelines|filters)|\bact\s+as\s+(an?\s+)?` +
		`(unrestricted|unfiltered|uncensored|jailbroken)\b`)},
	{"do_anything_now", regexp.MustCompile(`(?i)\bdo\s+anything\s+now\b`)},
}

func detectInjection(content string, extra []*regexp.Regexp) *finding {
	names := make([]string, 0)
	for _, injection := range injectionPatterns {
		if injection.pattern.MatchString(content) {
			names = append(names, injection.name)
		}
	}
	for i, pattern := range extra {
		if pattern.MatchString(content) {
			names = append(names, fmt.Sprintf("pattern %d", i+1))
		}
	}
	if len(names) == 0 {
		return nil
	}
	return &finding{detail: strings.Join(names, ", ")}
}

var piiPatterns = map[string]*regexp.Regexp{
	// 13-19 digits, optionally grouped by spaces or dashes; checked with the Luhn algorithm
	PIICreditCard: regexp.MustCompile(`\b(?:\d[ -]?){12,18}\d\b`),
	PIISSN:        regexp.MustCompile(`\b(\d{3})-(\d{2})-(\d{4})\b`),
	PIIEmail:      regexp.MustCompile(`\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}\b`),
	// numbers with a country code, e.g. +44 20 7946 0958, or North American ones, e.g. (555) 123-4567
	PIIPhone: regexp.MustCompile(`\+\d{1,3}[\s.-]?\(?\d{1,4}\)?(?:[\s.-]?\d{2,4}){2,4}\b|` +
		`(?:\(\d{3}\)\s?|\b\d{3}[\s.-])\d{3}[\s.-]\d{4}\b`),
	PIIIPAddress: regexp.MustCompile(`\b(?:(?:25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)\.){3}` +
		`(?:25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)\b`),
}

// detectPII finds the PII of the given categories, or of every category if none are given
func detectPII(content string, categories []string) *finding {
	if len(categories) == 0 {
		categories = piiCategories
	}

	spans := make([][2]int, 0)
	found := make([]string, 0)
	for _, category := range piiCategories {
		if !contains(categories, category) {
			continue
		}
		matched := false
		for _, span := range piiPatterns[category].FindAllStringSubmatchIndex(content, -1) {
			if overlaps(spans, span[0], span[1]) || !validPII(category, content, span) {
				continue
			}
			spans = append(spans, [2]int{span[0], span[1]})
			matched = true
		}
		if matched {
			found = append(found, category)
		}
	}
	if len(found) == 0 {
		return nil
	}
	return &finding{detail: strings.Join(found, ", "), spans: spans}
}

// validPII rules out matches that cannot be the PII, e.g. numbers that fail the Luhn
// check, or SSNs with an area, group or serial number that is never issued
func validPII(category, content string, span []int) bool {
	switch category {
	case PIICreditCard:
		return luhn(content[span[0]:span[1]])
	case PIISSN:
		area, group, serial := content[span[2]:span[3]], content[span[4]:span[5]], content[span[6]:span[7]]
		return area != "000" && area != "666" && area[0] != '9' && group != "00" && serial != "0000"
	}
	return true
}

// luhn returns true if the digits of number, which may contain spaces and dashes, pass
// the Luhn check that card numbers carry
func luhn(number string) bool {
	digits := make([]int, 0, len(number))
	for _, c := range number {
		if c >= '0' && c <= '9' {
			digits = append(digits, int(c-'0'))
		}
	}
	if len(digits) < 13 || len(digits) > 19 {
		return false
	}

	sum := 0
	for i := len(digits) - 1; i >= 0; i-- {
		digit := digits[i]
		if (len(digits)-1-i)%2 == 1 {
			digit *= 2
			if digit > 9 {
				digit -= 9
			}
		}
		sum += digit
	}
	return sum%10 == 0
}

func overlaps(spans [][2]int, start, end int) bool {
	for _, span := range spans {
		if start < span[1] && span[0] < end {
			return true
		}
	}
	return false
}

// detectTerms finds the terms, as whole words, whatever their case
func detectTerms(content string, terms []string) *finding {
	spans := make([][2]int, 0)
	found := make([]string, 0)
	for _, term := range terms {
		pattern := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(term) + `\b`)
		matches := pattern.FindAllStringIndex(content, -1)
		for _, match := range matches {
			spans = append(spans, [2]int{match[0], match[1]})
		}
		if len(matches) > 0 {
			found = append(found, term)
		}
	}
	if len(found) == 0 {
		return nil
	}
	return &finding{detail: strings.Join(found, ", "), spans: spans}
}

func detectPatterns(content string, patterns []*regexp.Regexp) *finding {
	spans := make([][2]int, 0)
	found := make([]string, 0)
	for i, pattern := range patterns {
		matches := pattern.FindAllStringIndex(content, -1)
		for _, match := range matches {
			spans = append(spans, [2]int{match[0], match[1]})
		}
		if len(matches) > 0 {
			found = append(found, fmt.Sprintf("pattern %d", i+1))
		}
	}
	if len(found) == 0 {
		return nil
	}
	return &finding{detail: strings.Join(found, ", "), spans: spans}
}

// redact replaces the spans of content, merging those that overlap
func redact(content string, spans []redaction) string {
	if len(spans) == 0 {
		return content
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].start < spans[j].start })

	var out strings.Builder
	position := 0
	for i := 0; i < len(spans); i++ {
		start, end, replacement := spans[i].start, spans[i].end, spans[i].replacement
		for i+1 < len(spans) && spans[i+1].start < end {
			i++
			end = max(end, spans[i].end)
		}
		if start < position {
			start = position
		}
		out.WriteString(content[position:start])
		out.WriteString(replacement)
		position = end
	}
	out.WriteString(content[position:])
	return out.String()
}

// redaction is a part of the content to replace
type redaction struct {
	start, end  int
	replacement string
}
