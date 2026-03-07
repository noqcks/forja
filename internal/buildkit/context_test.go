package buildkit

import (
	"errors"
	"os"
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

func TestNewContextFSRespectsDockerignore(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM scratch\n"), 0o644); err != nil {
		t.Fatalf("write Dockerfile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".dockerignore"), []byte("dist\n"), 0o644); err != nil {
		t.Fatalf("write .dockerignore: %v", err)
	}
	if err := os.Mkdir(filepath.Join(dir, "dist"), 0o755); err != nil {
		t.Fatalf("mkdir dist: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dist", "artifact.txt"), []byte("ignored\n"), 0o644); err != nil {
		t.Fatalf("write ignored file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app.txt"), []byte("included\n"), 0o644); err != nil {
		t.Fatalf("write included file: %v", err)
	}

	contextFS, err := NewContextFS(dir, filepath.Join(dir, "Dockerfile"))
	if err != nil {
		t.Fatalf("NewContextFS() error = %v", err)
	}

	reader, err := contextFS.Open("app.txt")
	if err != nil {
		t.Fatalf("Open(app.txt) error = %v", err)
	}
	_ = reader.Close()

	if _, err := contextFS.Open("dist/artifact.txt"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Open(dist/artifact.txt) error = %v, want not exist", err)
	}
}

func TestNewContextFSKeepsDockerfileWhenIgnored(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM scratch\n"), 0o644); err != nil {
		t.Fatalf("write Dockerfile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".dockerignore"), []byte("Dockerfile\n.dockerignore\n"), 0o644); err != nil {
		t.Fatalf("write .dockerignore: %v", err)
	}

	contextFS, err := NewContextFS(dir, filepath.Join(dir, "Dockerfile"))
	if err != nil {
		t.Fatalf("NewContextFS() error = %v", err)
	}

	dockerfileReader, err := contextFS.Open("Dockerfile")
	if err != nil {
		t.Fatalf("Open(Dockerfile) error = %v", err)
	}
	_ = dockerfileReader.Close()

	dockerignoreReader, err := contextFS.Open(".dockerignore")
	if err != nil {
		t.Fatalf("Open(.dockerignore) error = %v", err)
	}
	_ = dockerignoreReader.Close()
}

func TestCacheNameNormalizesBaseName(t *testing.T) {
	t.Parallel()

	if got := CacheName("/tmp/My App"); got != "my-app" {
		t.Fatalf("CacheName() = %q, want %q", got, "my-app")
	}
}
