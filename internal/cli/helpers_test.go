package cli

import (
	"strings"
	"testing"
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
