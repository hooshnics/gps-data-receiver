package replay

import (
	"fmt"
	"strings"
)

// ProductionReprocessGuard refuses risky production reprocess without explicit confirmation.
// Externals enabled + ENVIRONMENT=production requires confirmProduction=true.
func ProductionReprocessGuard(environment, mode string, noExternal, confirmProduction bool) error {
	env := strings.ToLower(strings.TrimSpace(environment))
	m := strings.ToLower(strings.TrimSpace(mode))
	if env != "production" && env != "prod" {
		return nil
	}
	if m != "reprocess" {
		return nil
	}
	if noExternal {
		return nil
	}
	if confirmProduction {
		return nil
	}
	return fmt.Errorf("refusing production reprocess with externals enabled without -confirm-production")
}

// ValidateReplayEnvironment soft-checks environment naming for operators.
// Empty or development/staging/production/lab are accepted; unknown values return a warning error.
func ValidateReplayEnvironment(environment string) error {
	env := strings.ToLower(strings.TrimSpace(environment))
	if env == "" {
		return nil
	}
	switch env {
	case "development", "dev", "staging", "production", "prod", "lab", "test":
		return nil
	default:
		return fmt.Errorf("unknown ENVIRONMENT %q — expected development|staging|production|lab (set explicitly)", environment)
	}
}
