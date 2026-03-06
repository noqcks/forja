package aws

import (
	"encoding/base64"
	"testing"

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
