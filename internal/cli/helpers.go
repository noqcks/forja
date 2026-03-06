package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/noqcks/forja/internal/buildkit"
	"github.com/noqcks/forja/internal/cloud"
	awsprovider "github.com/noqcks/forja/internal/cloud/aws"
	"github.com/noqcks/forja/internal/config"
)

func providerFromConfig(ctx context.Context, cfg *config.Config, profile string) (cloud.Provider, error) {
	switch cfg.Provider {
	case "", "aws":
		return awsprovider.New(ctx, cfg.Region, profile)
	default:
		return nil, fmt.Errorf("unsupported provider %q", cfg.Provider)
	}
}

func platformList(flagValue string, defaultPlatform string) []string {
	if strings.TrimSpace(flagValue) == "" {
		flagValue = defaultPlatform
	}
	parts := strings.Split(flagValue, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func platformArch(platform string) (string, error) {
	switch strings.TrimSpace(platform) {
	case "linux/amd64":
		return "amd64", nil
	case "linux/arm64":
		return "arm64", nil
	default:
		return "", fmt.Errorf("unsupported platform %q", platform)
	}
}

func platformDescriptor(platform string) (v1.Platform, error) {
	switch strings.TrimSpace(platform) {
	case "linux/amd64":
		return v1.Platform{OS: "linux", Architecture: "amd64"}, nil
	case "linux/arm64":
		return v1.Platform{OS: "linux", Architecture: "arm64"}, nil
	default:
		return v1.Platform{}, fmt.Errorf("unsupported platform %q", platform)
	}
}

func renderUserData(certS3Path string, cacheBucket string, region string, selfDestructMinutes int) string {
	return fmt.Sprintf(`#!/bin/bash
set -euo pipefail

mkdir -p /etc/buildkit/certs

CERT_S3_PATH=%q
CACHE_REGION=%q
SELF_DESTRUCT_MINUTES=%d

aws s3 cp "${CERT_S3_PATH}/server-cert.pem" /etc/buildkit/certs/server-cert.pem
aws s3 cp "${CERT_S3_PATH}/server-key.pem" /etc/buildkit/certs/server-key.pem
aws s3 cp "${CERT_S3_PATH}/ca-cert.pem" /etc/buildkit/certs/ca-cert.pem

nohup buildkitd \
  --addr tcp://0.0.0.0:8372 \
  --tlscacert /etc/buildkit/certs/ca-cert.pem \
  --tlscert /etc/buildkit/certs/server-cert.pem \
  --tlskey /etc/buildkit/certs/server-key.pem > /var/log/buildkitd.log 2>&1 &

(sleep $((SELF_DESTRUCT_MINUTES * 60)) && \
  INSTANCE_ID=$(TOKEN=$(curl -sX PUT "http://169.254.169.254/latest/api/token" -H "X-aws-ec2-metadata-token-ttl-seconds: 30") && curl -s -H "X-aws-ec2-metadata-token: $TOKEN" http://169.254.169.254/latest/meta-data/instance-id) && \
  aws ec2 terminate-instances --instance-ids "$INSTANCE_ID" --region "$CACHE_REGION") &
`, certS3Path, region, selfDestructMinutes)
}

func cacheNameForContext(contextDir string) string {
	abs, err := filepath.Abs(contextDir)
	if err != nil {
		return buildkit.CacheName(contextDir)
	}
	return buildkit.CacheName(abs)
}

func ensureSameRepository(tags []string) (string, error) {
	if len(tags) == 0 {
		return "", nil
	}
	repo, err := repositoryOfTag(tags[0])
	if err != nil {
		return "", err
	}
	for _, tag := range tags[1:] {
		nextRepo, err := repositoryOfTag(tag)
		if err != nil {
			return "", err
		}
		if nextRepo != repo {
			return "", fmt.Errorf("all tags must share a repository for multi-arch push; got %s and %s", repo, nextRepo)
		}
	}
	return repo, nil
}

func repositoryOfTag(tag string) (string, error) {
	lastColon := strings.LastIndex(tag, ":")
	lastSlash := strings.LastIndex(tag, "/")
	if lastColon == -1 || lastColon < lastSlash {
		return "", fmt.Errorf("tag %q must include a tag component", tag)
	}
	return tag[:lastColon], nil
}
