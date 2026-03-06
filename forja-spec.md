# Forja: Self-Hosted Remote Docker Build Machine

## Spec v1.1 | Status: Draft

**Owner:** Benji
**Date:** 2026-03-06

---

## 1. Summary

Forja is an open-source Go CLI that gives you a personal, on-demand Docker build machine on AWS EC2. It works like [Depot](https://depot.dev) but runs entirely in your own AWS account with no SaaS dependency.

The v1 implementation uses AWS as the first provider, but the codebase is intentionally structured behind a provider abstraction so additional backends such as Hetzner can be added later without rewriting the CLI or build orchestration layers.

The core loop:
1. `forja init` provisions AWS infrastructure (EC2 launch template, S3 cache bucket, IAM role, security group).
2. `forja build .` launches an ephemeral EC2 instance running BuildKit, sends build context over gRPC with mTLS, builds the image remotely, and terminates the instance.
3. Build cache persists in S3 across builds. You pay only for compute time + S3 storage.

Forja displays the cost of each build when it completes.

---

## 2. Goals

- Build Docker images remotely on EC2, on demand.
- Near-zero idle cost (S3 cache storage only when no builds are running).
- No SSH. No persistent servers. No SaaS dependency.
- Drop-in replacement for `docker buildx build` (Depot-style CLI interface).
- Transparent multi-arch builds (ARM + x86) via parallel native instances.
- Open-source and usable by anyone with an AWS account.

## 3. Non-Goals

- Multi-tenant hosted build platform.
- Kubernetes-native build orchestration.
- Horizontal autoscaling / build queuing in v1.
- Replacing CI systems broadly.
- Spot instance support in v1.

---

## 4. Architecture

### 4.1 Components

```
+------------------+         mTLS/gRPC          +-------------------------+
|                  | -------------------------> |   EC2 Instance          |
|   forja CLI      |    (BuildKit protocol)     |   - buildkitd           |
|   (your laptop)  |                            |   - 60-min self-destruct|
|                  | <------------------------- |   - cloud-init bootstrap|
+------------------+    build result / logs     +-------------------------+
        |                                                |
        | AWS SDK                                        | cloud-init
        v                                                v
+------------------+                            +------------------+
| AWS APIs         |                            | S3 Cache Bucket  |
| - EC2            |                            | - BuildKit cache |
| - S3             |                            | - Cert delivery  |
| - ECR (push)     |                            +------------------+
| - Pricing API    |
+------------------+
```

### 4.2 Build Flow

1. **CLI generates ephemeral mTLS certificates** (CA + server cert + client cert) for this build session. Certs are never reused across builds.

2. **CLI uploads server certs to S3** at a known path: `s3://<bucket>/builds/<build-id>/certs/`.

3. **CLI launches EC2 instance** from a launch template with user data that contains:
   - The S3 cert path for this build
   - Region and cache bucket name
   - Instance self-destruct timeout (default: 60 min)

4. **Instance boots (target: 3-5 seconds)** using an optimized minimal AMI:
   - cloud-init pulls server certs from S3
   - Starts `buildkitd` with mTLS configuration
   - Starts a self-destruct systemd timer (terminates instance after 60 min idle)

5. **CLI polls instance until BuildKit port is reachable** via public IP + mTLS handshake.

6. **CLI establishes a pure-Go BuildKit client session** pointing at `<public-ip>:8372` with the client mTLS certs.

7. **Build executes over the BuildKit gRPC protocol from Go.** Context is streamed from the local machine to remote BuildKit without shelling out to `docker buildx`. BuildKit handles layer caching with S3 backend (`--cache-to type=s3` / `--cache-from type=s3`).

8. **Build output** is controlled by flags:
   - Default: cache only (image stays in BuildKit/S3 cache)
   - `--push`: push to a registry (ECR, Docker Hub, GHCR, etc.)
   - `--load`: pull a single-platform image back to the local Docker daemon

9. **CLI terminates the instance** and deletes ephemeral certs from S3.

10. **CLI displays build cost**: `instance_runtime_seconds * hourly_price / 3600`.

### 4.3 Multi-Architecture Builds

When `--platform linux/amd64,linux/arm64` is specified:

1. CLI launches two instances in parallel:
   - Graviton (e.g., `c7g.xlarge`) for `linux/arm64`
   - Intel/AMD (e.g., `c7a.xlarge`) for `linux/amd64`
2. Each instance builds its native architecture.
3. CLI creates and pushes a multi-arch manifest list combining both images.
4. Both instances are terminated.
5. Cost display shows combined cost of both instances.

### 4.4 Failure Handling & Cleanup

**Aggressive cleanup with safety net:**

- CLI registers signal handlers for SIGINT, SIGTERM. On any exit (success, failure, Ctrl+C), it terminates all instances launched for this build.
- If the CLI process is killed hard (SIGKILL, power loss, network failure), the **instance self-destructs after 60 minutes** via a systemd timer running on the instance itself.
- `forja cleanup` command finds and terminates any orphaned forja instances (identified by tags) as a manual safety valve.

**Self-destruct mechanism on instance:**
```
# /etc/systemd/system/forja-self-destruct.timer
# Runs 60 minutes after boot
# Checks if buildkitd has been idle, then calls:
#   aws ec2 terminate-instances --instance-ids $(curl -s http://169.254.169.254/latest/meta-data/instance-id)
```

The instance's IAM role includes `ec2:TerminateInstances` scoped to its own instance ID.

---

## 5. AWS Resources

All resources are created by `forja init` and removed by `forja destroy`.

### 5.1 Resources Created

| Resource | Purpose | Cost When Idle |
|----------|---------|----------------|
| S3 bucket: `forja-cache-<account-id>-<region>` | BuildKit cache + ephemeral cert delivery | ~$0.023/GB/month |
| IAM role: `forja-builder` | Instance permissions (S3, ECR, self-terminate) | Free |
| IAM instance profile: `forja-builder` | Attach role to EC2 | Free |
| Security group: `forja-builder` | Allow inbound on port 8372 (BuildKit) from anywhere, secured by mTLS | Free |
| EC2 launch template: `forja-builder-<arch>` | One per architecture (arm64, amd64) | Free |
| S3 lifecycle rule | Auto-expire cache objects after 14 days | N/A |

**Idle cost: effectively $0** (only S3 cache storage, which is cents per GB).

### 5.2 IAM Role Policy

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "S3CacheAndCerts",
      "Effect": "Allow",
      "Action": ["s3:GetObject", "s3:PutObject", "s3:DeleteObject", "s3:ListBucket"],
      "Resource": [
        "arn:aws:s3:::forja-cache-<account-id>-<region>",
        "arn:aws:s3:::forja-cache-<account-id>-<region>/*"
      ]
    },
    {
      "Sid": "ECRPush",
      "Effect": "Allow",
      "Action": [
        "ecr:GetAuthorizationToken",
        "ecr:BatchCheckLayerAvailability",
        "ecr:PutImage",
        "ecr:InitiateLayerUpload",
        "ecr:UploadLayerPart",
        "ecr:CompleteLayerUpload"
      ],
      "Resource": "*"
    },
    {
      "Sid": "SelfTerminate",
      "Effect": "Allow",
      "Action": "ec2:TerminateInstances",
      "Resource": "*",
      "Condition": {
        "StringEquals": {
          "ec2:ResourceTag/forja:managed": "true"
        }
      }
    }
  ]
}
```

### 5.3 Security Group

- **Inbound:** TCP 8372 from `0.0.0.0/0` (BuildKit gRPC, secured by mTLS — connections without valid client cert are rejected)
- **Outbound:** All (needed for S3, ECR, package repos)

### 5.4 CLI User IAM Permissions

The user running `forja` needs these permissions (can be scoped by tags/resource ARNs):

```
ec2:RunInstances
ec2:TerminateInstances
ec2:DescribeInstances
ec2:DescribeInstanceStatus
ec2:CreateLaunchTemplate
ec2:DeleteLaunchTemplate
ec2:CreateSecurityGroup
ec2:DeleteSecurityGroup
ec2:AuthorizeSecurityGroupIngress
ec2:RevokeSecurityGroupIngress
ec2:CreateTags
s3:CreateBucket
s3:PutBucketLifecycleConfiguration
s3:PutObject
s3:GetObject
s3:DeleteObject
s3:DeleteBucket
s3:ListBucket
iam:CreateRole
iam:DeleteRole
iam:PutRolePolicy
iam:DeleteRolePolicy
iam:CreateInstanceProfile
iam:DeleteInstanceProfile
iam:AddRoleToInstanceProfile
iam:RemoveRoleFromInstanceProfile
iam:PassRole
pricing:GetProducts  (for cost calculation)
```

---

## 6. Optimized AMI

### 6.1 What's In It

The AMI is a minimal Linux image optimized for fast boot:

- **Base:** Ubuntu 24.04 minimal
- **Installed:** buildkitd, AWS CLI v2, systemd
- **Removed/disabled:** snapd, unattended apt timers, AppArmor, and non-essential boot-time services that add latency to ephemeral builder startup
- **Optimized:** `noatime` mounts, `fsck.mode=skip`, EC2-only cloud-init datasource config, duplicate address detection disabled for faster networking
- **Included:** forja cloud-init script that handles cert pull + buildkitd start + self-destruct timer

**Target boot time:** 3-5 seconds from RunInstances to buildkitd accepting connections.

### 6.2 AMI Distribution

- Packer definitions live in the `forja` repo under `packer/`.
- Maintainer AMIs are built by GitHub Actions using Packer, not by the end-user CLI.
- Pre-built public AMIs are published by the maintainer in 5 regions:
  - `us-east-1`, `us-west-2`, `eu-west-1`, `eu-central-1`, `ap-southeast-1`
- Both `arm64` and `amd64` AMIs are published.
- v1 expects AMI IDs to be supplied through configuration or release metadata. It does not build AMIs in the user's account.

### 6.3 AMI Boot Sequence

```
kernel boot (~1s)
  -> systemd minimal target (~1s)
    -> cloud-init pulls certs from S3 (~0.5s)
      -> buildkitd starts with mTLS (~0.5s)
        -> self-destruct timer armed (60 min)
