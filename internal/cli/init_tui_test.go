package cli

import "testing"

func TestNewInitModelDefaultsToLargeCustomInstances(t *testing.T) {
	t.Parallel()

	model := newInitModel()
	if model.customAMD64Input.Value() != "c7a.8xlarge" {
		t.Fatalf("unexpected default amd64 instance %q", model.customAMD64Input.Value())
	}
	if model.customARM64Input.Value() != "c7g.8xlarge" {
		t.Fatalf("unexpected default arm64 instance %q", model.customARM64Input.Value())
	}
}

func TestValidateInitAnswers(t *testing.T) {
	t.Parallel()

	valid := initAnswers{
		Region:      "us-east-1",
		AMD64AMI:    "ami-0abc",
		ARM64AMI:    "ami-0def",
		CustomAMD64: "c7a.8xlarge",
		CustomARM64: "c7g.8xlarge",
	}
	if err := validateInitAnswers(valid); err != nil {
		t.Fatalf("validateInitAnswers() unexpected error = %v", err)
	}

	if err := validateInitAnswers(initAnswers{}); err == nil {
		t.Fatal("expected region to be required")
	}

	if err := validateInitAnswers(initAnswers{
		Region:      "us-west-2",
		ARM64AMI:    "ami-0def",
		CustomAMD64: "c7a.8xlarge",
		CustomARM64: "c7g.8xlarge",
	}); err == nil {
		t.Fatal("expected amd64 AMI to be required")
	}

	if err := validateInitAnswers(initAnswers{
		Region:      "us-east-1",
		CustomAMD64: "c7a.8xlarge",
		CustomARM64: "c7g.8xlarge",
	}); err != nil {
		t.Fatalf("expected us-east-1 to use built-in AMIs, got %v", err)
	}

	if err := validateInitAnswers(initAnswers{
		Region:   "us-east-1",
		AMD64AMI: "ami-0abc",
		ARM64AMI: "ami-0def",
	}); err == nil {
		t.Fatal("expected instance types to be required")
	}
}
