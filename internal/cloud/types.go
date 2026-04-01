package cloud

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

const (
	InstanceTagBuildID    = "forja:build-id"
	InstanceTagBuildHash  = "forja:build-hash"
	InstanceTagArch       = "forja:arch"
	InstanceTagCertS3Path = "forja-cert-s3-path"
)

type Identity struct {
	AccountID string
	ARN       string
}

type ProvisionRequest struct {
	Region              string
	AccountID           string
	CacheBucket         string
	CacheTTLDays        int
	SelfDestructMinutes int
	Instances           map[string]string
	Registry            string
	PublishedAMI        map[string]string
}

type ProvisionResult struct {
	SecurityGroupID     string
	SecurityGroupName   string
	IAMRoleName         string
	IAMRoleARN          string
	InstanceProfileName string
	InstanceProfileARN  string
	DefaultVPCID        string
	DefaultSubnetIDs    []string
	LaunchTemplates     map[string]string
	AMI                 map[string]string
}

type UploadCertificatesRequest struct {
	Bucket     string
	BuildID    string
	CACert     []byte
	ServerCert []byte
	ServerKey  []byte
}

type LaunchBuilderRequest struct {
	Region               string
	Architecture         string
	LaunchTemplateID     string
	SubnetID             string
	InstanceTypeOverride string
	BuildID              string
	CertS3Path           string
	UserData             string
	DiskSizeGB           int32
}

type BuilderInstance struct {
	ID           string
	PublicIP     string
	InstanceType string
	Architecture string
	Name         string
	BuildID      string
	BuildHash    string
	Region       string
	LaunchTime   time.Time
}

type OrphanedInstance struct {
	ID           string
	InstanceType string
	State        string
	Name         string
	BuildID      string
	BuildHash    string
	LaunchTime   time.Time
}

type DestroyRequest struct {
	Region              string
	CacheBucket         string
	LaunchTemplates     map[string]string
	SecurityGroupID     string
	IAMRoleName         string
	InstanceProfileName string
}

type DestroyResult struct {
	TerminatedInstances int
}

func BuildSessionHash(buildID string) string {
	buildID = strings.TrimSpace(buildID)
	if buildID == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(buildID))
	return hex.EncodeToString(sum[:])[:12]
}

func BuilderInstanceName(buildID string, arch string) string {
	hash := BuildSessionHash(buildID)
	if hash == "" {
		return "forja-builder"
	}
	arch = strings.TrimSpace(strings.TrimPrefix(arch, "linux/"))
	if arch == "" {
		return fmt.Sprintf("forja-build-%s", hash)
	}
	return fmt.Sprintf("forja-build-%s-%s", hash, arch)
}
