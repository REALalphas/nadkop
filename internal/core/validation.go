package core

import (
	"fmt"
	"strings"
)

// RuleValidationError identifies the editor rule and field without parsing text.
// RuleIndex is zero based for API clients; Error uses the visible rule number.
type RuleValidationError struct {
	RuleIndex int
	Field     string
	Message   string
}

func (e *RuleValidationError) Error() string {
	return fmt.Sprintf("Rule %d: %s", e.RuleIndex+1, e.Message)
}

func ruleError(index int, field, format string, args ...any) error {
	return &RuleValidationError{RuleIndex: index, Field: field, Message: fmt.Sprintf(format, args...)}
}

// ValidateRuleSets checks saved draft selectors against actual installed data.
// It does not fetch subscriptions or restart the running service.
func ValidateRuleSets(c Config, path string) error {
	var db *GeoDatabase
	// Retain only the compatibility result, never expanded strings or slices.
	validated := map[string]bool{}
	for i, rule := range c.Rules {
		rule = rule.Active()
		if len(rule.Sets) == 0 {
			continue
		}
		if db == nil {
			var err error
			db, err = OpenGeoDatabase(path)
			if err != nil {
				return ruleError(i, "sets", "Dataset database is not loaded. Update databases in settings.")
			}
		}
		for _, set := range rule.Sets {
			compatible, known := validated[set]
			if !known {
				domains, _, err := db.Expand(set)
				if err != nil {
					return ruleError(i, "sets", "Set \"%s\" is not available in the installed databases. Update databases or remove the set.", set)
				}
				compatible = true
				for _, domain := range domains {
					if !strings.HasPrefix(domain, "domain:") && !strings.HasPrefix(domain, "full:") {
						compatible = false
						break
					}
				}
				validated[set] = compatible
			}
			if rule.Action == "zapret" && !compatible {
				return ruleError(i, "sets", "Set \"%s\" is incompatible with Zapret. Select another action or remove the set.", set)
			}
		}
	}
	return nil
}
