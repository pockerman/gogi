package guardrails

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// policyFile is a policy as written in a YAML file (listing 6.26)
type policyFile struct {
	Name          string `yaml:"name"`
	Type          string `yaml:"type"`
	Application   string `yaml:"application"`
	ApplicationID string `yaml:"application_id"`
	Mode          string `yaml:"mode"`
	Description   string `yaml:"description"`
	Version       string `yaml:"version"`
	Rules         []any  `yaml:"rules"`
}

// LoadPolicyDir reads the policies of the .yaml and .yml files in dir. A file may hold
// several policies, as YAML documents separated by ---. Every policy is compiled, so an
// invalid one fails the load rather than being skipped. It returns the warnings about rules
// that are accepted but not enforced
func LoadPolicyDir(dir string) ([]Policy, []string, error) {

	paths := make([]string, 0)
	for _, pattern := range []string{"*.yaml", "*.yml"} {
		matches, err := filepath.Glob(filepath.Join(dir, pattern))
		if err != nil {
			return nil, nil, err
		}
		paths = append(paths, matches...)
	}
	sort.Strings(paths)

	policies := make([]Policy, 0)
	warnings := make([]string, 0)
	names := map[string]string{}
	for _, path := range paths {
		loaded, err := loadPolicyFile(path)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", path, err)
		}
		for _, policy := range loaded {
			if other, ok := names[policy.Name]; ok {
				return nil, nil, fmt.Errorf("%s: policy %q is also defined in %s", path, policy.Name, other)
			}
			names[policy.Name] = path

			_, policyWarnings, err := Compile(policy)
			if err != nil {
				return nil, nil, fmt.Errorf("%s: %w", path, err)
			}
			for _, warning := range policyWarnings {
				warnings = append(warnings, fmt.Sprintf("%s: policy %s: %s", path, policy.Name, warning))
			}
			policies = append(policies, policy)
		}
	}
	return policies, warnings, nil
}

func loadPolicyFile(path string) ([]Policy, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	policies := make([]Policy, 0)
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	for {
		var file policyFile
		err := decoder.Decode(&file)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if file.Name == "" && file.Type == "" && file.Rules == nil {
			continue // an empty document
		}

		rules, err := json.Marshal(file.Rules)
		if err != nil {
			return nil, fmt.Errorf("policy %q: rules: %w", file.Name, err)
		}
		application := file.ApplicationID
		if application == "" {
			application = file.Application
		}
		policies = append(policies, Policy{
			Name:          file.Name,
			Type:          file.Type,
			ApplicationID: application,
			Mode:          strings.TrimSpace(file.Mode),
			RulesJSON:     string(rules),
			Description:   file.Description,
			Version:       file.Version,
			Source:        SourceFile,
		})
	}
	return policies, nil
}
