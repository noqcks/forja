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
	log "github.com/sirupsen/logrus"
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
	if req.Load {
		pr, pw := io.Pipe()
		opt.Exports = append(opt.Exports, bkclient.ExportEntry{
			Type: "docker",
			Attrs: map[string]string{
				"name": strings.Join(req.Tags, ","),
			},
			Output: func(map[string]string) (io.WriteCloser, error) {
				return pw, nil
			},
		})
		go func() {
			defer pr.Close()
			loadErrCh <- loadIntoDocker(ctx, pr, req.Stdout)
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
	var reporter sync.WaitGroup
	reporter.Add(1)
	go func() {
		defer reporter.Done()
		reportSolveStatus(req.Stderr, statusCh)
	}()

	resp, err := client.Solve(ctx, nil, opt, statusCh)
	reporter.Wait()
	if err != nil {
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

func reportSolveStatus(w io.Writer, ch <-chan *bkclient.SolveStatus) {
	if w == nil {
		w = io.Discard
	}
	seenCompleted := map[string]bool{}
	for status := range ch {
		for _, vertex := range status.Vertexes {
			if vertex == nil || vertex.Name == "" {
				continue
			}
			if vertex.Completed != nil && !seenCompleted[vertex.Name] {
				seenCompleted[vertex.Name] = true
				log.Infof("=> %s", vertex.Name)
			}
			if vertex.Error != "" {
				log.Errorf("error: %s", vertex.Error)
			}
		}
		for _, entry := range status.Logs {
			if len(entry.Data) > 0 {
				_, _ = w.Write(entry.Data)
			}
		}
	}
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
