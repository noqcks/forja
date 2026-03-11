# Forja

Self-hosted remote Docker build machine using EC2 spot instances and BuildKit.

## Release Process

There are two types of releases:

### CLI-only release (no AMI changes)
When the change is only to the CLI binary (e.g., new flags, bug fixes in Go code), just tag and push:
```
git tag -a v0.X.0 -m "forja v0.X.0"
git push origin v0.X.0
```
This triggers `.github/workflows/release.yml` which runs goreleaser to build binaries for all platforms and create a GitHub release.

### Full release (AMI + CLI changes)
When changes affect the builder AMI (BuildKit version, userdata script, installed packages), use the prepare-release workflow:
```
gh workflow run prepare-release.yml \
  --field version=v0.X.0 \
  --field region=us-east-1 \
  --field buildkit_version=v0.28.0 \
  --field publish_public=true
```
This builds AMIs for amd64 + arm64, bakes AMI IDs into `internal/release/amis.go`, commits, tags, and then the release workflow triggers automatically.

## S3 Build Cache

Cache is namespaced by the build context directory basename (see `internal/buildkit/context.go:CacheName`). Use `--cache-name` to pin a consistent namespace across git worktrees.
