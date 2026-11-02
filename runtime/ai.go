package runtime

import "github.com/beepgrid/beepgrid/internal/aimodel"

// AIConfig selects an optional assistance provider. Neither provider nor model
// has a default. Credentials stay in the server process.
type AIConfig = aimodel.Config

// AIConfigFromEnv reads the same explicit configuration used by the standalone
// binary. Libraries do not read process environment or choose models implicitly.
func AIConfigFromEnv(getenv func(string) string) (AIConfig, error) {
	return aimodel.FromEnv(getenv)
}
