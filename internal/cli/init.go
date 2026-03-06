package cli

import (
	"context"
	"fmt"

	"github.com/noqcks/forja/internal/cloud"
	awsprovider "github.com/noqcks/forja/internal/cloud/aws"
	"github.com/noqcks/forja/internal/config"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

func newInitCmd(root *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Provision Forja AWS infrastructure",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			return runInit(ctx, cmd, root)
		},
	}
}

func runInit(ctx context.Context, cmd *cobra.Command, root *rootOptions) error {
	answers, err := collectInitAnswers(cmd)
	if err != nil {
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
		return err
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
