package aws

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/pricing"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/noqcks/forja/internal/cloud"
)

type Provider struct {
	cfg     sdkaws.Config
	region  string
	ec2     *ec2.Client
	iam     *iam.Client
	s3      *s3.Client
	sts     *sts.Client
	pricing *pricing.Client
}

func New(ctx context.Context, region string, profile string) (*Provider, error) {
	opts := []func(*awsconfig.LoadOptions) error{}
	if region != "" {
		opts = append(opts, awsconfig.WithRegion(region))
	}
	if profile != "" {
		opts = append(opts, awsconfig.WithSharedConfigProfile(profile))
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}
	if cfg.Region == "" {
		return nil, fmt.Errorf("aws region is required")
	}
	return &Provider{
		cfg:     cfg,
		region:  cfg.Region,
		ec2:     ec2.NewFromConfig(cfg),
		iam:     iam.NewFromConfig(cfg),
		s3:      s3.NewFromConfig(cfg),
		sts:     sts.NewFromConfig(cfg),
		pricing: pricing.NewFromConfig(cfg, func(o *pricing.Options) { o.Region = "us-east-1" }),
	}, nil
}

func (p *Provider) Name() string {
	return "aws"
}

func (p *Provider) Identity(ctx context.Context) (*cloud.Identity, error) {
	out, err := p.sts.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return nil, fmt.Errorf("get caller identity: %w", err)
	}
	return &cloud.Identity{
		AccountID: sdkaws.ToString(out.Account),
		ARN:       sdkaws.ToString(out.Arn),
	}, nil
}

func (p *Provider) InstanceTypeArchitectures(ctx context.Context, instanceType string) ([]string, error) {
	instanceType = strings.TrimSpace(instanceType)
	if instanceType == "" {
		return nil, nil
	}
	out, err := p.ec2.DescribeInstanceTypes(ctx, &ec2.DescribeInstanceTypesInput{
		InstanceTypes: []ec2types.InstanceType{ec2types.InstanceType(instanceType)},
	})
	if err != nil {
		return nil, fmt.Errorf("describe instance types: %w", err)
	}
	if len(out.InstanceTypes) == 0 {
		return nil, fmt.Errorf("aws returned no instance type info for %s", instanceType)
	}
	return forjaArchitectures(out.InstanceTypes[0].ProcessorInfo), nil
}

func forjaArchitectures(info *ec2types.ProcessorInfo) []string {
	if info == nil || len(info.SupportedArchitectures) == 0 {
		return nil
	}
	result := make([]string, 0, len(info.SupportedArchitectures))
	seen := map[string]struct{}{}
	for _, supported := range info.SupportedArchitectures {
		var arch string
		switch string(supported) {
		case "x86_64":
			arch = "amd64"
		case "arm64":
			arch = "arm64"
		default:
			continue
		}
		if _, ok := seen[arch]; ok {
			continue
		}
		seen[arch] = struct{}{}
		result = append(result, arch)
	}
	return result
}

func encodeUserData(script string) string {
	return base64.StdEncoding.EncodeToString([]byte(script))
}

func retry(ctx context.Context, attempts int, fn func() error) error {
	var err error
	for i := 0; i < attempts; i++ {
		if err = fn(); err == nil {
			return nil
		}
		if i == attempts-1 {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(i+1) * time.Second):
		}
	}
	return err
}
