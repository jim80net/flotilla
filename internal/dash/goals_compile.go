package dash

import "github.com/jim80net/flotilla/internal/goals"

func maybeCompileGoalsFromYAML(yamlPath, jsonPath string) error {
	return goals.MaybeCompileYAMLToJSON(yamlPath, jsonPath)
}
