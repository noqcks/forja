package buildkit

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	dockerconfig "github.com/docker/cli/cli/config"
	dockerclient "github.com/docker/docker/client"
	bkclient "github.com/moby/buildkit/client"
	"github.com/moby/buildkit/session"
	"github.com/moby/buildkit/session/auth/authprovider"
	"github.com/moby/buildkit/session/secrets/secretsprovider"
	"github.com/moby/buildkit/util/progress/progressui"
	"github.com/tonistiigi/fsutil"
)

type Request struct {
	Addr           string
	CACertPath     string
	ClientCertPath string
	ClientKeyPath  string
	ContextDir     string
	DockerfilePath string
	Platform       string
	Tags           []string
	BuildArgs      []string
	Target         string
	Secrets        []string
	NoCache        bool
	Push           bool
	Load           bool
	PushByDigest   bool
	CacheBucket    string
	CacheRegion    string
	CacheName      string
	Progress       string
	Stdout         io.Writer
	Stderr         io.Writer
}

type Result struct {
	ExporterResponse map[string]string
}

func Run(ctx context.Context, req Request) (*Result, error) {
	client, err := bkclient.New(
		ctx,
		req.Addr,
		bkclient.WithCredentials(req.ClientCertPath, req.ClientKeyPath),
		bkclient.WithServerConfig("forja-builder", req.CACertPath),
	)
	if err != nil {
		return nil, fmt.Errorf("connect buildkit client: %w", err)
	}
	defer client.Close()

	if err := client.Wait(ctx); err != nil {
		return nil, fmt.Errorf("wait for buildkit: %w", err)
	}

	contextDir, dockerfilePath, err := NormalizeContext(req.ContextDir, req.DockerfilePath)
	if err != nil {
		return nil, err
	}
	contextFS, err := fsutil.NewFS(contextDir)
	if err != nil {
		return nil, fmt.Errorf("open build context: %w", err)
	}
	dockerfileFS, err := fsutil.NewFS(filepath.Dir(dockerfilePath))
	if err != nil {
		return nil, fmt.Errorf("open dockerfile dir: %w", err)
	}

	attachables, err := sessionAttachables(req.Secrets)
	if err != nil {
		return nil, err
	}

	frontendAttrs := map[string]string{
		"filename": filepath.Base(dockerfilePath),
	}
	if req.Target != "" {
		frontendAttrs["target"] = req.Target
	}
	if req.Platform != "" {
		frontendAttrs["platform"] = req.Platform
	}
	if req.NoCache {
		frontendAttrs["no-cache"] = ""
	}
	for _, buildArg := range req.BuildArgs {
		key, value, ok := strings.Cut(buildArg, "=")
		if !ok {
			return nil, fmt.Errorf("invalid --build-arg %q; expected KEY=VALUE", buildArg)
		}
		frontendAttrs["build-arg:"+key] = value
	}

	opt := bkclient.SolveOpt{
		Frontend:      "dockerfile.v0",
		FrontendAttrs: frontendAttrs,
		LocalMounts: map[string]fsutil.FS{
			"context":    contextFS,
			"dockerfile": dockerfileFS,
		},
		Session: attachables,
	}

	if req.CacheBucket != "" && req.CacheRegion != "" {
		cacheAttrs := map[string]string{
			"bucket": req.CacheBucket,
			"region": req.CacheRegion,
			"name":   req.CacheName,
		}
		opt.CacheImports = []bkclient.CacheOptionsEntry{{Type: "s3", Attrs: cacheAttrs}}
		opt.CacheExports = []bkclient.CacheOptionsEntry{{Type: "s3", Attrs: cacheAttrs}}
	}

	loadErrCh := make(chan error, 1)
	var loadPipeWriter *io.PipeWriter
	var exportDone chan struct{}
	var exportDoneOnce sync.Once
	if req.Load {
		pr, pw := io.Pipe()
		loadPipeWriter = pw
		exportDone = make(chan struct{})
		opt.Exports = append(opt.Exports, bkclient.ExportEntry{
			Type: "docker",
			Attrs: map[string]string{
				"name": strings.Join(req.Tags, ","),
			},
			Output: func(map[string]string) (io.WriteCloser, error) {
				return &pipeWriteCloser{
					WriteCloser: pw,
					onClose: func() {
						exportDoneOnce.Do(func() {
							close(exportDone)
						})
					},
				}, nil
			},
		})
		go func() {
			err := loadIntoDocker(ctx, pr, req.Stdout)
			<-exportDone
			loadErrCh <- err
		}()
	}
	if req.Push {
		attrs := map[string]string{
			"name": strings.Join(req.Tags, ","),
			"push": "true",
		}
		if req.PushByDigest {
			attrs["push-by-digest"] = "true"
		}
		opt.Exports = append(opt.Exports, bkclient.ExportEntry{
			Type:  bkclient.ExporterImage,
			Attrs: attrs,
		})
	}

	statusCh := make(chan *bkclient.SolveStatus)
	displayMode := progressui.DisplayMode(req.Progress)
	if displayMode == "" {
		displayMode = progressui.AutoMode
	}
	out := req.Stderr
	if out == nil {
		out = os.Stderr
	}
	display, err := progressui.NewDisplay(out, displayMode)
	if err != nil {
		return nil, fmt.Errorf("init progress display: %w", err)
	}
	var displayErr error
	var reporter sync.WaitGroup
	reporter.Add(1)
	go func() {
		defer reporter.Done()
		_, displayErr = display.UpdateFrom(ctx, statusCh)
	}()

	resp, err := client.Solve(ctx, nil, opt, statusCh)
	reporter.Wait()
	if err == nil && displayErr != nil {
		return nil, fmt.Errorf("progress display: %w", displayErr)
	}
	if err != nil {
		if req.Load {
			_ = loadPipeWriter.CloseWithError(err)
			exportDoneOnce.Do(func() {
				close(exportDone)
			})
		}
		return nil, fmt.Errorf("solve build: %w", err)
	}
	if req.Load {
		if err := <-loadErrCh; err != nil {
			return nil, err
		}
	}
	return &Result{ExporterResponse: resp.ExporterResponse}, nil
}

