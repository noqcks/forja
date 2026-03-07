#!/usr/bin/env bash
set -euo pipefail

template="packer/forja-builder.pkr.hcl"
architecture="both"
region="${PACKER_REGION:-us-east-1}"
buildkit_version="${PACKER_BUILDKIT_VERSION:-v0.28.0}"
publish_public="${PACKER_PUBLISH_PUBLIC:-false}"
commit_sha="${PACKER_COMMIT_SHA:-}"
do_build=false

usage() {
  cat <<'EOF'
Usage: scripts/validate-packer.sh [options]

Validates the Forja Packer template and provisioner scripts. With --build, it
also performs real AMI builds for the selected architecture.

Options:
  --architecture <amd64|arm64|both>  Architecture(s) to validate or build.
  --region <aws-region>              AWS region for packer validate/build.
  --buildkit-version <version>       BuildKit version to bake into the AMI.
  --publish-public <true|false>      Publish the built AMI publicly.
  --commit-sha <sha>                 Commit SHA tag to stamp into the AMI.
  --build                            Run packer build after validation.
  --template <path>                  Packer template path.
  --help                             Show this help text.

Examples:
  scripts/validate-packer.sh
  scripts/validate-packer.sh --architecture amd64
  scripts/validate-packer.sh --build --architecture amd64 --region us-east-1
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --architecture)
      architecture="${2:?missing value for --architecture}"
      shift 2
      ;;
    --region)
      region="${2:?missing value for --region}"
      shift 2
      ;;
    --buildkit-version)
      buildkit_version="${2:?missing value for --buildkit-version}"
      shift 2
      ;;
    --publish-public)
      publish_public="${2:?missing value for --publish-public}"
      shift 2
      ;;
    --commit-sha)
      commit_sha="${2:?missing value for --commit-sha}"
      shift 2
      ;;
    --build)
      do_build=true
      shift
      ;;
    --template)
      template="${2:?missing value for --template}"
      shift 2
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

case "${architecture}" in
  amd64|arm64)
    archs=("${architecture}")
    ;;
  both)
    archs=("amd64" "arm64")
    ;;
  *)
    echo "invalid architecture: ${architecture}" >&2
    exit 1
    ;;
esac

if [[ -z "${commit_sha}" ]]; then
  if command -v git >/dev/null 2>&1 && git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    commit_sha="$(git rev-parse --short HEAD)"
  else
    commit_sha="dev"
  fi
fi

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

is_true() {
  case "$1" in
    true|TRUE|True|1|yes|YES|Yes|on|ON|On)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

check_public_ami_prereqs() {
  local state

  if ! command -v aws >/dev/null 2>&1; then
    echo "warning: aws CLI not found; skipping AMI public-share preflight for region ${region}" >&2
    echo "         if this account blocks public AMI sharing, packer will fail during ModifyImageAttribute" >&2
    return 0
  fi

  if ! state="$(aws ec2 get-image-block-public-access-state \
    --region "${region}" \
    --query 'ImageBlockPublicAccessState' \
    --output text 2>/dev/null)"; then
    echo "warning: unable to read AMI block public access state for region ${region}; continuing" >&2
    echo "         public AMI builds require ec2:GetImageBlockPublicAccessState or the build may fail late" >&2
    return 0
  fi

  if [[ "${state}" == "block-new-sharing" ]]; then
    cat >&2 <<EOF
error: this AWS account blocks new public AMI sharing in ${region}

The current build requested --publish-public=true, which makes Packer call
ModifyImageAttribute with ami_groups=["all"]. AWS rejects that while AMI block
public access is enabled for the account, so the build would fail after the AMI
is created.

Choose one:
  1. Build a private AMI instead:
     scripts/validate-packer.sh --build --publish-public false ...
  2. Disable the account-level block before publishing publicly:
     aws ec2 disable-image-block-public-access --region ${region}

To confirm the current setting manually:
  aws ec2 get-image-block-public-access-state --region ${region}
EOF
    exit 1
  fi
}

require_cmd packer
require_cmd bash

echo "==> Checking provisioner scripts"
for script in packer/scripts/base.sh packer/scripts/optimize-boot.sh packer/scripts/cleanup.sh; do
  bash -n "${script}"
done

echo "==> Checking packer formatting"
packer fmt -check packer

echo "==> Initializing packer plugins"
packer init "${template}"

echo "==> Inspecting template"
packer inspect "${template}" >/dev/null

echo "==> Validating template"
for arch in "${archs[@]}"; do
  echo "  -> ${arch}"
  packer validate \
    -var "region=${region}" \
    -var "architecture=${arch}" \
    -var "buildkit_version=${buildkit_version}" \
    "${template}"
done

if [[ "${do_build}" != "true" ]]; then
  echo "==> Validation complete"
  exit 0
fi

if is_true "${publish_public}"; then
  echo "==> Checking public AMI sharing prerequisites"
  check_public_ami_prereqs
fi

echo "==> Running AMI build(s)"
for arch in "${archs[@]}"; do
  manifest_path="packer/manifest-${arch}-${region}.json"
  rm -f packer/manifest.json "${manifest_path}"

  echo "  -> build ${arch}"
  packer build \
    -var "region=${region}" \
    -var "architecture=${arch}" \
    -var "publish_public=${publish_public}" \
    -var "buildkit_version=${buildkit_version}" \
    -var "commit_sha=${commit_sha}" \
    "${template}"

  if [[ -f packer/manifest.json ]]; then
    cp packer/manifest.json "${manifest_path}"
    echo "     manifest: ${manifest_path}"
    if command -v jq >/dev/null 2>&1; then
      jq -r '.builds[-1].artifact_id' "${manifest_path}"
    fi
  fi
done

echo "==> Build validation complete"
