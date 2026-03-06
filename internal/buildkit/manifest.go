package buildkit

import (
	"context"
	"fmt"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

type ManifestSource struct {
	Platform v1.Platform
	Digest   string
	Repo     string
}

func WriteManifestList(ctx context.Context, tags []string, sources []ManifestSource) error {
	if len(tags) == 0 || len(sources) == 0 {
		return nil
	}
	var index v1.ImageIndex = empty.Index
	opts := []remote.Option{
		remote.WithContext(ctx),
		remote.WithAuthFromKeychain(authn.DefaultKeychain),
	}
	for _, source := range sources {
		ref, err := name.ParseReference(source.Repo + "@" + source.Digest)
		if err != nil {
			return fmt.Errorf("parse source digest ref: %w", err)
		}
		desc, err := remote.Get(ref, opts...)
		if err != nil {
			return fmt.Errorf("fetch remote image %s: %w", ref.Name(), err)
		}
		img, err := desc.Image()
		if err != nil {
			return fmt.Errorf("resolve remote image %s: %w", ref.Name(), err)
		}
		index = mutate.AppendManifests(index, mutate.IndexAddendum{
			Add: img,
			Descriptor: v1.Descriptor{
				Digest:    desc.Digest,
				Size:      desc.Size,
				MediaType: desc.MediaType,
				Platform:  &source.Platform,
			},
		})
	}
	for _, tag := range tags {
		ref, err := name.NewTag(tag)
		if err != nil {
			return fmt.Errorf("parse image tag %s: %w", tag, err)
		}
		if err := remote.WriteIndex(ref, index, opts...); err != nil {
			return fmt.Errorf("push manifest list %s: %w", tag, err)
		}
	}
	return nil
}
