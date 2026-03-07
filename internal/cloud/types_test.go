package cloud

import "testing"

func TestBuildSessionHashIsStable(t *testing.T) {
	t.Parallel()

	first := BuildSessionHash("bld_example")
	second := BuildSessionHash("bld_example")
	if first == "" {
		t.Fatal("expected non-empty hash")
	}
	if first != second {
		t.Fatalf("expected stable hash, got %q and %q", first, second)
	}
	if len(first) != 12 {
		t.Fatalf("expected 12-char hash, got %q", first)
	}
}

func TestBuilderInstanceNameUsesBuildHash(t *testing.T) {
	t.Parallel()

	name := BuilderInstanceName("bld_example", "linux/arm64")
	if name != "forja-build-"+BuildSessionHash("bld_example")+"-arm64" {
		t.Fatalf("unexpected instance name %q", name)
	}

	prefix := BuilderInstanceName("bld_example", "")
	if prefix != "forja-build-"+BuildSessionHash("bld_example") {
		t.Fatalf("unexpected instance prefix %q", prefix)
	}
}
