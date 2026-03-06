package release

import "testing"

func TestAWSAMI(t *testing.T) {
	t.Parallel()

	if got := AWSAMI("us-east-1", "amd64"); got == "" {
		t.Fatal("expected amd64 AMI for us-east-1")
	}
	if got := AWSAMI("us-east-1", "arm64"); got == "" {
		t.Fatal("expected arm64 AMI for us-east-1")
	}
	if got := AWSAMI("us-west-2", "amd64"); got != "" {
		t.Fatalf("expected empty AMI for unknown region, got %q", got)
	}
}

func TestAWSAMIsForRegionCopiesMap(t *testing.T) {
	t.Parallel()

	amis, ok := AWSAMIsForRegion("us-east-1")
	if !ok {
		t.Fatal("expected AMIs for us-east-1")
	}
	amis["amd64"] = "ami-overwritten"

	refetched, ok := AWSAMIsForRegion("us-east-1")
	if !ok {
		t.Fatal("expected AMIs for us-east-1 on refetch")
	}
	if refetched["amd64"] == "ami-overwritten" {
		t.Fatal("expected returned AMI map to be a copy")
	}
}
