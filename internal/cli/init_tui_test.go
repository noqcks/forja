package cli

import "testing"

func TestValidateInitAnswers(t *testing.T) {
	t.Parallel()

	valid := initAnswers{
		Region:     "us-east-1",
		SizeChoice: initSizeSmall,
		AMD64AMI:   "ami-0abc",
		ARM64AMI:   "ami-0def",
	}
	if err := validateInitAnswers(valid); err != nil {
		t.Fatalf("validateInitAnswers() unexpected error = %v", err)
	}

	if err := validateInitAnswers(initAnswers{
		SizeChoice: initSizeSmall,
	}); err == nil {
		t.Fatal("expected region to be required")
	}

	if err := validateInitAnswers(initAnswers{
		Region:     "us-east-1",
		SizeChoice: initSizeSmall,
		ARM64AMI:   "ami-0def",
	}); err == nil {
		t.Fatal("expected amd64 AMI to be required")
	}

	if err := validateInitAnswers(initAnswers{
		Region:     "us-east-1",
		SizeChoice: initSizeCustom,
		AMD64AMI:   "ami-0abc",
		ARM64AMI:   "ami-0def",
	}); err == nil {
		t.Fatal("expected custom instance types to be required")
	}
}

func TestInstanceTypesForSizeChoice(t *testing.T) {
	t.Parallel()

	instances, err := instanceTypesForSizeChoice(initSizeMedium, "", "")
	if err != nil {
		t.Fatalf("instanceTypesForSizeChoice() unexpected error = %v", err)
	}
	if instances["amd64"] != "c7a.xlarge" || instances["arm64"] != "c7g.xlarge" {
		t.Fatalf("instanceTypesForSizeChoice() = %#v", instances)
	}

	custom, err := instanceTypesForSizeChoice(initSizeCustom, "m7a.large", "c7g.large")
	if err != nil {
		t.Fatalf("instanceTypesForSizeChoice() custom unexpected error = %v", err)
	}
	if custom["amd64"] != "m7a.large" || custom["arm64"] != "c7g.large" {
		t.Fatalf("instanceTypesForSizeChoice() custom = %#v", custom)
	}

	if _, err := instanceTypesForSizeChoice("unknown", "", ""); err == nil {
		t.Fatal("expected unsupported size choice to fail")
	}
}

func TestNormalizeSizeChoice(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"":        initSizeSmall,
		"small":   initSizeSmall,
		"medium":  initSizeMedium,
		"large":   initSizeLarge,
		"custom":  initSizeCustom,
		" Large ": initSizeLarge,
	}

	for input, want := range cases {
		if got := normalizeSizeChoice(input); got != want {
			t.Fatalf("normalizeSizeChoice(%q) = %q, want %q", input, got, want)
		}
	}

	if got := normalizeSizeChoice("weird"); got != "weird" {
		t.Fatalf("normalizeSizeChoice() should preserve unknown values, got %q", got)
	}
}
