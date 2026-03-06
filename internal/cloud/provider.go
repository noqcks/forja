package cloud

import "context"

type Provider interface {
	Name() string
	Identity(ctx context.Context) (*Identity, error)
	EnsureInfrastructure(ctx context.Context, req ProvisionRequest) (*ProvisionResult, error)
	UploadCertificates(ctx context.Context, req UploadCertificatesRequest) (string, error)
	DeleteCertificates(ctx context.Context, bucket string, buildID string) error
	LaunchBuilder(ctx context.Context, req LaunchBuilderRequest) (*BuilderInstance, error)
	TerminateInstances(ctx context.Context, region string, instanceIDs []string) error
	ListOrphanedInstances(ctx context.Context, region string) ([]OrphanedInstance, error)
	DestroyInfrastructure(ctx context.Context, req DestroyRequest) (*DestroyResult, error)
	InstancePrice(ctx context.Context, region string, instanceType string) (float64, error)
}
