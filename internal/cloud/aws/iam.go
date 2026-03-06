package aws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
)

const (
	roleName            = "forja-builder"
	instanceProfileName = "forja-builder"
	inlinePolicyName    = "forja-builder-inline"
)

func (p *Provider) ensureIAM(ctx context.Context, bucket string) (roleARN string, profileARN string, err error) {
	assumeRolePolicy := `{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": {"Service": "ec2.amazonaws.com"},
      "Action": "sts:AssumeRole"
    }
  ]
}`

	getRole, err := p.iam.GetRole(ctx, &iam.GetRoleInput{RoleName: sdkaws.String(roleName)})
	if err != nil {
		var noSuchEntity *iamtypes.NoSuchEntityException
		if !errors.As(err, &noSuchEntity) {
			return "", "", fmt.Errorf("get iam role: %w", err)
		}
		createOut, createErr := p.iam.CreateRole(ctx, &iam.CreateRoleInput{
			RoleName:                 sdkaws.String(roleName),
			AssumeRolePolicyDocument: sdkaws.String(assumeRolePolicy),
			Description:              sdkaws.String("Forja EC2 builder role"),
		})
		if createErr != nil {
			return "", "", fmt.Errorf("create iam role: %w", createErr)
		}
		getRole = &iam.GetRoleOutput{Role: createOut.Role}
	}

	policy, err := inlinePolicy(bucket)
	if err != nil {
		return "", "", err
	}
	if _, err := p.iam.PutRolePolicy(ctx, &iam.PutRolePolicyInput{
		PolicyDocument: sdkaws.String(policy),
		PolicyName:     sdkaws.String(inlinePolicyName),
		RoleName:       sdkaws.String(roleName),
	}); err != nil {
		return "", "", fmt.Errorf("put role policy: %w", err)
	}

	profile, err := p.iam.GetInstanceProfile(ctx, &iam.GetInstanceProfileInput{
		InstanceProfileName: sdkaws.String(instanceProfileName),
	})
	if err != nil {
		var noSuchEntity *iamtypes.NoSuchEntityException
		if !errors.As(err, &noSuchEntity) {
			return "", "", fmt.Errorf("get instance profile: %w", err)
		}
		created, createErr := p.iam.CreateInstanceProfile(ctx, &iam.CreateInstanceProfileInput{
			InstanceProfileName: sdkaws.String(instanceProfileName),
		})
		if createErr != nil {
			return "", "", fmt.Errorf("create instance profile: %w", createErr)
		}
		profile = &iam.GetInstanceProfileOutput{InstanceProfile: created.InstanceProfile}
	}

	if !profileHasRole(profile.InstanceProfile.Roles, roleName) {
		if _, err := p.iam.AddRoleToInstanceProfile(ctx, &iam.AddRoleToInstanceProfileInput{
			InstanceProfileName: sdkaws.String(instanceProfileName),
			RoleName:            sdkaws.String(roleName),
		}); err != nil {
			var limitExceeded *iamtypes.LimitExceededException
			if !errors.As(err, &limitExceeded) {
				return "", "", fmt.Errorf("attach role to instance profile: %w", err)
			}
		}
	}

	time.Sleep(5 * time.Second)
	return sdkaws.ToString(getRole.Role.Arn), sdkaws.ToString(profile.InstanceProfile.Arn), nil
}

func inlinePolicy(bucket string) (string, error) {
	policy := map[string]any{
		"Version": "2012-10-17",
		"Statement": []map[string]any{
			{
				"Sid":    "S3CacheAndCerts",
				"Effect": "Allow",
				"Action": []string{"s3:GetObject", "s3:PutObject", "s3:DeleteObject", "s3:ListBucket"},
				"Resource": []string{
					fmt.Sprintf("arn:aws:s3:::%s", bucket),
					fmt.Sprintf("arn:aws:s3:::%s/*", bucket),
				},
			},
			{
				"Sid":    "ECRPush",
				"Effect": "Allow",
				"Action": []string{
					"ecr:GetAuthorizationToken",
					"ecr:BatchCheckLayerAvailability",
					"ecr:PutImage",
					"ecr:BatchGetImage",
					"ecr:InitiateLayerUpload",
					"ecr:UploadLayerPart",
					"ecr:CompleteLayerUpload",
				},
				"Resource": "*",
			},
			{
				"Sid":      "SelfTerminate",
				"Effect":   "Allow",
				"Action":   "ec2:TerminateInstances",
				"Resource": "*",
				"Condition": map[string]any{
					"StringEquals": map[string]string{
						"ec2:ResourceTag/forja:managed": "true",
					},
				},
			},
		},
	}
	data, err := json.Marshal(policy)
	if err != nil {
		return "", fmt.Errorf("marshal iam policy: %w", err)
	}
	return string(data), nil
}

func profileHasRole(roles []iamtypes.Role, name string) bool {
	for _, role := range roles {
		if sdkaws.ToString(role.RoleName) == name {
			return true
		}
	}
	return false
}
