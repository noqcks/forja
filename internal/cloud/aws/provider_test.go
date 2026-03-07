package aws

import (
	"encoding/base64"
	"testing"

	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/noqcks/forja/internal/cloud"
)

var _ cloud.Provider = (*Provider)(nil)

func TestEncodeUserDataBase64EncodesInput(t *testing.T) {
	t.Parallel()

	raw := "#!/bin/bash\necho hi\n"
	encoded := encodeUserData(raw)
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("DecodeString() error = %v", err)
	}
	if string(decoded) != raw {
		t.Fatalf("decoded user data = %q, want %q", string(decoded), raw)
	}
}

func TestForjaArchitecturesMapsSupportedArchitectures(t *testing.T) {
	t.Parallel()

	got := forjaArchitectures(&ec2types.ProcessorInfo{
		SupportedArchitectures: []ec2types.ArchitectureType{"x86_64", "arm64", "x86_64"},
	})
	if len(got) != 2 || got[0] != "amd64" || got[1] != "arm64" {
		t.Fatalf("forjaArchitectures() = %#v", got)
	}

	unsupported := forjaArchitectures(&ec2types.ProcessorInfo{
		SupportedArchitectures: []ec2types.ArchitectureType{"x86_64_mac"},
	})
	if len(unsupported) != 0 {
		t.Fatalf("expected unsupported architectures to be ignored, got %#v", unsupported)
	}
}
