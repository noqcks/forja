package aws

import (
	"context"
	"fmt"
	"sort"
	"strings"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/noqcks/forja/internal/cloud"
)

const buildKitPort int32 = 8372

func (p *Provider) EnsureInfrastructure(ctx context.Context, req cloud.ProvisionRequest) (*cloud.ProvisionResult, error) {
	if err := p.ensureBucket(ctx, req.CacheBucket, req.CacheTTLDays); err != nil {
		return nil, err
	}
	roleARN, profileARN, err := p.ensureIAM(ctx, req.CacheBucket)
	if err != nil {
		return nil, err
	}
	vpcID, subnetIDs, err := p.defaultNetwork(ctx)
	if err != nil {
		return nil, err
	}
	sgID, sgName, err := p.ensureSecurityGroup(ctx, vpcID)
	if err != nil {
		return nil, err
	}
	return &cloud.ProvisionResult{
		SecurityGroupID:     sgID,
		SecurityGroupName:   sgName,
		IAMRoleName:         roleName,
		IAMRoleARN:          roleARN,
		InstanceProfileName: instanceProfileName,
		InstanceProfileARN:  profileARN,
		DefaultVPCID:        vpcID,
		DefaultSubnetIDs:    subnetIDs,
		LaunchTemplates:     map[string]string{},
	}, nil
}

func (p *Provider) defaultNetwork(ctx context.Context) (string, []string, error) {
	vpcs, err := p.ec2.DescribeVpcs(ctx, &ec2.DescribeVpcsInput{
		Filters: []ec2types.Filter{{Name: sdkaws.String("isDefault"), Values: []string{"true"}}},
	})
	if err != nil {
		return "", nil, fmt.Errorf("describe default vpc: %w", err)
	}
	if len(vpcs.Vpcs) == 0 {
		return "", nil, fmt.Errorf("no default VPC found in %s", p.region)
	}
	vpcID := sdkaws.ToString(vpcs.Vpcs[0].VpcId)
	subnets, err := p.ec2.DescribeSubnets(ctx, &ec2.DescribeSubnetsInput{
		Filters: []ec2types.Filter{{Name: sdkaws.String("default-for-az"), Values: []string{"true"}}},
	})
	if err != nil {
		return "", nil, fmt.Errorf("describe default subnets: %w", err)
	}
	subnetIDs := make([]string, 0, len(subnets.Subnets))
	for _, subnet := range subnets.Subnets {
		if sdkaws.ToString(subnet.VpcId) == vpcID {
			subnetIDs = append(subnetIDs, sdkaws.ToString(subnet.SubnetId))
		}
	}
	sort.Strings(subnetIDs)
	if len(subnetIDs) == 0 {
		return "", nil, fmt.Errorf("no default subnets found in default VPC %s", vpcID)
	}
	return vpcID, subnetIDs, nil
}

func (p *Provider) ensureSecurityGroup(ctx context.Context, vpcID string) (string, string, error) {
	const name = "forja-builder"
	existing, err := p.ec2.DescribeSecurityGroups(ctx, &ec2.DescribeSecurityGroupsInput{
		Filters: []ec2types.Filter{
			{Name: sdkaws.String("group-name"), Values: []string{name}},
			{Name: sdkaws.String("vpc-id"), Values: []string{vpcID}},
		},
	})
	if err != nil {
		return "", "", fmt.Errorf("describe security groups: %w", err)
	}
	if len(existing.SecurityGroups) > 0 {
		return sdkaws.ToString(existing.SecurityGroups[0].GroupId), name, nil
	}
	out, err := p.ec2.CreateSecurityGroup(ctx, &ec2.CreateSecurityGroupInput{
		GroupName:   sdkaws.String(name),
		Description: sdkaws.String("Forja BuildKit builders"),
		VpcId:       sdkaws.String(vpcID),
		TagSpecifications: []ec2types.TagSpecification{
			{ResourceType: ec2types.ResourceTypeSecurityGroup, Tags: defaultTags("forja-builder")},
		},
	})
	if err != nil {
		return "", "", fmt.Errorf("create security group: %w", err)
	}
	_, err = p.ec2.AuthorizeSecurityGroupIngress(ctx, &ec2.AuthorizeSecurityGroupIngressInput{
		GroupId: out.GroupId,
		IpPermissions: []ec2types.IpPermission{
			{
				FromPort:   sdkaws.Int32(buildKitPort),
				ToPort:     sdkaws.Int32(buildKitPort),
				IpProtocol: sdkaws.String("tcp"),
				IpRanges:   []ec2types.IpRange{{CidrIp: sdkaws.String("0.0.0.0/0")}},
			},
		},
	})
	if err != nil && !strings.Contains(err.Error(), "InvalidPermission.Duplicate") {
		return "", "", fmt.Errorf("authorize security group ingress: %w", err)
	}
	return sdkaws.ToString(out.GroupId), name, nil
}

