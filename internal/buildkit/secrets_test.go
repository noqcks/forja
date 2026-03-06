package buildkit

import "testing"

func TestParseSecretFileSpec(t *testing.T) {
	t.Parallel()

	src, err := parseSecret("id=npmrc,src=/tmp/.npmrc")
	if err != nil {
		t.Fatalf("parseSecret() error = %v", err)
	}
	if src.ID != "npmrc" || src.FilePath != "/tmp/.npmrc" {
		t.Fatalf("unexpected secret source: %+v", src)
	}
}

func TestParseSecretEnvSpec(t *testing.T) {
	t.Parallel()

	src, err := parseSecret("type=env,id=token,env=GITHUB_TOKEN")
	if err != nil {
		t.Fatalf("parseSecret() error = %v", err)
	}
	if src.ID != "token" || src.Env != "GITHUB_TOKEN" || src.FilePath != "" {
		t.Fatalf("unexpected env secret source: %+v", src)
	}
}

func TestParseSecretRejectsUnexpectedKeys(t *testing.T) {
	t.Parallel()

	if _, err := parseSecret("id=test,wat=value"); err == nil {
		t.Fatal("expected parseSecret() to reject unexpected keys")
	}
}
