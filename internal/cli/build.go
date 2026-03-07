package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/moby/buildkit/exporter/containerimage/exptypes"
	"github.com/noqcks/forja/internal/buildkit"
	"github.com/noqcks/forja/internal/certs"
	"github.com/noqcks/forja/internal/cloud"
	"github.com/noqcks/forja/internal/config"
	"github.com/noqcks/forja/internal/cost"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

type buildOptions struct {
	dockerfile   string
	tags         []string
	platforms    string
	push         bool
	load         bool
	buildArgs    []string
	target       string
	secrets      []string
	noCache      bool
	progress     string
	instanceType string
}

func newBuildCmd(root *rootOptions) *cobra.Command {
	opts := &buildOptions{}
	cmd := &cobra.Command{
		Use:   "build [context]",
		Short: "Build a Docker image remotely",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			contextDir := "."
			if len(args) == 1 {
				contextDir = args[0]
			}
			return runBuild(cmd.Context(), cmd, root, opts, contextDir)
		},
	}
	cmd.Flags().StringVarP(&opts.dockerfile, "file", "f", "", "Path to Dockerfile")
	cmd.Flags().StringSliceVarP(&opts.tags, "tag", "t", nil, "Image tag(s)")
	cmd.Flags().StringVar(&opts.platforms, "platform", "", "Target platform(s)")
	cmd.Flags().BoolVar(&opts.push, "push", false, "Push image after build")
	cmd.Flags().BoolVar(&opts.load, "load", false, "Load image into local Docker daemon")
	cmd.Flags().StringSliceVar(&opts.buildArgs, "build-arg", nil, "Build-time variables")
	cmd.Flags().StringVar(&opts.target, "target", "", "Build target stage")
	cmd.Flags().StringSliceVar(&opts.secrets, "secret", nil, "Build secrets")
	cmd.Flags().BoolVar(&opts.noCache, "no-cache", false, "Do not use cache")
	cmd.Flags().StringVar(&opts.progress, "progress", "auto", "Progress output type")
	cmd.Flags().StringVar(&opts.instanceType, "instance-type", "", "Override instance type for this build")
	return cmd
}

