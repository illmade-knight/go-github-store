package filter

import (
	"fmt"

	"github.com/bmatcuk/doublestar/v4"
	"gopkg.in/yaml.v3"
)

// FilterRules represents the parsed YAML structure for inclusion/exclusion logic.
type FilterRules struct {
	Include []string `yaml:"include" json:"include" firestore:"include"`
	Exclude []string `yaml:"exclude" json:"exclude" firestore:"exclude"`
}

// ParseYAML parses and validates a YAML string into FilterRules.
func ParseYAML(yamlStr string) (FilterRules, error) {
	var rules FilterRules
	if err := yaml.Unmarshal([]byte(yamlStr), &rules); err != nil {
		return rules, fmt.Errorf("invalid YAML structure: %w", err)
	}
	return rules, nil
}

// Match determines if a given file path passes the inclusion/exclusion rules.
func (r *FilterRules) Match(path string) bool {
	// 1. Check Excludes first (Exclude overrides Include)
	for _, pattern := range r.Exclude {
		if match, _ := doublestar.Match(pattern, path); match {
			return false // Immediately drop
		}
	}

	// 2. Default inclusion if no include rules are specified
	if len(r.Include) == 0 {
		return true
	}

	// 3. Check Includes (Must match at least one)
	for _, pattern := range r.Include {
		if match, _ := doublestar.Match(pattern, path); match {
			return true
		}
	}

	return false
}
