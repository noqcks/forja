package cli

import (
	"context"
	"fmt"

	"github.com/AlecAivazis/survey/v2"
	"github.com/noqcks/forja/internal/cloud"
	awsprovider "github.com/noqcks/forja/internal/cloud/aws"
	"github.com/noqcks/forja/internal/config"
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
	region := "us-east-1"
	defaultPlatform := "linux/amd64"
	sizeChoice := "Small"
	registry := ""
	amd64AMI := ""
	arm64AMI := ""
	customAMD64 := "c7a.large"
	customARM64 := "c7g.large"

	questions := []*survey.Question{
		{
			Name: "region",
			Prompt: &survey.Input{
				Message: "AWS region:",
				Default: region,
			},
		},
		{
			Name: "defaultPlatform",
			Prompt: &survey.Select{
				Message: "Default platform:",
				Default: defaultPlatform,
				Options: []string{"linux/amd64", "linux/arm64"},
			},
		},
		{
			Name: "sizeChoice",
			Prompt: &survey.Select{
				Message: "Instance size for builds:",
				Default: sizeChoice,
				Options: []string{
					"Small (c7a.large / c7g.large)",
					"Medium (c7a.xlarge / c7g.xlarge)",
					"Large (c7a.2xlarge / c7g.2xlarge)",
					"Custom",
				},
			},
		},
		{
			Name: "registry",
			Prompt: &survey.Input{
				Message: "Default registry (optional):",
				Default: registry,
			},
		},
		{
			Name: "amd64AMI",
			Prompt: &survey.Input{
				Message: "Published amd64 AMI ID:",
				Default: amd64AMI,
			},
			Validate: survey.Required,
		},
		{
			Name: "arm64AMI",
			Prompt: &survey.Input{
				Message: "Published arm64 AMI ID:",
				Default: arm64AMI,
			},
			Validate: survey.Required,
		},
	}

	answers := struct {
		Region          string
		DefaultPlatform string
		SizeChoice      string
		Registry        string
		AMD64AMI        string
		ARM64AMI        string
	}{}
	if err := survey.Ask(questions, &answers); err != nil {
		return err
	}

	instances := map[string]string{}
	switch answers.SizeChoice {
	case "Medium (c7a.xlarge / c7g.xlarge)":
		instances["amd64"] = "c7a.xlarge"
		instances["arm64"] = "c7g.xlarge"
	case "Large (c7a.2xlarge / c7g.2xlarge)":
		instances["amd64"] = "c7a.2xlarge"
		instances["arm64"] = "c7g.2xlarge"
	case "Custom":
		if err := survey.AskOne(&survey.Input{Message: "Custom amd64 instance type:", Default: customAMD64}, &customAMD64, survey.WithValidator(survey.Required)); err != nil {
			return err
		}
		if err := survey.AskOne(&survey.Input{Message: "Custom arm64 instance type:", Default: customARM64}, &customARM64, survey.WithValidator(survey.Required)); err != nil {
			return err
		}
		instances["amd64"] = customAMD64
		instances["arm64"] = customARM64
	default:
		instances["amd64"] = "c7a.large"
		instances["arm64"] = "c7g.large"
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
	fmt.Fprintf(cmd.OutOrStdout(), "AWS credentials detected (account: %s)\n\n", identity.AccountID)
	fmt.Fprintln(cmd.OutOrStdout(), "Creating AWS resources...")

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
	cfg.DefaultPlatform = answers.DefaultPlatform
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

	fmt.Fprintf(cmd.OutOrStdout(), "  [ok] S3 bucket: %s\n", cfg.CacheBucket)
	fmt.Fprintf(cmd.OutOrStdout(), "  [ok] IAM role: %s\n", cfg.Resources.IAMRoleName)
	fmt.Fprintf(cmd.OutOrStdout(), "  [ok] Security group: %s (%s)\n", cfg.Resources.SecurityGroupID, cfg.Resources.SecurityGroupName)
	fmt.Fprintf(cmd.OutOrStdout(), "  [ok] Launch template: %s (amd64)\n", cfg.Resources.LaunchTemplates["amd64"])
	fmt.Fprintf(cmd.OutOrStdout(), "  [ok] Launch template: %s (arm64)\n", cfg.Resources.LaunchTemplates["arm64"])
	path, _ := config.ConfigPath()
	fmt.Fprintf(cmd.OutOrStdout(), "\nConfig written to %s\n\n", path)
	fmt.Fprintln(cmd.OutOrStdout(), "Ready! Try: forja build .")
	return nil
}
