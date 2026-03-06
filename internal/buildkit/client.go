package buildkit

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	dockerconfig "github.com/docker/cli/cli/config"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
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
	WaitDuration     time.Duration
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

	waitStart := time.Now()
	if err := client.Wait(ctx); err != nil {
		return nil, fmt.Errorf("wait for buildkit: %w", err)
	}
	waitDuration := time.Since(waitStart)

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

	var loadTarPath string
	var loadTarFile *os.File
	if req.Load {
		loadTarFile, err = os.CreateTemp("", "forja-load-*.tar")
		if err != nil {
			return nil, fmt.Errorf("create temp image tar: %w", err)
		}
		loadTarPath = loadTarFile.Name()
		opt.Exports = append(opt.Exports, bkclient.ExportEntry{
			Type: bkclient.ExporterOCI,
			Attrs: map[string]string{
				"name": strings.Join(req.Tags, ","),
			},
			Output: func(map[string]string) (io.WriteCloser, error) {
				return loadTarFile, nil
			},
		})
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
			_ = loadTarFile.Close()
			_ = os.Remove(loadTarPath)
		}
		return nil, fmt.Errorf("solve build: %w", err)
	}
	if req.Load {
		if err := loadTarFile.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
			_ = os.Remove(loadTarPath)
			return nil, fmt.Errorf("close temp image tar: %w", err)
		}
		defer os.Remove(loadTarPath)
		if err := loadIntoDocker(ctx, loadTarPath, req.Tags, req.Stdout); err != nil {
			return nil, err
		}
	}
	return &Result{ExporterResponse: resp.ExporterResponse, WaitDuration: waitDuration}, nil
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

func loadIntoDocker(ctx context.Context, tarPath string, tags []string, stdout io.Writer) error {
	if stdout == nil {
		stdout = io.Discard
	}

	dockerTarPath, cleanup, err := convertOCIArchiveToDockerArchive(tarPath, tags)
	if err != nil {
		return err
	}
	defer cleanup()

	cmd := exec.CommandContext(ctx, "docker", "load", "-i", dockerTarPath)
	cmd.Stdout = stdout
	cmd.Stderr = stdout
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker image load: %w", err)
	}
	return nil
}

func convertOCIArchiveToDockerArchive(ociTarPath string, tags []string) (string, func(), error) {
	tempDir, err := os.MkdirTemp("", "forja-oci-*")
	if err != nil {
		return "", nil, fmt.Errorf("create temp oci dir: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(tempDir) }

	if err := extractTar(ociTarPath, tempDir); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("extract oci archive: %w", err)
	}

	lp, err := layout.FromPath(tempDir)
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("open oci layout: %w", err)
	}
	index, err := lp.ImageIndex()
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("read oci image index: %w", err)
	}
	manifest, err := index.IndexManifest()
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("read oci index manifest: %w", err)
	}
	if len(manifest.Manifests) == 0 {
		cleanup()
		return "", nil, fmt.Errorf("oci archive does not contain any manifests")
	}

	img, err := lp.Image(manifest.Manifests[0].Digest)
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("read oci image: %w", err)
	}

	refToImage := make(map[name.Reference]v1.Image, len(tags))
	for _, tag := range tags {
		ref, err := name.ParseReference(tag, name.WeakValidation)
		if err != nil {
			cleanup()
			return "", nil, fmt.Errorf("parse image tag %q: %w", tag, err)
		}
		refToImage[ref] = img
	}

	dockerTarPath := filepath.Join(tempDir, "docker-image.tar")
	if err := tarball.MultiRefWriteToFile(dockerTarPath, refToImage); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("write docker archive: %w", err)
	}

	return dockerTarPath, cleanup, nil
}

func extractTar(src string, dest string) error {
	file, err := os.Open(src)
	if err != nil {
		return err
	}
	defer file.Close()

	reader := tar.NewReader(file)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}

		targetPath := filepath.Join(dest, header.Name)
		cleanDest := filepath.Clean(dest) + string(os.PathSeparator)
		cleanTarget := filepath.Clean(targetPath)
		if cleanTarget != filepath.Clean(dest) && !strings.HasPrefix(cleanTarget, cleanDest) {
			return fmt.Errorf("tar entry escapes extraction dir: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(cleanTarget, os.FileMode(header.Mode)); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(cleanTarget), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(cleanTarget, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(header.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, reader); err != nil {
				out.Close()
				return err
			}
			if err := out.Close(); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported tar entry type %d for %s", header.Typeflag, header.Name)
		}
	}
}
