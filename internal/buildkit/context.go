package buildkit

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/moby/patternmatcher"
	"github.com/moby/patternmatcher/ignorefile"
	"github.com/tonistiigi/fsutil"
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

func NewContextFS(contextDir string, dockerfilePath string) (fsutil.FS, error) {
	contextFS, err := fsutil.NewFS(contextDir)
	if err != nil {
		return nil, fmt.Errorf("open build context: %w", err)
	}

	excludes, err := readDockerignore(contextDir)
	if err != nil {
		return nil, fmt.Errorf("read .dockerignore: %w", err)
	}
	if len(excludes) == 0 {
		return contextFS, nil
	}

	if relDockerfile, ok := dockerfilePathWithinContext(contextDir, dockerfilePath); ok {
		excludes = trimBuildFilesFromExcludes(excludes, relDockerfile)
	}

	filteredFS, err := fsutil.NewFilterFS(contextFS, &fsutil.FilterOpt{
		ExcludePatterns: excludes,
	})
	if err != nil {
		return nil, fmt.Errorf("apply .dockerignore: %w", err)
	}
	return filteredFS, nil
}

func dockerfilePathWithinContext(contextDir string, dockerfilePath string) (string, bool) {
	relPath, err := filepath.Rel(contextDir, dockerfilePath)
	if err != nil {
		return "", false
	}
	if relPath == ".." || strings.HasPrefix(relPath, ".."+string(filepath.Separator)) {
		return "", false
	}
	return relPath, true
}

func readDockerignore(contextDir string) ([]string, error) {
	f, err := os.Open(filepath.Join(contextDir, ".dockerignore"))
	switch {
	case os.IsNotExist(err):
		return nil, nil
	case err != nil:
		return nil, err
	}
	defer f.Close()

	patterns, err := ignorefile.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("error reading .dockerignore: %w", err)
	}
	return patterns, nil
}

func trimBuildFilesFromExcludes(excludes []string, dockerfile string) []string {
	if keep, _ := patternmatcher.Matches(".dockerignore", excludes); keep {
		excludes = append(excludes, "!.dockerignore")
	}

	dockerfile = filepath.ToSlash(dockerfile)
	if keep, _ := patternmatcher.Matches(dockerfile, excludes); keep {
		excludes = append(excludes, "!"+dockerfile)
	}
	return excludes
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
