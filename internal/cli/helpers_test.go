package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/noqcks/forja/internal/config"
)

func TestPlatformListUsesDefaultAndTrims(t *testing.T) {
	t.Parallel()

	got := platformList(" linux/amd64 , linux/arm64 ", "linux/amd64")
	if len(got) != 2 || got[0] != "linux/amd64" || got[1] != "linux/arm64" {
		t.Fatalf("platformList() = %#v", got)
	}

	fallback := platformList("", "linux/amd64")
	if len(fallback) != 1 || fallback[0] != "linux/amd64" {
		t.Fatalf("platformList() fallback = %#v", fallback)
	}
}

func TestRepositoryHelpersValidateMultiArchTagRules(t *testing.T) {
	t.Parallel()

	repo, err := repositoryOfTag("ghcr.io/noqcks/forja:latest")
	if err != nil {
		t.Fatalf("repositoryOfTag() error = %v", err)
	}
	if repo != "ghcr.io/noqcks/forja" {
		t.Fatalf("repositoryOfTag() = %q", repo)
	}

	if _, err := ensureSameRepository([]string{
		"ghcr.io/noqcks/forja:latest",
		"ghcr.io/noqcks/forja:v1",
	}); err != nil {
		t.Fatalf("ensureSameRepository() unexpected error = %v", err)
	}

	if _, err := ensureSameRepository([]string{
		"ghcr.io/noqcks/forja:latest",
		"ghcr.io/noqcks/other:v1",
	}); err == nil {
		t.Fatal("expected ensureSameRepository() to reject mixed repos")
	}
}

