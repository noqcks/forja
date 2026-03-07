package buildkit

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	bkclient "github.com/moby/buildkit/client"
)

type fakeBuildkitClient struct {
	waitFn  func(context.Context) error
	solveFn func(context.Context, bkclient.SolveOpt, chan *bkclient.SolveStatus) (*bkclient.SolveResponse, error)
	closeFn func() error
}

func (f *fakeBuildkitClient) Wait(ctx context.Context) error {
	if f.waitFn != nil {
		return f.waitFn(ctx)
	}
	return nil
}

func (f *fakeBuildkitClient) Solve(ctx context.Context, opt bkclient.SolveOpt, statusCh chan *bkclient.SolveStatus) (*bkclient.SolveResponse, error) {
	if f.solveFn != nil {
		return f.solveFn(ctx, opt, statusCh)
	}
	close(statusCh)
	return &bkclient.SolveResponse{}, nil
}

func (f *fakeBuildkitClient) Close() error {
	if f.closeFn != nil {
		return f.closeFn()
	}
	return nil
}

func TestRunReturnsOnCancelEvenIfSolveHangs(t *testing.T) {
	contextDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(contextDir, "Dockerfile"), []byte("FROM scratch\n"), 0o644); err != nil {
		t.Fatalf("write Dockerfile: %v", err)
	}

	originalClientFactory := newBuildkitClient
	originalGracePeriod := buildkitCancelGracePeriod
	t.Cleanup(func() {
		newBuildkitClient = originalClientFactory
		buildkitCancelGracePeriod = originalGracePeriod
	})

	buildkitCancelGracePeriod = 50 * time.Millisecond

	solveStarted := make(chan struct{})
	closeCalled := make(chan struct{})
	var closeOnce sync.Once

	newBuildkitClient = func(ctx context.Context, req Request) (buildkitClient, error) {
		return &fakeBuildkitClient{
			solveFn: func(ctx context.Context, opt bkclient.SolveOpt, statusCh chan *bkclient.SolveStatus) (*bkclient.SolveResponse, error) {
				statusCh <- &bkclient.SolveStatus{}
				close(solveStarted)
				<-ctx.Done()
				select {}
			},
			closeFn: func() error {
				closeOnce.Do(func() { close(closeCalled) })
				return nil
			},
		}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, err := Run(ctx, Request{
			Addr:       "tcp://buildkit.invalid:8372",
			ContextDir: contextDir,
			Progress:   "plain",
			Stderr:     &bytes.Buffer{},
		})
		done <- err
	}()

	<-solveStarted
	cancel()

	select {
	case <-closeCalled:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected client.Close to be called after cancellation")
	}

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run() error = %v, want context canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run() did not return after cancellation")
	}
}

func TestRunSetsS3CacheExportModeMax(t *testing.T) {
	contextDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(contextDir, "Dockerfile"), []byte("FROM scratch\n"), 0o644); err != nil {
		t.Fatalf("write Dockerfile: %v", err)
	}

	originalClientFactory := newBuildkitClient
	t.Cleanup(func() {
		newBuildkitClient = originalClientFactory
	})

	var gotOpt bkclient.SolveOpt
	newBuildkitClient = func(ctx context.Context, req Request) (buildkitClient, error) {
		return &fakeBuildkitClient{
			solveFn: func(ctx context.Context, opt bkclient.SolveOpt, statusCh chan *bkclient.SolveStatus) (*bkclient.SolveResponse, error) {
				gotOpt = opt
				close(statusCh)
				return &bkclient.SolveResponse{}, nil
			},
		}, nil
	}

	_, err := Run(context.Background(), Request{
		Addr:        "tcp://buildkit.invalid:8372",
		ContextDir:  contextDir,
		CacheBucket: "forja-cache-test",
		CacheRegion: "us-east-1",
		CacheName:   "repo",
		Progress:    "plain",
		Stderr:      &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(gotOpt.CacheImports) != 1 {
		t.Fatalf("len(CacheImports) = %d, want 1", len(gotOpt.CacheImports))
	}
	if gotOpt.CacheImports[0].Attrs["mode"] != "" {
		t.Fatalf("CacheImports mode = %q, want empty", gotOpt.CacheImports[0].Attrs["mode"])
	}

	if len(gotOpt.CacheExports) != 1 {
		t.Fatalf("len(CacheExports) = %d, want 1", len(gotOpt.CacheExports))
	}
	if gotOpt.CacheExports[0].Type != "s3" {
		t.Fatalf("CacheExports type = %q, want s3", gotOpt.CacheExports[0].Type)
	}
	if gotOpt.CacheExports[0].Attrs["mode"] != "max" {
		t.Fatalf("CacheExports mode = %q, want max", gotOpt.CacheExports[0].Attrs["mode"])
	}
}
