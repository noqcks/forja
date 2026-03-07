package cli

import "testing"

func TestNewInitModelDefaultsRegion(t *testing.T) {
	t.Parallel()

	model := newInitModel()
	if len(model.regions) == 0 {
		t.Fatal("expected at least one region")
	}
	if model.regions[model.regionIndex] != "us-east-1" {
		t.Fatalf("unexpected default region %q", model.regions[model.regionIndex])
	}
	if got := model.visibleItems(); len(got) != 3 {
		t.Fatalf("unexpected visible item count %d", len(got))
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
		CustomAMD64: defaultAMD64Instance,
		CustomARM64: defaultARM64Instance,
	}); err == nil {
		t.Fatal("expected amd64 AMI to be required")
	}

	if err := validateInitAnswers(initAnswers{
		Region:      "us-east-1",
		CustomAMD64: defaultAMD64Instance,
		CustomARM64: defaultARM64Instance,
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

func TestAnswersFromStateUsesHiddenDefaults(t *testing.T) {
	t.Parallel()

	model := newInitModel()
	// regionIndex 0 should be us-east-1 (only region currently)

	answers := model.answersFromState()
	if answers.Registry != "" {
		t.Fatalf("unexpected registry %q", answers.Registry)
	}
	if answers.AMD64AMI != defaultAMD64AMI {
		t.Fatalf("unexpected amd64 AMI %q", answers.AMD64AMI)
	}
	if answers.ARM64AMI != defaultARM64AMI {
		t.Fatalf("unexpected arm64 AMI %q", answers.ARM64AMI)
	}
	if answers.CustomAMD64 != defaultAMD64Instance {
		t.Fatalf("unexpected amd64 instance %q", answers.CustomAMD64)
	}
	if answers.CustomARM64 != defaultARM64Instance {
		t.Fatalf("unexpected arm64 instance %q", answers.CustomARM64)
	}
}
