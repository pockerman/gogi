package tools

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// DefaultParametersJSON is the parameters schema of a tool that takes no arguments
const DefaultParametersJSON = `{"type":"object","properties":{}}`

const schemaURL = "gogi://tool/schema.json"

// CompileSchema compiles a JSON Schema. References are resolved only within the
// schema itself: a registration must not make the platform read files or URLs
func CompileSchema(schemaJSON string) (*jsonschema.Schema, error) {
	document, err := jsonschema.UnmarshalJSON(strings.NewReader(schemaJSON))
	if err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}

	compiler := jsonschema.NewCompiler()
	compiler.UseLoader(jsonschema.SchemeURLLoader{})
	if err := compiler.AddResource(schemaURL, document); err != nil {
		return nil, err
	}
	return compiler.Compile(schemaURL)
}

// ValidateArguments checks arguments, a JSON document, against the compiled parameters
// schema of a tool. It returns the problems found, or none if the arguments are valid
func ValidateArguments(schema *jsonschema.Schema, argumentsJSON string) []string {
	if strings.TrimSpace(argumentsJSON) == "" {
		argumentsJSON = "{}"
	}

	arguments, err := jsonschema.UnmarshalJSON(strings.NewReader(argumentsJSON))
	if err != nil {
		return []string{fmt.Sprintf("arguments are not valid JSON: %v", err)}
	}

	err = schema.Validate(arguments)
	if err == nil {
		return nil
	}

	var validationErr *jsonschema.ValidationError
	if !errors.As(err, &validationErr) {
		return []string{err.Error()}
	}

	problems := make([]string, 0)
	for _, unit := range validationErr.BasicOutput().Errors {
		if unit.Error == nil {
			continue
		}
		location := unit.InstanceLocation
		if location == "" {
			location = "/"
		}
		problems = append(problems, fmt.Sprintf("%s: %s", location, unit.Error.String()))
	}
	if len(problems) == 0 {
		problems = append(problems, validationErr.Error())
	}
	return problems
}

// objectSchema is the part of an object schema that compatibility checks need
type objectSchema struct {
	Properties map[string]json.RawMessage `json:"properties"`
	Required   []string                   `json:"required"`
}

func parseObjectSchema(schemaJSON string) (objectSchema, error) {
	var schema objectSchema
	if strings.TrimSpace(schemaJSON) == "" {
		return schema, nil
	}
	err := json.Unmarshal([]byte(schemaJSON), &schema)
	return schema, err
}

// incompatibleChanges returns the changes from the previous schema of the arguments
// (or results) of a tool to the next one that break callers of the previous version:
// removed properties and, for arguments, new required properties
func incompatibleChanges(previousJSON, nextJSON string, checkRequired bool) ([]string, error) {
	previous, err := parseObjectSchema(previousJSON)
	if err != nil {
		return nil, err
	}
	next, err := parseObjectSchema(nextJSON)
	if err != nil {
		return nil, err
	}

	changes := make([]string, 0)
	for property := range previous.Properties {
		if _, ok := next.Properties[property]; !ok {
			changes = append(changes, fmt.Sprintf("removes property %q", property))
		}
	}

	if checkRequired {
		wasRequired := make(map[string]bool, len(previous.Required))
		for _, property := range previous.Required {
			wasRequired[property] = true
		}
		for _, property := range next.Required {
			if !wasRequired[property] {
				changes = append(changes, fmt.Sprintf("makes property %q required", property))
			}
		}
	}

	sort.Strings(changes)
	return changes, nil
}
