package aws

import (
	"context"
	"fmt"
	"strings"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/noqcks/forja/internal/cloud"
)

func (p *Provider) ensureBucket(ctx context.Context, bucket string, ttlDays int) error {
	_, err := p.s3.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: sdkaws.String(bucket)})
	if err != nil {
		input := &s3.CreateBucketInput{Bucket: sdkaws.String(bucket)}
		if p.region != "us-east-1" {
			input.CreateBucketConfiguration = &s3types.CreateBucketConfiguration{
				LocationConstraint: s3types.BucketLocationConstraint(p.region),
			}
		}
		if _, createErr := p.s3.CreateBucket(ctx, input); createErr != nil && !strings.Contains(createErr.Error(), "BucketAlreadyOwnedByYou") {
			return fmt.Errorf("create bucket: %w", createErr)
		}
	}

	_, err = p.s3.PutBucketLifecycleConfiguration(ctx, &s3.PutBucketLifecycleConfigurationInput{
		Bucket: sdkaws.String(bucket),
		LifecycleConfiguration: &s3types.BucketLifecycleConfiguration{
			Rules: []s3types.LifecycleRule{
				{
					ID:     sdkaws.String("forja-cache-expiry"),
					Status: s3types.ExpirationStatusEnabled,
					Expiration: &s3types.LifecycleExpiration{
						Days: sdkaws.Int32(int32(ttlDays)),
					},
					Filter: &s3types.LifecycleRuleFilter{Prefix: sdkaws.String("")},
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("put lifecycle rule: %w", err)
	}
	return nil
}

func (p *Provider) UploadCertificates(ctx context.Context, req cloud.UploadCertificatesRequest) (string, error) {
	prefix := fmt.Sprintf("builds/%s/certs", req.BuildID)
	objects := map[string][]byte{
		prefix + "/ca-cert.pem":     req.CACert,
		prefix + "/server-cert.pem": req.ServerCert,
		prefix + "/server-key.pem":  req.ServerKey,
	}
	for key, data := range objects {
		_, err := p.s3.PutObject(ctx, &s3.PutObjectInput{
			Bucket:               sdkaws.String(req.Bucket),
			Key:                  sdkaws.String(key),
			Body:                 strings.NewReader(string(data)),
			ServerSideEncryption: s3types.ServerSideEncryptionAes256,
		})
		if err != nil {
			return "", fmt.Errorf("upload certificate %s: %w", key, err)
		}
	}
	return fmt.Sprintf("s3://%s/%s", req.Bucket, prefix), nil
}

func (p *Provider) DeleteCertificates(ctx context.Context, bucket string, buildID string) error {
	prefix := fmt.Sprintf("builds/%s/certs/", buildID)
	out, err := p.s3.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: sdkaws.String(bucket),
		Prefix: sdkaws.String(prefix),
	})
	if err != nil {
		return fmt.Errorf("list certificates: %w", err)
	}
	if len(out.Contents) == 0 {
		return nil
	}
	objects := make([]s3types.ObjectIdentifier, 0, len(out.Contents))
	for _, object := range out.Contents {
		objects = append(objects, s3types.ObjectIdentifier{Key: object.Key})
	}
	_, err = p.s3.DeleteObjects(ctx, &s3.DeleteObjectsInput{
		Bucket: sdkaws.String(bucket),
		Delete: &s3types.Delete{Objects: objects, Quiet: sdkaws.Bool(true)},
	})
	if err != nil {
		return fmt.Errorf("delete certificates: %w", err)
	}
	return nil
}

func (p *Provider) emptyBucket(ctx context.Context, bucket string) error {
	var token *string
	for {
		out, err := p.s3.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket:            sdkaws.String(bucket),
			ContinuationToken: token,
		})
		if err != nil {
			return fmt.Errorf("list bucket contents: %w", err)
		}
		if len(out.Contents) > 0 {
			objects := make([]s3types.ObjectIdentifier, 0, len(out.Contents))
			for _, object := range out.Contents {
				objects = append(objects, s3types.ObjectIdentifier{Key: object.Key})
			}
			if _, err := p.s3.DeleteObjects(ctx, &s3.DeleteObjectsInput{
				Bucket: sdkaws.String(bucket),
				Delete: &s3types.Delete{Objects: objects, Quiet: sdkaws.Bool(true)},
			}); err != nil {
				return fmt.Errorf("delete bucket objects: %w", err)
			}
		}
		if !sdkaws.ToBool(out.IsTruncated) {
			break
		}
		token = out.NextContinuationToken
	}
	return nil
}
