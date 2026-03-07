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