```

### 6.4 Cloud-Init User Data

The CLI passes this as EC2 user data at launch:

```bash
#!/bin/bash
set -euo pipefail

CERT_S3_PATH="s3://forja-cache-123456789012-us-east-1/builds/bld_abc123/certs"
CACHE_BUCKET="forja-cache-123456789012-us-east-1"
CACHE_REGION="us-east-1"
SELF_DESTRUCT_MINUTES=60

# Pull certs
aws s3 cp "${CERT_S3_PATH}/server-cert.pem" /etc/buildkit/certs/server-cert.pem
aws s3 cp "${CERT_S3_PATH}/server-key.pem" /etc/buildkit/certs/server-key.pem
aws s3 cp "${CERT_S3_PATH}/ca-cert.pem" /etc/buildkit/certs/ca-cert.pem

# Start buildkitd with mTLS and S3 cache
buildkitd \
  --addr tcp://0.0.0.0:8372 \
  --tlscacert /etc/buildkit/certs/ca-cert.pem \
  --tlscert /etc/buildkit/certs/server-cert.pem \
  --tlskey /etc/buildkit/certs/server-key.pem &

# Arm self-destruct
(sleep $((SELF_DESTRUCT_MINUTES * 60)) && \
  INSTANCE_ID=$(curl -s http://169.254.169.254/latest/meta-data/instance-id) && \
  aws ec2 terminate-instances --instance-ids "$INSTANCE_ID" --region "$CACHE_REGION") &
```

---

## 7. S3 Build Cache

### 7.1 How It Works

BuildKit natively supports S3 as a cache backend:

```
--cache-to type=s3,region=us-east-1,bucket=forja-cache-123456789012-us-east-1,name=myproject
--cache-from type=s3,region=us-east-1,bucket=forja-cache-123456789012-us-east-1,name=myproject
```

Cache keys are content-addressed (based on Dockerfile instructions and file hashes). Different repos/projects naturally get different cache entries without explicit namespacing.

### 7.2 Lifecycle Policy

S3 bucket has a lifecycle rule to expire objects after 14 days. This prevents unbounded cache growth. Users can adjust the TTL in config.

### 7.3 Cache Cost Estimate

Typical Docker project cache: 1-5 GB. At S3 Standard pricing:
- 5 GB cache = ~$0.12/month

---

## 8. mTLS Certificate Management

### 8.1 Per-Session Ephemeral Certificates

Every build session generates a fresh certificate chain:

1. **CLI generates** an ephemeral CA (self-signed, RSA 2048 or Ed25519)
2. **CLI generates** a server certificate signed by the CA (for buildkitd)
3. **CLI generates** a client certificate signed by the CA (for buildx connection)
4. Server cert + key + CA cert are uploaded to S3 for the instance to pull
5. Client cert + key + CA cert are kept in memory on the CLI side
6. After build completes, all certs are deleted from S3

No persistent CA. No cert rotation needed. No cert storage. Each build is cryptographically isolated.

### 8.2 Security Properties

- Connections without a valid client cert (signed by the session CA) are rejected by buildkitd
- The CA only exists for the duration of one build
- Even if an attacker intercepts the public IP, they cannot connect without the ephemeral client cert
- Certs in S3 are encrypted at rest (SSE-S3) and accessible only via the instance's IAM role

---

## 9. CLI Specification

### 9.1 Installation

```bash
# Install script (Linux + macOS)
curl -sfL https://get.forja.dev | sh

# Or download from GitHub Releases
```

Published via [goreleaser](https://goreleaser.com/) on GitHub Releases. Binaries for:
- `darwin/arm64` (macOS Apple Silicon)
- `darwin/amd64` (macOS Intel)
- `linux/amd64`
- `linux/arm64`

### 9.2 Commands

```
forja init              Interactive wizard to provision AWS resources
forja build [context]   Build a Docker image remotely
forja cleanup           Find and terminate orphaned forja instances
forja destroy           Tear down all AWS resources created by forja init
forja version           Print version
```

### 9.3 `forja init`

Interactive wizard flow:

```
$ forja init

Welcome to Forja! Let's set up your remote build environment.

AWS credentials detected (profile: default, account: 123456789012)

? AWS region: us-east-1
? Default platform: linux/amd64
? Instance size for builds:
  > Small  (2 vCPU, 4 GB  — c7a.large / c7g.large)   ~$0.07/hr
    Medium (4 vCPU, 8 GB  — c7a.xlarge / c7g.xlarge)  ~$0.14/hr
    Large  (8 vCPU, 16 GB — c7a.2xlarge / c7g.2xlarge) ~$0.28/hr
    Custom (specify instance type)
? Default registry (for --push): 123456789012.dkr.ecr.us-east-1.amazonaws.com

Creating AWS resources...
  [ok] S3 bucket: forja-cache-123456789012-us-east-1
  [ok] IAM role: forja-builder
  [ok] Security group: sg-0abc123 (forja-builder)
  [ok] Launch template: lt-arm64-abc123 (c7g.large)
  [ok] Launch template: lt-amd64-def456 (c7a.large)

Config written to ~/.forja/config.yaml

Ready! Try: forja build .
```

**What it creates:**
- S3 bucket with lifecycle rules
- IAM role + instance profile
- Security group
- Launch templates (one per architecture)
- Config file at `~/.forja/config.yaml`

**Network assumption:**
- v1 uses the account's default VPC and default subnets. If no default VPC exists, `forja init` exits with a clear remediation error.

### 9.4 `forja build`

Interface mirrors [Depot CLI](https://depot.dev/docs/cli/reference#depot-build):

```
forja build [context] [flags]

Flags:
  -f, --file string          Path to Dockerfile (default: Dockerfile)
  -t, --tag strings          Image tag(s) (e.g., myapp:latest)
      --platform strings     Target platform(s) (e.g., linux/amd64,linux/arm64)
      --push                 Push image to registry after build
      --load                 Load image into local Docker daemon
      --build-arg strings    Build-time variables (KEY=VALUE)
      --target string        Build target stage
      --secret strings       Build secrets (id=mysecret,src=./secret.txt)
      --no-cache             Do not use cache
      --progress string      Progress output type (auto, plain, tty)
      --instance-type string Override instance type for this build
      --profile string       AWS profile to use
```

**Example usage:**

```bash
# Simple build, push to ECR
forja build -t 123456789012.dkr.ecr.us-east-1.amazonaws.com/myapp:latest --push .

# Multi-arch build
forja build --platform linux/amd64,linux/arm64 -t myapp:latest --push .

# Build with secrets
forja build --secret id=npmrc,src=$HOME/.npmrc -t myapp:latest .

# Override instance type for a heavy build
forja build --instance-type c7a.2xlarge -t myapp:latest --push .
```

**Output:**

```
$ forja build -t myapp:latest --push .

Launching builder (c7a.large, us-east-1)... ready (3.2s)
[+] Building 42.1s (14/14) FINISHED
 => [internal] load build definition from Dockerfile                    0.1s
 => [internal] load .dockerignore                                       0.0s
 => [internal] load metadata for docker.io/library/python:3.12-slim     0.3s
 => [1/10] FROM docker.io/library/python:3.12-slim@sha256:abc...        0.0s
 => CACHED [2/10] RUN apt-get update && apt-get install -y ...          0.0s
 => [3/10] COPY requirements.txt .                                      0.1s
 ...
 => pushing manifest for 123456789012.dkr.ecr.us-east-1.amazonaws.com/myapp:latest
 => done

Build complete.
  Duration:  42.1s
  Instance:  c7a.large (us-east-1)
  Cost:      $0.0008
  Image:     123456789012.dkr.ecr.us-east-1.amazonaws.com/myapp:latest
```

**Registry auth source:**
- Registry pull/push credentials are sourced from the local machine's Docker credential configuration and forwarded over the BuildKit session.
- In the current implementation, users should authenticate locally before `forja build --push` for private registries (including ECR if Docker credentials are not already present).

### 9.5 `forja cleanup`

Finds and terminates orphaned instances:

```
$ forja cleanup

Found 1 orphaned forja instance(s):
  i-0abc123def456  c7a.large  running  launched 47 min ago

Terminate? [y/N] y
  [ok] Terminated i-0abc123def456
```

Identification: instances with tag `forja:managed=true` in `running` state.

### 9.6 `forja destroy`

Tears down all resources created by `forja init`:

```
$ forja destroy

This will delete ALL forja AWS resources in us-east-1:
  - S3 bucket: forja-cache-123456789012-us-east-1 (including all cached data)
  - IAM role: forja-builder
  - Security group: forja-builder
  - Launch templates: lt-arm64-abc123, lt-amd64-def456
  - Any running forja instances

Type "destroy" to confirm: destroy

  [ok] Terminated 0 running instances
  [ok] Deleted S3 bucket
  [ok] Deleted launch templates
  [ok] Deleted security group
  [ok] Deleted IAM role + instance profile

All forja resources removed. Config file at ~/.forja/config.yaml retained.
```

## 10. Configuration

### 10.1 Config File

Location: `~/.forja/config.yaml`

```yaml
region: us-east-1
default_platform: linux/amd64

# Instance types per architecture
instances:
  amd64: c7a.large
  arm64: c7g.large

# Default registry for --push
registry: 123456789012.dkr.ecr.us-east-1.amazonaws.com

# S3 cache
cache_bucket: forja-cache-123456789012-us-east-1
cache_ttl_days: 14

# Self-destruct timeout for instances (minutes)
self_destruct_minutes: 60

# Resource IDs (managed by forja init)
resources:
  security_group_id: sg-0abc123
  iam_role_arn: arn:aws:iam::123456789012:role/forja-builder
  instance_profile_arn: arn:aws:iam::123456789012:instance-profile/forja-builder
  launch_templates:
    amd64: lt-amd64-def456
    arm64: lt-arm64-abc123
  ami:
    amd64: ami-0abc123
    arm64: ami-0def456
```

`resources.ami.*` values are required in v1. `forja init` validates that AMI IDs are available for the selected region before creating launch templates.

### 10.2 Pricing Cache

Location: `~/.forja/pricing.json`

Updated automatically on `forja build` if the file is older than 30 days. Fetched from the AWS Pricing API (`pricing:GetProducts`).

```json
{
  "last_updated": "2026-03-06T12:00:00Z",
  "prices": {
    "c7a.large": {"us-east-1": 0.07245, "us-west-2": 0.07245, ...},
    "c7g.large": {"us-east-1": 0.0680, ...},
    ...
  }
}
```

---

## 11. Day-2 Operations

### 11.1 AMI Upgrades

When a new forja version ships with AMI improvements, users update the configured AMI IDs and re-run `forja init` to refresh launch templates.

### 11.2 Orphan Detection

If instances are orphaned (CLI crashed, network loss):

1. Instance self-destructs after 60 minutes (automatic)
2. `forja cleanup` finds and terminates them (manual)
3. AWS billing alerts catch anything that slips through (recommended)

### 11.3 Cache Management

```bash
# Cache is managed by S3 lifecycle rules (14-day TTL by default)
# To clear cache manually:
aws s3 rm s3://forja-cache-123456789012-us-east-1/ --recursive

# To change TTL, edit config and re-run:
forja init  # updates lifecycle rule
```

### 11.4 Debugging Failed Builds

When a build fails:
1. Build output is streamed to the terminal in real-time (BuildKit progress output).
2. Instance is terminated after the build (success or failure).
3. For deeper debugging, user can set `self_destruct_minutes: 120` and SSH into the instance via SSM before it self-destructs.

### 11.5 Security Updates

- AMIs should be refreshed periodically by updating the configured published AMI IDs.
- buildkitd version is pinned in the published AMIs; updating requires a new AMI release.
- mTLS certs are ephemeral per-build, so no cert rotation needed.

---

## 12. Cost Analysis

### 12.1 Per-Build Cost

| Instance Type | vCPU | RAM | Hourly Rate | 1-min build | 5-min build | 10-min build |
|--------------|------|-----|-------------|-------------|-------------|--------------|
| c7g.large    | 2    | 4 GB | $0.068     | $0.001      | $0.006      | $0.011       |
| c7a.large    | 2    | 4 GB | $0.072     | $0.001      | $0.006      | $0.012       |
| c7g.xlarge   | 4    | 8 GB | $0.136     | $0.002      | $0.011      | $0.023       |
| c7a.xlarge   | 4    | 8 GB | $0.145     | $0.002      | $0.012      | $0.024       |
| c7a.2xlarge  | 8    | 16 GB| $0.290     | $0.005      | $0.024      | $0.048       |

EC2 bills per-second with a 60-second minimum.

### 12.2 Monthly Cost Estimates

| Usage Pattern | Instance | Builds/day | Avg duration | Monthly cost |
|--------------|----------|------------|--------------|--------------|
| Light        | c7a.large | 3         | 3 min        | ~$0.60 + S3  |
| Medium       | c7a.xlarge | 10       | 5 min        | ~$7.25 + S3  |
| Heavy        | c7a.2xlarge | 20      | 10 min       | ~$29.00 + S3 |

S3 cache cost: typically $0.05-$0.50/month depending on project size.

### 12.3 Comparison to Depot

Depot pricing: $0.02/min for builds. A 5-minute build = $0.10.
Forja on c7a.large: 5-minute build = $0.006.

**Forja is ~16x cheaper per build minute** since you pay raw EC2 rates.

---

## 13. Go Project Structure

```
forja/
  cmd/
    forja/
      main.go              # CLI entrypoint
  internal/
    cli/
      root.go              # Root command (cobra)
      init.go              # forja init
      build.go             # forja build
      cleanup.go           # forja cleanup
      destroy.go           # forja destroy
      version.go           # forja version
    cloud/
      provider.go          # Provider interfaces used by the CLI
      types.go             # Shared build and infrastructure types
      aws/
        provider.go        # AWS provider implementation
        ec2.go             # Launch, terminate, describe instances
        s3.go              # Bucket creation, cert upload/download, cache config
        iam.go             # Role, policy, instance profile management
        pricing.go         # Pricing API + local cache
        network.go         # Default VPC and subnet discovery
        sg.go              # Security group management
        launch_template.go # Launch template CRUD
    buildkit/
      client.go            # Pure-Go remote BuildKit client
      context.go           # Build context handling
      manifest.go          # Multi-arch manifest merging
    certs/
      generate.go          # Ephemeral CA + cert generation
    config/
      config.go            # Config file read/write
      wizard.go            # Interactive init wizard
    cost/
      calculator.go        # Per-build cost calculation + display
  install.sh               # curl | sh installer
  packer/
    forja-builder.pkr.hcl  # Builder AMI definition
    scripts/
      base.sh              # Installs BuildKit, AWS CLI, SSM agent
      optimize-boot.sh     # Boot-time tuning inspired by Depot's EC2 work
      cleanup.sh           # Image cleanup before snapshot
  .github/
    workflows/
      ami-build.yml        # Maintainer AMI build/publish pipeline
  .goreleaser.yml          # Release configuration
  go.mod
  go.sum
  README.md
  LICENSE
```

### 13.1 Key Dependencies

- `github.com/spf13/cobra` — CLI framework
- `github.com/aws/aws-sdk-go-v2` — AWS SDK
- `github.com/charmbracelet/bubbletea` — Terminal UI framework for interactive prompts
- `github.com/charmbracelet/bubbles` — TUI input components used by the CLI prompts
- `crypto/x509`, `crypto/ecdsa` — Certificate generation (stdlib)
- `github.com/moby/buildkit` — BuildKit client and solve protocol

---

## 14. v1 Scope

v1 ships as a feature-complete single-user tool:

| Feature | Included in v1 |
|---------|---------------|
| `forja init` (interactive wizard) | Yes |
| `forja build` (single arch) | Yes |
| `forja build --platform` (multi-arch) | Yes |
| `forja cleanup` | Yes |
| `forja destroy` | Yes |
| S3 build cache | Yes |
| Per-session ephemeral mTLS | Yes |
| Per-build cost display | Yes |
| Pre-built public AMIs (5 regions) | Yes |
| 60-minute instance self-destruct | Yes |
| `--push`, `--load`, `--secret` flags | Yes |
| `--profile` AWS profile override | Yes |
| Provider abstraction for future non-AWS backends | Yes |
| GitHub Releases + install script | Yes |
| Spot instance support | No (v2) |
| Project namespaces | No (v2) |
| Build history / cumulative cost tracking | No (v2) |
| Homebrew tap | No (v2) |
| CI integration (GitHub Actions action) | No (v2) |

---

## 15. Clarified Decisions / Remaining Questions

### 15.1 Decisions Locked For v1

1. **Full v1 surface area:** Implement the full v1 CLI described in this spec, with pre-built AMI IDs instead of in-account AMI build automation.
2. **Pure-Go build integration:** Remote builds use BuildKit's Go client/protocol directly. The CLI does not shell out to `docker buildx`.
3. **Provider modularity:** Cloud operations are abstracted behind provider interfaces so future backends such as Hetzner can be added without reshaping the CLI.
4. **Networking:** v1 assumes the AWS account's default VPC and default subnets.
5. **Image outputs:** v1 supports `--push` for single-arch and multi-arch builds. `--load` is supported for single-platform builds.

### 15.2 Remaining Questions

1. **BuildKit version pinning:** Should the AMI pin to a specific buildkit release, or track latest stable?
2. **EBS volume type:** gp3 is cheapest for the root volume. How large should it be? 20 GB should suffice for buildkitd + OS since cache is in S3.
3. **Rate limiting:** Should forja prevent launching more than N instances simultaneously to avoid surprise bills?
4. **Telemetry:** Should forja collect anonymous usage metrics (opt-in) for improving the tool?

---

## 16. Testing & End-to-End Validation

### 16.1 Unit Tests

Unit tests cover isolated logic with no AWS calls. All AWS interactions are mocked via the provider interface.

| Package | What to Test | Notes |
|---------|-------------|-------|
| `internal/certs` | CA generation, server/client cert signing, cert chain validation, expiry settings | Pure crypto — no external deps |
| `internal/config` | Config YAML read/write, defaults, validation of required fields, missing file handling | Filesystem only — use `t.TempDir()` |
| `internal/cost` | Cost calculation math (seconds × hourly rate / 3600), pricing cache read/parse, stale cache detection | No AWS calls — feed mock pricing data |
| `internal/cloud` | Provider interface contract: ensure AWS provider satisfies the interface | Compile-time check via `var _ cloud.Provider = (*aws.Provider)(nil)` |
| `internal/cloud/aws` | Each AWS sub-module (ec2, s3, iam, sg, launch_template, pricing) gets tests with mocked AWS SDK clients | Use `aws-sdk-go-v2`'s mock interfaces or `smithy-go/middleware` test helpers |

**Run:** `go test ./...`

### 16.2 Integration Tests (AWS Required)

These tests make real AWS API calls against a dedicated test account. Gated behind `FORJA_INTEGRATION=1` build tag so they don't run in normal CI.

| Test | What It Validates |
|------|-------------------|
| `TestInitCreatesResources` | `forja init` creates S3 bucket, IAM role, instance profile, security group, launch templates. Verify each resource exists via AWS SDK describe calls. |
| `TestDestroyRemovesResources` | After init, `forja destroy` removes all resources. Verify each resource returns "not found". |
| `TestCertUploadAndPull` | Generate certs, upload to S3, download from S3, verify byte-for-byte match + valid TLS handshake between client/server certs. |
| `TestInstanceLaunchAndTerminate` | Launch an instance from the launch template, wait for running state, verify `forja:managed=true` tag, terminate, wait for terminated state. |
| `TestCleanupFindsOrphans` | Launch an instance with forja tags, run cleanup logic, verify it's found and terminated. |
| `TestSecurityGroupRules` | Verify inbound rule is TCP 8372 from 0.0.0.0/0 and outbound is all traffic. |
| `TestIAMPolicyStatements` | Verify the created IAM role policy contains the expected S3, ECR, and SelfTerminate statements. |

**Run:** `FORJA_INTEGRATION=1 go test -tags integration ./...`

**Cleanup:** Each integration test should defer resource cleanup so orphaned resources don't accumulate. Tests should use a unique name suffix (e.g., `forja-test-<random>`) to avoid collisions with real resources.

### 16.3 End-to-End (E2E) Smoke Tests

Full happy-path tests that exercise the entire build flow. Run manually or in CI against a real AWS account.

#### E2E Test 1: Single-Arch Build + Push

```bash
# Prerequisites: AWS creds configured, forja init already run, ECR repo exists
forja build -t <ecr-repo>/forja-e2e:test-amd64 --platform linux/amd64 --push test/fixtures/simple-dockerfile/

# Validate:
# 1. Exit code is 0
# 2. Output contains "Build complete"
# 3. Output contains cost line (e.g., "Cost: $0.00")
# 4. Image is pullable:
docker pull <ecr-repo>/forja-e2e:test-amd64
# 5. No orphaned instances remain:
aws ec2 describe-instances --filters "Name=tag:forja:managed,Values=true" "Name=instance-state-name,Values=running" --query 'Reservations[].Instances[].InstanceId' --output text
# Should return empty
```

#### E2E Test 2: Multi-Arch Build + Push

```bash
forja build -t <ecr-repo>/forja-e2e:test-multi --platform linux/amd64,linux/arm64 --push test/fixtures/simple-dockerfile/

# Validate:
# 1. Exit code is 0
# 2. Manifest list contains both architectures:
docker manifest inspect <ecr-repo>/forja-e2e:test-multi | jq '.manifests[].platform.architecture'
# Should output: "amd64" and "arm64"
# 3. Cost output shows combined cost for both instances
# 4. No orphaned instances
```

#### E2E Test 3: Build with Cache Hit

```bash
# First build (cold cache)
forja build -t <ecr-repo>/forja-e2e:cache-test --push test/fixtures/simple-dockerfile/
# Note the duration

# Second build (warm cache)
forja build -t <ecr-repo>/forja-e2e:cache-test --push test/fixtures/simple-dockerfile/
# Validate: second build should be significantly faster (CACHED steps visible in output)
```

#### E2E Test 4: Build with --load

```bash
forja build -t forja-e2e:load-test --load test/fixtures/simple-dockerfile/

# Validate:
# 1. Image exists locally:
docker image inspect forja-e2e:load-test
# 2. Image runs correctly:
docker run --rm forja-e2e:load-test echo "hello from forja"
```

#### E2E Test 5: Ctrl+C Cleanup

```bash
# Start a build with a slow Dockerfile, then Ctrl+C after instance is launched
forja build -t forja-e2e:cancel-test test/fixtures/slow-dockerfile/ &
sleep 15  # wait for instance to launch
kill -INT $!

# Validate: instance is terminated within a few seconds
sleep 5
aws ec2 describe-instances --filters "Name=tag:forja:managed,Values=true" "Name=instance-state-name,Values=running" --query 'Reservations[].Instances[].InstanceId' --output text
# Should return empty
```

#### E2E Test 6: Init / Destroy Lifecycle

```bash
# Full lifecycle in a clean account/region
forja init  # follow wizard prompts
forja build -t forja-e2e:lifecycle --push test/fixtures/simple-dockerfile/
forja destroy  # type "destroy" to confirm

# Validate:
# 1. After init: config file exists at ~/.forja/config.yaml
# 2. After build: image was pushed successfully
# 3. After destroy: all resources are gone (S3 bucket, IAM role, SG, launch templates)
```

### 16.4 Test Fixtures

```
test/
  fixtures/
    simple-dockerfile/
      Dockerfile           # FROM alpine:3.19 \n RUN echo "hello" > /hello.txt \n CMD ["cat", "/hello.txt"]
    slow-dockerfile/
      Dockerfile           # FROM alpine:3.19 \n RUN sleep 120    (for cancel testing)
    multi-stage/
      Dockerfile           # Multi-stage build to test --target flag
    with-secrets/
      Dockerfile           # RUN --mount=type=secret,id=mysecret cat /run/secrets/mysecret
      secret.txt           # "test-secret-value"
```

### 16.5 CI Pipeline

Current baseline implemented in the repo:
- A GitHub Actions workflow runs `go test ./...`, `go vet ./...`, and `go build ./...` on pushes and pull requests.
- This baseline CI covers unit and compile validation only. AWS integration and E2E validation remain a separate follow-up because they require dedicated cloud credentials, AMIs, and registries.

```yaml
# .github/workflows/ci.yml
name: CI
on: [push, pull_request]

jobs:
  unit:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - run: go test ./...
      - run: go vet ./...
      - run: go build ./...

The following integration and E2E jobs are the intended next stage once dedicated CI cloud infrastructure exists:

  integration:
    runs-on: ubuntu-latest
    if: github.ref == 'refs/heads/main'
    needs: unit
    permissions:
      id-token: write  # OIDC for AWS
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.22'
      - uses: aws-actions/configure-aws-credentials@v4
        with:
          role-to-assume: arn:aws:iam::${{ secrets.AWS_ACCOUNT_ID }}:role/forja-ci
          aws-region: us-east-1
      - run: FORJA_INTEGRATION=1 go test -tags integration -timeout 10m ./...

  e2e:
    runs-on: ubuntu-latest
    if: github.ref == 'refs/heads/main'
    needs: integration
    permissions:
      id-token: write
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.22'
      - uses: aws-actions/configure-aws-credentials@v4
        with:
          role-to-assume: arn:aws:iam::${{ secrets.AWS_ACCOUNT_ID }}:role/forja-ci
          aws-region: us-east-1
      - run: go build -o forja ./cmd/forja
      - run: ./test/e2e/run-all.sh
```

### 16.6 Validation Checklist

Before cutting a release, verify all of the following:

- [ ] `go test ./...` passes (all unit tests)
- [ ] `go vet ./...` and `golangci-lint run` report no issues
- [ ] Integration tests pass against a real AWS account
- [ ] E2E Test 1: Single-arch build + push works
- [ ] E2E Test 2: Multi-arch build produces valid manifest list
- [ ] E2E Test 3: Second build is faster (cache hit confirmed)
- [ ] E2E Test 4: `--load` pulls image to local daemon
- [ ] E2E Test 5: Ctrl+C terminates instance promptly
- [ ] E2E Test 6: Full init/build/destroy lifecycle completes cleanly
- [ ] No orphaned instances remain after any test
- [ ] `goreleaser --snapshot` produces binaries for all 4 targets
- [ ] Install script (`curl | sh`) downloads and runs the binary on macOS and Linux
- [ ] Cost display shows a reasonable non-zero value after a build
- [ ] mTLS: a raw TCP connection to port 8372 (without client cert) is rejected

---

## References

- [Depot: How it works](https://depot.dev/docs/container-builds/overview)
- [Depot: Making EC2 boot time 8x faster](https://depot.dev/blog/faster-ec2-boot-time)
- [Depot: Improving EC2 boot time from 4s to 2.8s](https://depot.dev/blog/accelerating-builds-improve-ec2-boot-time)
- [BuildKit S3 cache backend](https://lchenghui.com/my-journey-on-building-a-remote-docker-buildkit)
- [BuildKit in depth](https://depot.dev/blog/buildkit-in-depth)
- [AWS EC2 Hibernate](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/Hibernate.html)
- [AWS EC2 Warm Pools](https://docs.aws.amazon.com/autoscaling/ec2/userguide/ec2-auto-scaling-warm-pools.html)