func (p *Provider) ensureLaunchTemplates(ctx context.Context, amis map[string]string, instances map[string]string, sgID string) (map[string]string, error) {
	result := map[string]string{}
	for _, arch := range []string{"amd64", "arm64"} {
		if strings.TrimSpace(amis[arch]) == "" {
			return nil, fmt.Errorf("published AMI for %s is required", arch)
		}
		if strings.TrimSpace(instances[arch]) == "" {
			return nil, fmt.Errorf("instance type for %s is required", arch)
		}
		name := "forja-builder-" + arch
		data := &ec2types.RequestLaunchTemplateData{
			ImageId:      sdkaws.String(amis[arch]),
			InstanceType: ec2types.InstanceType(instances[arch]),
			SecurityGroupIds: []string{
				sgID,
			},
			IamInstanceProfile: &ec2types.LaunchTemplateIamInstanceProfileSpecificationRequest{
				Name: sdkaws.String(instanceProfileName),
			},
			MetadataOptions: &ec2types.LaunchTemplateInstanceMetadataOptionsRequest{
				HttpTokens:   ec2types.LaunchTemplateHttpTokensStateRequired,
				HttpEndpoint: ec2types.LaunchTemplateInstanceMetadataEndpointStateEnabled,
			},
			BlockDeviceMappings: []ec2types.LaunchTemplateBlockDeviceMappingRequest{
				{
					DeviceName: sdkaws.String("/dev/xvda"),
					Ebs: &ec2types.LaunchTemplateEbsBlockDeviceRequest{
						DeleteOnTermination: sdkaws.Bool(true),
						VolumeSize:          sdkaws.Int32(20),
						VolumeType:          ec2types.VolumeTypeGp3,
					},
				},
			},
		}

		desc, err := p.ec2.DescribeLaunchTemplates(ctx, &ec2.DescribeLaunchTemplatesInput{
			LaunchTemplateNames: []string{name},
		})
		if err != nil {
			if !strings.Contains(err.Error(), "InvalidLaunchTemplateName.NotFoundException") {
				return nil, fmt.Errorf("describe launch templates: %w", err)
			}
			desc = &ec2.DescribeLaunchTemplatesOutput{}
		}
		if len(desc.LaunchTemplates) == 0 {
			out, createErr := p.ec2.CreateLaunchTemplate(ctx, &ec2.CreateLaunchTemplateInput{
				LaunchTemplateName: sdkaws.String(name),
				LaunchTemplateData: data,
				TagSpecifications: []ec2types.TagSpecification{
					{ResourceType: ec2types.ResourceTypeLaunchTemplate, Tags: defaultTags(name)},
				},
			})
			if createErr != nil {
				return nil, fmt.Errorf("create launch template %s: %w", name, createErr)
			}
			result[arch] = sdkaws.ToString(out.LaunchTemplate.LaunchTemplateId)
			continue
		}
		lt := desc.LaunchTemplates[0]
		_, err = p.ec2.CreateLaunchTemplateVersion(ctx, &ec2.CreateLaunchTemplateVersionInput{
			LaunchTemplateId:   lt.LaunchTemplateId,
			LaunchTemplateData: data,
		})
		if err != nil {
			return nil, fmt.Errorf("create launch template version %s: %w", name, err)
		}
		_, err = p.ec2.ModifyLaunchTemplate(ctx, &ec2.ModifyLaunchTemplateInput{
			LaunchTemplateId: lt.LaunchTemplateId,
			DefaultVersion:   sdkaws.String("$Latest"),
		})
		if err != nil {
			return nil, fmt.Errorf("set default launch template version %s: %w", name, err)
		}
		result[arch] = sdkaws.ToString(lt.LaunchTemplateId)
	}
	return result, nil
}

