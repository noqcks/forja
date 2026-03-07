package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/noqcks/forja/internal/cloud"
	awsprovider "github.com/noqcks/forja/internal/cloud/aws"
	"github.com/noqcks/forja/internal/config"
	releaseinfo "github.com/noqcks/forja/internal/release"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

type initOptions struct {
	noTUI       bool
	region      string
	size        string
	registry    string
	amd64AMI    string
	arm64AMI    string
	customAMD64 string
	customARM64 string
}

func newInitCmd(root *rootOptions) *cobra.Command {
	opts := &initOptions{}
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Provision Forja AWS infrastructure",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			return runInit(ctx, cmd, root, opts)
		},
	}

	cmd.Flags().BoolVar(&opts.noTUI, "no-tui", false, "Disable the interactive init UI and use flags only")
	cmd.Flags().StringVar(&opts.region, "region", "us-east-1", "AWS region")
	cmd.Flags().StringVar(&opts.size, "size", "small", "Builder size preset: small, medium, large, or custom")
	cmd.Flags().StringVar(&opts.registry, "registry", "", "Default registry prefix")
	cmd.Flags().StringVar(&opts.amd64AMI, "amd64-ami", defaultAMD64AMI, "Published amd64 AMI ID")
	cmd.Flags().StringVar(&opts.arm64AMI, "arm64-ami", defaultARM64AMI, "Published arm64 AMI ID")
	cmd.Flags().StringVar(&opts.customAMD64, "amd64-instance", "c7a.large", "Custom amd64 instance type when --size=custom")
	cmd.Flags().StringVar(&opts.customARM64, "arm64-instance", "c7g.large", "Custom arm64 instance type when --size=custom")
	return cmd
}

func runInit(ctx context.Context, cmd *cobra.Command, root *rootOptions, opts *initOptions) error {
	answers, err := collectInitAnswers(cmd, opts)
	if err != nil {
		if errors.Is(err, errInitCanceled) {
			log.Info("Init cancelled.")
			return nil
		}
		return err
	}
	answers.AMD64AMI = resolvePublishedAMI(answers.Region, "amd64", answers.AMD64AMI)
	answers.ARM64AMI = resolvePublishedAMI(answers.Region, "arm64", answers.ARM64AMI)
	if err := validateInitAnswers(answers); err != nil {
		return err
	}

	instances, err := instanceTypesForSizeChoice(answers.SizeChoice, answers.CustomAMD64, answers.CustomARM64)
	if err != nil {
		return err
	}

	provider, err := awsprovider.New(ctx, answers.Region, root.profile)
	if err != nil {
		return err
	}
	identity, err := provider.Identity(ctx)
	if err != nil {
		return formatAWSIdentityError(err, root.profile)
	}

	cacheBucket := fmt.Sprintf("forja-cache-%s-%s", identity.AccountID, answers.Region)
	log.Infof("AWS credentials detected (account: %s)", identity.AccountID)
	log.Info("Creating AWS resources...")

	result, err := provider.EnsureInfrastructure(ctx, cloud.ProvisionRequest{
		Region:              answers.Region,
		AccountID:           identity.AccountID,
		CacheBucket:         cacheBucket,
		CacheTTLDays:        14,
		SelfDestructMinutes: 60,
		Instances:           instances,
		Registry:            answers.Registry,
		PublishedAMI: map[string]string{
			"amd64": answers.AMD64AMI,
			"arm64": answers.ARM64AMI,
		},
	})
	if err != nil {
		return err
	}

	cfg := config.Default()
	cfg.Region = answers.Region
	cfg.Instances = instances
	cfg.Registry = answers.Registry
	cfg.CacheBucket = cacheBucket
	cfg.PublishedAMI = map[string]string{
		"amd64": answers.AMD64AMI,
		"arm64": answers.ARM64AMI,
	}
	cfg.Resources.AccountID = identity.AccountID
	cfg.Resources.SecurityGroupID = result.SecurityGroupID
	cfg.Resources.SecurityGroupName = result.SecurityGroupName
	cfg.Resources.IAMRoleName = result.IAMRoleName
	cfg.Resources.IAMRoleARN = result.IAMRoleARN
	cfg.Resources.InstanceProfileName = result.InstanceProfileName
	cfg.Resources.InstanceProfileARN = result.InstanceProfileARN
	cfg.Resources.DefaultVPCID = result.DefaultVPCID
	cfg.Resources.DefaultSubnetIDs = result.DefaultSubnetIDs
	cfg.Resources.LaunchTemplates = result.LaunchTemplates
	cfg.Resources.AMI = result.AMI
	if err := config.Save(cfg); err != nil {
		return err
	}

	log.Infof("[ok] S3 bucket: %s", cfg.CacheBucket)
	log.Infof("[ok] IAM role: %s", cfg.Resources.IAMRoleName)
	log.Infof("[ok] Security group: %s (%s)", cfg.Resources.SecurityGroupID, cfg.Resources.SecurityGroupName)
	log.Infof("[ok] Launch template: %s (amd64)", cfg.Resources.LaunchTemplates["amd64"])
	log.Infof("[ok] Launch template: %s (arm64)", cfg.Resources.LaunchTemplates["arm64"])
	path, _ := config.ConfigPath()
	log.Infof("Config written to %s", path)
	log.Info("Ready! Try: forja build .")
	return nil
}

func collectInitAnswers(cmd *cobra.Command, opts *initOptions) (initAnswers, error) {
	if shouldUseFlagInit(cmd, opts) {
		answers := initAnswers{
			Region:      strings.TrimSpace(opts.region),
			SizeChoice:  normalizeSizeChoice(opts.size),
			Registry:    strings.TrimSpace(opts.registry),
			AMD64AMI:    strings.TrimSpace(opts.amd64AMI),
			ARM64AMI:    strings.TrimSpace(opts.arm64AMI),
			CustomAMD64: strings.TrimSpace(opts.customAMD64),
			CustomARM64: strings.TrimSpace(opts.customARM64),
		}
		return answers, nil
	}
	return collectInitAnswersTUI(cmd)
}

func resolvePublishedAMI(region string, arch string, explicit string) string {
	explicit = strings.TrimSpace(explicit)
	if explicit != "" {
		return explicit
	}
	return releaseinfo.AWSAMI(strings.TrimSpace(region), arch)
}

func shouldUseFlagInit(cmd *cobra.Command, opts *initOptions) bool {
	if opts.noTUI {
		return true
	}
	for _, name := range []string{
		"region",
		"size",
		"registry",
		"amd64-ami",
		"arm64-ami",
		"amd64-instance",
		"arm64-instance",
	} {
		if cmd.Flags().Changed(name) {
			return true
		}
	}
	return !isTerminal(cmd.InOrStdin()) || !isTerminal(cmd.OutOrStdout())
}

func isTerminal(stream any) bool {
	file, ok := stream.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(file.Fd()))
}

func normalizeSizeChoice(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "small", initSizeSmall:
		return initSizeSmall
	case "medium", initSizeMedium:
		return initSizeMedium
	case "large", initSizeLarge:
		return initSizeLarge
	case "custom", initSizeCustom:
		return initSizeCustom
	default:
		return strings.TrimSpace(value)
	}
}
