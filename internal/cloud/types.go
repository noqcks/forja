package cloud

import "time"

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
	UserData             string
}

type BuilderInstance struct {
	ID           string
	PublicIP     string
	InstanceType string
	Architecture string
	Region       string
	LaunchTime   time.Time
}

type OrphanedInstance struct {
	ID           string
	InstanceType string
	State        string
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
