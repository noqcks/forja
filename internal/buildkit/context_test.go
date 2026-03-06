package buildkit

import (
	"path/filepath"
	"testing"
)

func TestNormalizeContextDefaultsDockerfileWithinContext(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	contextDir, dockerfilePath, err := NormalizeContext(dir, "")
	if err != nil {
		t.Fatalf("NormalizeContext() error = %v", err)
	}
	if contextDir != dir {
		t.Fatalf("contextDir = %s, want %s", contextDir, dir)
	}
	if dockerfilePath != filepath.Join(dir, "Dockerfile") {
		t.Fatalf("dockerfilePath = %s", dockerfilePath)
	}
}

func TestCacheNameNormalizesBaseName(t *testing.T) {
	t.Parallel()

	if got := CacheName("/tmp/My App"); got != "my-app" {
		t.Fatalf("CacheName() = %q, want %q", got, "my-app")
	}
}
