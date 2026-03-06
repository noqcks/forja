package buildkit

import (
	"fmt"
	"path/filepath"
	"strings"
)

func NormalizeContext(contextDir string, dockerfile string) (string, string, error) {
	if contextDir == "" {
		contextDir = "."
	}
	absContext, err := filepath.Abs(contextDir)
	if err != nil {
		return "", "", fmt.Errorf("resolve context dir: %w", err)
	}
	if dockerfile == "" {
		dockerfile = filepath.Join(absContext, "Dockerfile")
	}
	absDockerfile, err := filepath.Abs(dockerfile)
	if err != nil {
		return "", "", fmt.Errorf("resolve dockerfile: %w", err)
	}
	return absContext, absDockerfile, nil
}

func CacheName(contextDir string) string {
	base := filepath.Base(contextDir)
	base = strings.TrimSpace(base)
	if base == "" || base == "." || base == string(filepath.Separator) {
		return "forja"
	}
	base = strings.ToLower(base)
	base = strings.ReplaceAll(base, " ", "-")
	return base
}