func (p *Provider) LaunchBuilder(ctx context.Context, req cloud.LaunchBuilderRequest) (*cloud.BuilderInstance, error) {
	out, err := p.ec2.RunInstances(ctx, &ec2.RunInstancesInput{
		MinCount: sdkaws.Int32(1),
		MaxCount: sdkaws.Int32(1),
		LaunchTemplate: &ec2types.LaunchTemplateSpecification{
			LaunchTemplateId: sdkaws.String(req.LaunchTemplateID),
			Version:          sdkaws.String("$Latest"),
		},
		InstanceType: ec2types.InstanceType(req.InstanceTypeOverride),
		UserData:     sdkaws.String(encodeUserData(req.UserData)),
		SubnetId:     sdkaws.String(req.SubnetID),
		TagSpecifications: []ec2types.TagSpecification{
			{
				ResourceType: ec2types.ResourceTypeInstance,
				Tags: append(defaultTags("forja-builder"), []ec2types.Tag{
					{Key: sdkaws.String("forja:build-id"), Value: sdkaws.String(req.BuildID)},
					{Key: sdkaws.String("forja:arch"), Value: sdkaws.String(req.Architecture)},
				}...),
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("run instances: %w", err)
	}
	if len(out.Instances) == 0 {
		return nil, fmt.Errorf("aws returned no instance")
	}
	instanceID := sdkaws.ToString(out.Instances[0].InstanceId)
	describe := func() (*cloud.BuilderInstance, error) {
		desc, describeErr := p.ec2.DescribeInstances(ctx, &ec2.DescribeInstancesInput{InstanceIds: []string{instanceID}})
		if describeErr != nil {
			return nil, describeErr
		}
		for _, reservation := range desc.Reservations {
			for _, instance := range reservation.Instances {
				if instance.State != nil && instance.State.Name == ec2types.InstanceStateNameRunning && instance.PublicIpAddress != nil {
					instanceType := string(instance.InstanceType)
					if req.InstanceTypeOverride == "" {
						instanceType = string(instance.InstanceType)
					}
					return &cloud.BuilderInstance{
						ID:           instanceID,
						PublicIP:     sdkaws.ToString(instance.PublicIpAddress),
						InstanceType: instanceType,
						Architecture: req.Architecture,
						Region:       p.region,
						LaunchTime:   sdkaws.ToTime(instance.LaunchTime),
					}, nil
				}
			}
		}
		return nil, fmt.Errorf("instance %s is not ready yet", instanceID)
	}
	var instance *cloud.BuilderInstance
	err = retry(ctx, 90, func() error {
		var waitErr error
		instance, waitErr = describe()
		return waitErr
	})
	if err != nil {
		return nil, fmt.Errorf("wait for instance: %w", err)
	}
	return instance, nil
}

func (p *Provider) TerminateInstances(ctx context.Context, region string, instanceIDs []string) error {
	if len(instanceIDs) == 0 {
		return nil
	}
	_, err := p.ec2.TerminateInstances(ctx, &ec2.TerminateInstancesInput{InstanceIds: instanceIDs})
	if err != nil {
		return fmt.Errorf("terminate instances: %w", err)
	}
	return nil
}

func (p *Provider) ListOrphanedInstances(ctx context.Context, region string) ([]cloud.OrphanedInstance, error) {
	out, err := p.ec2.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
		Filters: []ec2types.Filter{
			{Name: sdkaws.String("tag:forja:managed"), Values: []string{"true"}},
			{Name: sdkaws.String("instance-state-name"), Values: []string{"pending", "running", "stopping", "stopped"}},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("describe instances: %w", err)
	}
	var instances []cloud.OrphanedInstance
	for _, reservation := range out.Reservations {
		for _, instance := range reservation.Instances {
			instances = append(instances, cloud.OrphanedInstance{
				ID:           sdkaws.ToString(instance.InstanceId),
				InstanceType: string(instance.InstanceType),
				State:        string(instance.State.Name),
				LaunchTime:   sdkaws.ToTime(instance.LaunchTime),
			})
		}
	}
	sort.Slice(instances, func(i, j int) bool { return instances[i].LaunchTime.Before(instances[j].LaunchTime) })
	return instances, nil
}

func (p *Provider) DestroyInfrastructure(ctx context.Context, req cloud.DestroyRequest) (*cloud.DestroyResult, error) {
	orphans, err := p.ListOrphanedInstances(ctx, req.Region)
	if err != nil {
		return nil, err
	}
	instanceIDs := make([]string, 0, len(orphans))
	for _, orphan := range orphans {
		instanceIDs = append(instanceIDs, orphan.ID)
	}
	if err := p.TerminateInstances(ctx, req.Region, instanceIDs); err != nil {
		return nil, err
	}

	if err := p.emptyBucket(ctx, req.CacheBucket); err != nil {
		return nil, err
	}
	if req.CacheBucket != "" {
		if _, err := p.s3.DeleteBucket(ctx, &s3.DeleteBucketInput{Bucket: sdkaws.String(req.CacheBucket)}); err != nil {
			return nil, fmt.Errorf("delete bucket: %w", err)
		}
	}
	for _, ltID := range req.LaunchTemplates {
		if ltID == "" {
			continue
		}
		if _, err := p.ec2.DeleteLaunchTemplate(ctx, &ec2.DeleteLaunchTemplateInput{LaunchTemplateId: sdkaws.String(ltID)}); err != nil {
			return nil, fmt.Errorf("delete launch template %s: %w", ltID, err)
		}
	}
	if req.SecurityGroupID != "" {
		if _, err := p.ec2.DeleteSecurityGroup(ctx, &ec2.DeleteSecurityGroupInput{GroupId: sdkaws.String(req.SecurityGroupID)}); err != nil {
			return nil, fmt.Errorf("delete security group: %w", err)
		}
	}
	if req.InstanceProfileName != "" {
		_, _ = p.iam.RemoveRoleFromInstanceProfile(ctx, &iam.RemoveRoleFromInstanceProfileInput{
			InstanceProfileName: sdkaws.String(req.InstanceProfileName),
			RoleName:            sdkaws.String(req.IAMRoleName),
		})
		if _, err := p.iam.DeleteInstanceProfile(ctx, &iam.DeleteInstanceProfileInput{
			InstanceProfileName: sdkaws.String(req.InstanceProfileName),
		}); err != nil {
			return nil, fmt.Errorf("delete instance profile: %w", err)
		}
	}
	if req.IAMRoleName != "" {
		_, _ = p.iam.DeleteRolePolicy(ctx, &iam.DeleteRolePolicyInput{
			RoleName:   sdkaws.String(req.IAMRoleName),
			PolicyName: sdkaws.String(inlinePolicyName),
		})
		if _, err := p.iam.DeleteRole(ctx, &iam.DeleteRoleInput{RoleName: sdkaws.String(req.IAMRoleName)}); err != nil {
			return nil, fmt.Errorf("delete role: %w", err)
		}
	}
	return &cloud.DestroyResult{TerminatedInstances: len(instanceIDs)}, nil
}

func defaultTags(name string) []ec2types.Tag {
	return []ec2types.Tag{
		{Key: sdkaws.String("Name"), Value: sdkaws.String(name)},
		{Key: sdkaws.String("forja:managed"), Value: sdkaws.String("true")},
	}
}