func sessionAttachables(secretSpecs []string) ([]session.Attachable, error) {
	dockerCfg := dockerconfig.LoadDefaultConfigFile(os.Stderr)
	attachables := []session.Attachable{
		authprovider.NewDockerAuthProvider(authprovider.DockerAuthProviderConfig{
			AuthConfigProvider: authprovider.LoadAuthConfig(dockerCfg),
		}),
	}
	if len(secretSpecs) == 0 {
		return attachables, nil
	}
	sources := make([]secretsprovider.Source, 0, len(secretSpecs))
	for _, spec := range secretSpecs {
		source, err := parseSecret(spec)
		if err != nil {
			return nil, err
		}
		sources = append(sources, *source)
	}
	store, err := secretsprovider.NewStore(sources)
	if err != nil {
		return nil, fmt.Errorf("prepare secrets: %w", err)
	}
	attachables = append(attachables, secretsprovider.NewSecretProvider(store))
	return attachables, nil
}

func loadIntoDocker(ctx context.Context, reader io.Reader, stdout io.Writer) error {
	cli, err := dockerclient.NewClientWithOpts(dockerclient.FromEnv, dockerclient.WithAPIVersionNegotiation())
	if err != nil {
		return fmt.Errorf("connect local docker daemon: %w", err)
	}
	defer cli.Close()
	resp, err := cli.ImageLoad(ctx, reader)
	if err != nil {
		return fmt.Errorf("docker image load: %w", err)
	}
	defer resp.Body.Close()
	if stdout == nil {
		stdout = io.Discard
	}
	if _, err := io.Copy(stdout, resp.Body); err != nil {
		return fmt.Errorf("stream docker load response: %w", err)
	}
	return nil
}

type pipeWriteCloser struct {
	io.WriteCloser
	onClose func()
}

func (p *pipeWriteCloser) Close() error {
	if p.onClose != nil {
		defer p.onClose()
	}
	return p.WriteCloser.Close()
}
