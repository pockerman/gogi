package tools

import (
	"encoding/json"
	"errors"
	"fmt"
	"gogi/gogi/storage/postgres"
	"gogi/gogi/utils"
	"net/url"
	"regexp"
	"strings"

	"github.com/Masterminds/semver/v3"
)

const (
	// DefaultToolVersion is the version of a tool registered without one
	DefaultToolVersion = "1.0.0"
	// DefaultToolOwner is the owner of a tool registered without one
	DefaultToolOwner = "gogi"
)

// ErrInvalidDefinition is returned for a tool definition that cannot be registered
var ErrInvalidDefinition = errors.New("invalid tool definition")

// ErrIncompatibleVersion is returned for a new minor or patch version of a tool that
// would break the callers of the previous version: it needs a new major version
var ErrIncompatibleVersion = errors.New("incompatible tool version")

// toolNamePattern matches fully qualified tool names: dot separated segments such as
// "healthcare.scheduling.book_appointment"
var toolNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]+(\.[A-Za-z0-9_-]+)*$`)

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidDefinition, fmt.Sprintf(format, args...))
}

// ValidateToolName checks that name is a fully qualified tool name
func ValidateToolName(name string) error {
	if !toolNamePattern.MatchString(name) {
		return invalid("name %q must be dot separated segments of letters, digits, '_' and '-', "+
			"e.g. \"healthcare.scheduling.book_appointment\"", name)
	}
	return nil
}

// NormalizeDefinition validates a tool definition before it is registered and fills
// in the defaults: version 1.0.0, owner "gogi", and a schema without parameters
func NormalizeDefinition(tool *postgres.GogiTool) error {
	if err := ValidateToolName(tool.Name); err != nil {
		return err
	}

	if tool.Version == "" {
		tool.Version = DefaultToolVersion
	}
	if _, err := semver.StrictNewVersion(tool.Version); err != nil {
		return invalid("version %q must be a semantic version, e.g. \"1.2.0\"", tool.Version)
	}
	if tool.Owner == "" {
		tool.Owner = DefaultToolOwner
	}
	if strings.TrimSpace(tool.Description) == "" {
		return invalid("a description is required: models read it to decide when to call the tool")
	}

	if strings.TrimSpace(tool.ParametersJSON) == "" {
		tool.ParametersJSON = DefaultParametersJSON
	}
	if _, err := CompileSchema(tool.ParametersJSON); err != nil {
		return invalid("parameters_json is not a valid JSON Schema: %v", err)
	}
	var parameters struct {
		Type any `json:"type"`
	}
	if err := json.Unmarshal([]byte(tool.ParametersJSON), &parameters); err != nil || parameters.Type != "object" {
		return invalid("parameters_json must be the schema of an object, with \"type\": \"object\"")
	}
	if strings.TrimSpace(tool.ReturnsJSON) != "" {
		if _, err := CompileSchema(tool.ReturnsJSON); err != nil {
			return invalid("returns_json is not a valid JSON Schema: %v", err)
		}
	}

	switch {
	case tool.Endpoint != "" && tool.MCPServerURL != "":
		return invalid("set either endpoint or mcp_server_url, not both")
	case tool.Endpoint != "":
		if err := validateHTTPURL(tool.Endpoint); err != nil {
			return invalid("endpoint: %v", err)
		}
	case tool.MCPServerURL != "":
		if err := validateHTTPURL(tool.MCPServerURL); err != nil {
			return invalid("mcp_server_url: %v", err)
		}
		if tool.MCPToolName == "" {
			// a tool of an MCP server is named by the last segment of its platform name
			tool.MCPToolName = tool.Name[strings.LastIndex(tool.Name, ".")+1:]
		}
	default:
		return invalid("an endpoint is required to execute the tool")
	}

	limits := tool.ExecutionLimits
	rateLimits := tool.RateLimits
	for _, value := range []int32{limits.TimeoutSeconds, limits.MemoryLimitMB, limits.CPULimitMillicores,
		limits.MaxResponseSizeKB, limits.MaxRetries, rateLimits.RequestsPerMinute,
		rateLimits.RequestsPerSession, rateLimits.DailyLimit, tool.Behavior.TypicalLatencyMs} {
		if value < 0 {
			return invalid("limits and latencies cannot be negative")
		}
	}
	return nil
}

func validateHTTPURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return fmt.Errorf("%q is not an http(s) URL", rawURL)
	}
	return nil
}

// CheckCompatibility checks a new version of a tool against its registered versions.
// Within a major version, a version must not break the callers of the version before
// it: it cannot remove parameters or result properties, or add required parameters.
// Such changes need a new major version
func CheckCompatibility(tool *postgres.GogiTool, registered []*postgres.GogiTool) error {
	version := semver.MustParse(tool.Version)

	var previous *postgres.GogiTool
	var previousVersion *semver.Version
	for _, candidate := range registered {
		candidateVersion, err := semver.NewVersion(candidate.Version)
		if err != nil || candidateVersion.Major() != version.Major() || !candidateVersion.LessThan(version) {
			continue
		}
		if previousVersion == nil || candidateVersion.GreaterThan(previousVersion) {
			previous, previousVersion = candidate, candidateVersion
		}
	}
	if previous == nil {
		return nil
	}

	parameterChanges, err := incompatibleChanges(previous.ParametersJSON, tool.ParametersJSON, true)
	if err != nil {
		return err
	}
	resultChanges, err := incompatibleChanges(previous.ReturnsJSON, tool.ReturnsJSON, false)
	if err != nil {
		return err
	}

	changes := make([]string, 0, len(parameterChanges)+len(resultChanges))
	for _, change := range parameterChanges {
		changes = append(changes, "parameters: "+change)
	}
	for _, change := range resultChanges {
		changes = append(changes, "results: "+change)
	}
	if len(changes) > 0 {
		return fmt.Errorf("%w: version %s breaks callers of version %s (%s); register it as version %d.0.0",
			ErrIncompatibleVersion, tool.Version, previous.Version, strings.Join(changes, "; "), version.Major()+1)
	}
	return nil
}

// SelectVersion returns the latest of the versions of a tool that satisfies the
// constraint, e.g. "^1.0.0" or ">=1.0.0 <3.0.0", or the latest version if the
// constraint is empty. It returns ErrToolNotFound if no version satisfies it
func SelectVersion(versions []*postgres.GogiTool, constraint string) (*postgres.GogiTool, error) {
	var constraints *semver.Constraints
	if constraint != "" {
		var err error
		if constraints, err = semver.NewConstraint(constraint); err != nil {
			return nil, fmt.Errorf("invalid version constraint %q: %w", constraint, err)
		}
	}

	var selected *postgres.GogiTool
	var selectedVersion *semver.Version
	for _, candidate := range versions {
		candidateVersion, err := semver.NewVersion(candidate.Version)
		if err != nil || (constraints != nil && !constraints.Check(candidateVersion)) {
			continue
		}
		if selectedVersion == nil || candidateVersion.GreaterThan(selectedVersion) {
			selected, selectedVersion = candidate, candidateVersion
		}
	}
	if selected == nil {
		return nil, utils.ErrToolNotFound
	}
	return selected, nil
}

// FindVersion returns the given version of a tool, or its latest version if version is empty
func FindVersion(versions []*postgres.GogiTool, version string) (*postgres.GogiTool, error) {
	if version == "" {
		return SelectVersion(versions, "")
	}
	for _, candidate := range versions {
		if candidate.Version == version {
			return candidate, nil
		}
	}
	return nil, utils.ErrToolNotFound
}

// NamespacePrefix returns the prefix of the names of the tools in a namespace:
// "healthcare.scheduling" and "healthcare.scheduling.*" both select the tools named
// "healthcare.scheduling.<...>"; "" and "*" select all tools
func NamespacePrefix(namespace string) string {
	namespace = strings.TrimSuffix(strings.TrimSuffix(namespace, "*"), ".")
	if namespace == "" {
		return ""
	}
	return namespace + "."
}
