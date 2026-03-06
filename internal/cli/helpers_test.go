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

func TestPlatformHelpersAndUserData(t *testing.T) {
	t.Parallel()

	if arch, err := platformArch("linux/arm64"); err != nil || arch != "arm64" {
		t.Fatalf("platformArch() = %q, %v", arch, err)
	}
	if _, err := platformDescriptor("linux/ppc64le"); err == nil {
		t.Fatal("expected unsupported platform error")
	}

	userData := renderUserData("s3://bucket/builds/x/certs", "bucket", "us-east-1", 60)
	for _, want := range []string{
		`CERT_S3_PATH="s3://bucket/builds/x/certs"`,
		`CACHE_REGION="us-east-1"`,
		`SELF_DESTRUCT_MINUTES=60`,
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