func TestQualifyTags(t *testing.T) {
	t.Parallel()

	ecr := "123456789012.dkr.ecr.us-east-1.amazonaws.com"

	tests := []struct {
		name     string
		tags     []string
		registry string
		want     []string
	}{
		{"empty registry is no-op", []string{"myapp:latest"}, "", []string{"myapp:latest"}},
		{"shorthand tag gets prefixed", []string{"myapp:latest"}, ecr, []string{ecr + "/myapp:latest"}},
		{"fully qualified tag unchanged", []string{ecr + "/myapp:latest"}, ecr, []string{ecr + "/myapp:latest"}},
		{"ghcr tag unchanged", []string{"ghcr.io/org/app:v1"}, ecr, []string{"ghcr.io/org/app:v1"}},
		{"localhost:5000 tag unchanged", []string{"localhost:5000/app:v1"}, ecr, []string{"localhost:5000/app:v1"}},
		{"mixed tags", []string{"myapp:v1", ecr + "/other:v2"}, ecr, []string{ecr + "/myapp:v1", ecr + "/other:v2"}},
		{"trailing slash on registry", []string{"myapp:v1"}, ecr + "/", []string{ecr + "/myapp:v1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := qualifyTags(tt.tags, tt.registry)
			if len(got) != len(tt.want) {
				t.Fatalf("qualifyTags() = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("qualifyTags()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestPlatformHelpersAndUserData(t *testing.T) {
	t.Parallel()

	if arch, err := platformArch("linux/arm64"); err != nil || arch != "arm64" {
		t.Fatalf("platformArch() = %q, %v", arch, err)
	}
	if _, err := platformDescriptor("linux/ppc64le"); err == nil {
		t.Fatal("expected unsupported platform error")
	}

	userData := renderUserData("bucket", "us-east-1", 60)
	for _, want := range []string{
		`CACHE_REGION="us-east-1"`,
		`SELF_DESTRUCT_MINUTES=60`,
		`latest/meta-data/tags/instance/forja-cert-s3-path`,
		`aws s3 cp "${CERT_S3_PATH}/server-cert.pem"`,
		"/etc/systemd/system/buildkitd.service.d/override.conf",
		"/usr/local/bin/forja-self-destruct.sh",
		"/etc/systemd/system/forja-self-destruct.timer",
		"OnBootSec=60m",
		"systemctl enable --now forja-self-destruct.timer",
	} {
		if !strings.Contains(userData, want) {
			t.Fatalf("user data missing %q\n%s", want, userData)
		}
	}
}

func TestLoadCommandConfigFormatsFriendlyErrors(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if _, err := loadCommandConfig(true); err == nil || !strings.Contains(err.Error(), "run `forja init` first") {
		t.Fatalf("expected missing-config hint, got %v", err)
	}

	configDir := filepath.Join(home, ".forja")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte("not: [valid\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if _, err := loadCommandConfig(true); err == nil || !strings.Contains(err.Error(), "invalid forja config at") {
		t.Fatalf("expected parse-config hint, got %v", err)
	}

	cfg := config.Default()
	if err := config.Save(cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if _, err := loadCommandConfig(true); err == nil || !strings.Contains(err.Error(), "run `forja init` to repair it") {
		t.Fatalf("expected validation hint, got %v", err)
	}
}

func TestValidateBuildOptionsRejectsBadRequestsBeforeAWS(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.Region = "us-east-1"
	cfg.CacheBucket = "forja-cache-test"
	cfg.Resources.DefaultSubnetIDs = []string{"subnet-123"}
	cfg.Resources.LaunchTemplates["amd64"] = "lt-amd64"
	cfg.Resources.LaunchTemplates["arm64"] = "lt-arm64"

	if _, _, err := validateBuildOptions(cfg, &buildOptions{}, []string{"linux/s390x"}); err == nil {
		t.Fatal("expected unsupported platform to fail early")
	}

	missingSubnet := *cfg
	missingSubnet.Resources = cfg.Resources
	missingSubnet.Resources.DefaultSubnetIDs = nil
	if _, _, err := validateBuildOptions(&missingSubnet, &buildOptions{}, []string{"linux/amd64"}); err == nil || !strings.Contains(err.Error(), "no default subnet") {
		t.Fatalf("expected missing subnet error, got %v", err)
	}

	missingTemplate := *cfg
	missingTemplate.Resources = cfg.Resources
	missingTemplate.Resources.LaunchTemplates = map[string]string{"amd64": "lt-amd64"}
	if _, _, err := validateBuildOptions(&missingTemplate, &buildOptions{}, []string{"linux/arm64"}); err == nil || !strings.Contains(err.Error(), "launch template for arm64") {
		t.Fatalf("expected missing launch template error, got %v", err)
	}

	if _, _, err := validateBuildOptions(cfg, &buildOptions{load: true}, []string{"linux/amd64", "linux/arm64"}); err == nil || !strings.Contains(err.Error(), "--load is only supported") {
		t.Fatalf("expected multi-platform load rejection, got %v", err)
	}

	repo, subnetID, err := validateBuildOptions(cfg, &buildOptions{
		push: true,
		tags: []string{"ghcr.io/noqcks/forja:latest", "ghcr.io/noqcks/forja:v1"},
	}, []string{"linux/amd64", "linux/arm64"})
	if err != nil {
		t.Fatalf("validateBuildOptions() unexpected error = %v", err)
	}
	if repo != "ghcr.io/noqcks/forja" {
		t.Fatalf("repo = %q", repo)
	}
	if subnetID != "subnet-123" {
		t.Fatalf("subnetID = %q", subnetID)
	}
}

func TestFormatAWSIdentityError(t *testing.T) {
	t.Parallel()

	missing := formatAWSIdentityError(errors.New("failed to refresh cached credentials, no EC2 IMDS role found"), "")
	if missing == nil || !strings.Contains(missing.Error(), "no AWS credentials available") {
		t.Fatalf("expected missing credential hint, got %v", missing)
	}

	profileMissing := formatAWSIdentityError(errors.New("missing credentials"), "prod")
	if profileMissing == nil || !strings.Contains(profileMissing.Error(), `profile "prod"`) {
		t.Fatalf("expected profile-specific hint, got %v", profileMissing)
	}

	denied := formatAWSIdentityError(errors.New("AccessDenied: not authorized"), "")
	if denied == nil || !strings.Contains(denied.Error(), "do not have the required permissions") {
		t.Fatalf("expected access denied hint, got %v", denied)
	}
}

func TestFormatS3BucketAccessError(t *testing.T) {
	t.Parallel()

	denied := formatS3BucketAccessError(errors.New("AccessDenied: forbidden"), "forja-cache-demo")
	if denied == nil || !strings.Contains(denied.Error(), `S3 bucket "forja-cache-demo"`) {
		t.Fatalf("expected bucket-specific access denied message, got %v", denied)
	}

	other := formatS3BucketAccessError(errors.New("timeout"), "forja-cache-demo")
	if other == nil || !strings.Contains(other.Error(), "prepare build session data") {
		t.Fatalf("expected generic bucket preparation message, got %v", other)
	}
}
