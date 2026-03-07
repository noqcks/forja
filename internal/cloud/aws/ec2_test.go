package aws

import (
	"testing"

	"github.com/noqcks/forja/internal/cloud"
)

func TestBuilderInstanceTagsUseHashedName(t *testing.T) {
	t.Parallel()

	buildID := "bld_e33857b116ae4ce198131dbdf0baf3e7"
	arch := "amd64"
	tags := builderInstanceTags(buildID, arch, "s3://bucket/builds/test/certs")

	if got := tagValue(tags, "Name"); got != "forja-build-"+cloud.BuildSessionHash(buildID)+"-"+arch {
		t.Fatalf("Name tag = %q", got)
	}
	if got := tagValue(tags, cloud.InstanceTagBuildHash); got != cloud.BuildSessionHash(buildID) {
		t.Fatalf("build hash tag = %q", got)
	}
	if got := tagValue(tags, cloud.InstanceTagBuildID); got != buildID {
		t.Fatalf("build ID tag = %q", got)
	}
}