func runBuild(ctx context.Context, cmd *cobra.Command, root *rootOptions, opts *buildOptions, contextDir string) error {
	cfg, err := loadCommandConfig(true)
	if err != nil {
		return err
	}
	if opts.push {
		opts.tags = qualifyTags(opts.tags, cfg.Registry)
	}
	if (opts.push || opts.load) && len(opts.tags) == 0 {
		return fmt.Errorf("at least one --tag is required with --push or --load")
	}
	platforms := platformList(opts.platforms, cfg.DefaultPlatform)
	repo, subnetID, err := validateBuildOptions(cfg, opts, platforms)
	if err != nil {
		return err
	}

	provider, err := providerFromConfig(ctx, cfg, root.profile)
	if err != nil {
		return err
	}
	if _, err := provider.Identity(ctx); err != nil {
		return formatAWSIdentityError(err, root.profile)
	}

	buildID := "bld_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	buildHash := cloud.BuildSessionHash(buildID)
	log.Infof("Build session: %s", buildID)
	log.Infof("EC2 instance name prefix: %s", cloud.BuilderInstanceName(buildID, ""))
	bundle, err := certs.Generate()
	if err != nil {
		return err
	}
	mat, err := certs.Materialize(bundle)
	if err != nil {
		return err
	}
	defer mat.Cleanup()

	certS3Path, err := provider.UploadCertificates(ctx, cloud.UploadCertificatesRequest{
		Bucket:     cfg.CacheBucket,
		BuildID:    buildID,
		CACert:     bundle.CACertPEM,
		ServerCert: bundle.ServerCertPEM,
		ServerKey:  bundle.ServerKeyPEM,
	})
	if err != nil {
		return formatS3BucketAccessError(err, cfg.CacheBucket)
	}
	defer provider.DeleteCertificates(context.Background(), cfg.CacheBucket, buildID)

	type launched struct {
		platform string
		arch     string
		instance *cloud.BuilderInstance
		price    float64
	}
	launchedBuilders := make([]launched, len(platforms))
	launchStart := time.Now()
	group, gctx := errgroup.WithContext(ctx)
	for i, platform := range platforms {
		i := i
		platform := platform
		group.Go(func() error {
			arch, err := platformArch(platform)
			if err != nil {
				return err
			}
			instance, err := provider.LaunchBuilder(gctx, cloud.LaunchBuilderRequest{
				Region:               cfg.Region,
				Architecture:         arch,
				LaunchTemplateID:     cfg.Resources.LaunchTemplates[arch],
				SubnetID:             subnetID,
				InstanceTypeOverride: opts.instanceType,
				BuildID:              buildID,
				CertS3Path:           certS3Path,
				UserData:             renderUserData(cfg.CacheBucket, cfg.Region, cfg.SelfDestructMinutes),
			})
			if err != nil {
				return err
			}
			price, priceErr := provider.InstancePrice(gctx, cfg.Region, instance.InstanceType)
			if priceErr != nil {
				price = 0
			}
			launchedBuilders[i] = launched{platform: platform, arch: arch, instance: instance, price: price}
			log.Infof("Launching builder %s (%s, %s, build %s)... ready in %.1fs", instance.Name, instance.InstanceType, cfg.Region, buildHash, time.Since(launchStart).Seconds())
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		instanceIDs := []string{}
		for _, builder := range launchedBuilders {
			if builder.instance != nil {
				instanceIDs = append(instanceIDs, builder.instance.ID)
			}
		}
		_ = provider.TerminateInstances(context.Background(), cfg.Region, instanceIDs)
		return err
	}

	defer func() {
		instanceIDs := []string{}
		for _, builder := range launchedBuilders {
			if builder.instance != nil {
				instanceIDs = append(instanceIDs, builder.instance.ID)
			}
		}
		_ = provider.TerminateInstances(context.Background(), cfg.Region, instanceIDs)
	}()

	start := time.Now()
	cacheName := cacheNameForContext(contextDir)
	if len(platforms) == 1 {
		builder := launchedBuilders[0]
		addr := fmt.Sprintf("tcp://%s:8372", builder.instance.PublicIP)
		result, err := buildkit.Run(ctx, buildkit.Request{
			Addr:           addr,
			CACertPath:     mat.CACertPath,
			ClientCertPath: mat.ClientCertPath,
			ClientKeyPath:  mat.ClientKeyPath,
			ContextDir:     contextDir,
			DockerfilePath: opts.dockerfile,
			Platform:       platforms[0],
			Tags:           opts.tags,
			BuildArgs:      opts.buildArgs,
			Target:         opts.target,
			Secrets:        opts.secrets,
			NoCache:        opts.noCache,
			Push:           opts.push,
			Load:           opts.load,
			CacheBucket:    cfg.CacheBucket,
			CacheRegion:    cfg.Region,
			CacheName:      cacheName,
			Progress:       opts.progress,
			Stdout:         cmd.OutOrStdout(),
			Stderr:         cmd.ErrOrStderr(),
		})
		if err != nil {
			return err
		}
		duration := time.Since(start)
		launchDuration := start.Sub(launchStart)
		estimated := cost.Estimate(duration.Seconds(), builder.price)
		log.Info("Build complete.")
		log.Infof("  Launch:    %.1fs", launchDuration.Seconds())
		log.Infof("  Ready:     %.1fs", result.WaitDuration.Seconds())
		log.Infof("  Duration:  %.1fs", duration.Seconds())
		log.Infof("  Instance:  %s (%s)", builder.instance.InstanceType, cfg.Region)
		log.Infof("  Cost:      $%.4f", estimated)
		if len(opts.tags) > 0 {
			log.Infof("  Image:     %s", opts.tags[0])
		}
		if digest := result.ExporterResponse[exptypes.ExporterImageDigestKey]; digest != "" {
			log.Infof("  Digest:    %s", digest)
		}
		return nil
	}

	if opts.push && len(opts.tags) == 0 {
		return fmt.Errorf("multi-arch push requires at least one tag")
	}

	manifestSources := make([]buildkit.ManifestSource, len(platforms))
	group, gctx = errgroup.WithContext(ctx)
	for i, builder := range launchedBuilders {
		i := i
		builder := builder
		group.Go(func() error {
			addr := fmt.Sprintf("tcp://%s:8372", builder.instance.PublicIP)
			tags := opts.tags
			pushByDigest := false
			if opts.push {
				tags = []string{repo}
				pushByDigest = true
			}
			result, err := buildkit.Run(gctx, buildkit.Request{
				Addr:           addr,
				CACertPath:     mat.CACertPath,
				ClientCertPath: mat.ClientCertPath,
				ClientKeyPath:  mat.ClientKeyPath,
				ContextDir:     contextDir,
				DockerfilePath: opts.dockerfile,
				Platform:       builder.platform,
				Tags:           tags,
				BuildArgs:      opts.buildArgs,
				Target:         opts.target,
				Secrets:        opts.secrets,
				NoCache:        opts.noCache,
				Push:           opts.push,
				Load:           false,
				PushByDigest:   pushByDigest,
				CacheBucket:    cfg.CacheBucket,
				CacheRegion:    cfg.Region,
				CacheName:      cacheName,
				Progress:       opts.progress,
				Stdout:         cmd.OutOrStdout(),
				Stderr:         cmd.ErrOrStderr(),
			})
			if err != nil {
				return err
			}
			digest := result.ExporterResponse[exptypes.ExporterImageDigestKey]
			if opts.push && digest == "" {
				return fmt.Errorf("build for %s did not return a pushed digest", builder.platform)
			}
			platformDesc, err := platformDescriptor(builder.platform)
			if err != nil {
				return err
			}
			manifestSources[i] = buildkit.ManifestSource{
				Platform: platformDesc,
				Digest:   digest,
				Repo:     repo,
			}
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return err
	}
	if opts.push {
		if err := buildkit.WriteManifestList(ctx, opts.tags, manifestSources); err != nil {
			return err
		}
	}

	duration := time.Since(start)
	launchDuration := start.Sub(launchStart)
	var totalCost float64
	for _, builder := range launchedBuilders {
		totalCost += cost.Estimate(duration.Seconds(), builder.price)
	}
	log.Info("Build complete.")
	log.Infof("  Launch:    %.1fs", launchDuration.Seconds())
	log.Infof("  Duration:  %.1fs", duration.Seconds())
	log.Infof("  Platforms: %s", strings.Join(platforms, ","))
	log.Infof("  Cost:      $%.4f", totalCost)
	if len(opts.tags) > 0 {
		log.Infof("  Image:     %s", opts.tags[0])
	}
	return nil
}

func validateBuildOptions(cfg *config.Config, opts *buildOptions, platforms []string) (string, string, error) {
	if len(platforms) == 0 {
		return "", "", fmt.Errorf("at least one target platform is required")
	}
	for _, platform := range platforms {
		arch, err := platformArch(platform)
		if err != nil {
			return "", "", err
		}
		if strings.TrimSpace(cfg.Resources.LaunchTemplates[arch]) == "" {
			return "", "", fmt.Errorf("launch template for %s is not configured; run `forja init` again", arch)
		}
	}
	if len(platforms) > 1 && opts.load {
		return "", "", fmt.Errorf("--load is only supported for single-platform builds")
	}
	if len(platforms) > 1 && opts.instanceType != "" {
		return "", "", fmt.Errorf("--instance-type override is only supported for single-platform builds")
	}

	subnetID := configuredSubnetID(cfg)
	if subnetID == "" {
		return "", "", fmt.Errorf("no default subnet is configured; run `forja init` again")
	}

	repo := ""
	if len(platforms) > 1 && opts.push {
		var err error
		repo, err = ensureSameRepository(opts.tags)
		if err != nil {
			return "", "", err
		}
	}
	return repo, subnetID, nil
}
