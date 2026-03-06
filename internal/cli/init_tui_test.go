package cli

import "testing"

func TestValidateInitAnswers(t *testing.T) {
	t.Parallel()

	valid := initAnswers{
		Region:     "us-east-1",
		SizeChoice: initSizeSmall,
	}
	if err := validateInitAnswers(valid); err != nil {
		t.Fatalf("validateInitAnswers() unexpected error = %v", err)
	}

	if err := validateInitAnswers(initAnswers{
		Region:     "us-east-1",
		SizeChoice: initSizeCustom,
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
