package aws

import (
	"context"
	"encoding/base64"
	"fmt"
	"time"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
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
