package buildkit

import (
	"fmt"
	"strings"

	"github.com/moby/buildkit/session/secrets/secretsprovider"
	"github.com/tonistiigi/go-csvvalue"
)

func parseSecret(raw string) (*secretsprovider.Source, error) {
	fields, err := csvvalue.Fields(raw, nil)
	if err != nil {
		return nil, fmt.Errorf("parse secret %q: %w", raw, err)
	}
	var src secretsprovider.Source
	var secretType string
	for _, field := range fields {
		key, value, ok := strings.Cut(field, "=")
		if !ok {
			return nil, fmt.Errorf("invalid secret field %q", field)
		}
		switch strings.ToLower(key) {
		case "type":
			secretType = value
		case "id":
			src.ID = value
		case "src", "source":
			src.FilePath = value
		case "env":
			src.Env = value
		default:
			return nil, fmt.Errorf("unexpected secret key %q", key)
		}
	}
	if secretType == "env" && src.Env == "" {
		src.Env = src.FilePath
		src.FilePath = ""
	}
	return &src, nil
}
