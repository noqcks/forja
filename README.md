# Forja

Self-hosted remote Docker image builder for AWS. Like [Depot](https://depot.dev), but runs entirely in your own account with no SaaS dependency.

Forja spins up ephemeral EC2 instances running [BuildKit](https://github.com/moby/buildkit), builds your images remotely over mTLS, and tears down the instance when done. You pay only for EC2 compute time and S3 cache storage.

## How It Works

1. `forja init` provisions AWS infrastructure (EC2 launch template, S3 cache bucket, IAM role, security group)
2. `forja build .` launches an ephemeral EC2 instance, sends build context over gRPC with mTLS, builds remotely, and terminates the instance
3. Build cache persists in S3 across builds
4. Cost of each build is displayed on completion

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

## Installation

```bash
# Install script (Linux + macOS)
curl -sfL https://get.forja.dev | sh

# Or download from GitHub Releases
```

Binaries are available for `darwin/arm64`, `darwin/amd64`, `linux/amd64`, and `linux/arm64`.

## Quick Start

```bash
# 1. Provision AWS resources (interactive wizard)
forja init

# 2. Build and push an image
forja build -t 123456789012.dkr.ecr.us-east-1.amazonaws.com/myapp:latest --push .

# 3. When you're done, tear everything down
forja destroy
```

## Commands

| Command | Description |
|---------|-------------|
| `forja init` | Interactive wizard to provision AWS resources |
| `forja build [context]` | Build a Docker image remotely |
| `forja cleanup` | Find and terminate orphaned forja instances |
| `forja destroy` | Tear down all AWS resources created by `forja init` |
| `forja version` | Print version |

## Build Examples

```bash
# Simple build, push to ECR
forja build -t 123456789012.dkr.ecr.us-east-1.amazonaws.com/myapp:latest --push .

# Multi-arch build
forja build --platform linux/amd64,linux/arm64 -t myapp:latest --push .

# Build with secrets
forja build --secret id=npmrc,src=$HOME/.npmrc -t myapp:latest .

# Override instance type for a heavy build
forja build --instance-type c7a.2xlarge -t myapp:latest --push .

# Load image into local Docker daemon
forja build -t myapp:latest --load .
```

### Build Flags

```
  -f, --file string          Path to Dockerfile (default: Dockerfile)
  -t, --tag strings          Image tag(s)
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

## Multi-Architecture Builds

When `--platform linux/amd64,linux/arm64` is specified, Forja launches two instances in parallel:

- Graviton (e.g., `c7g.xlarge`) for `linux/arm64`
- Intel/AMD (e.g., `c7a.xlarge`) for `linux/amd64`

Each builds natively on its target architecture -- no QEMU emulation. Forja then creates and pushes a multi-arch manifest list combining both images.

## Cost

You pay EC2 on-demand rates with no markup. For comparison, Depot charges ~16x more per build minute.

| Instance Type | vCPU | RAM | Hourly Rate | 5-min build |
|--------------|------|-----|-------------|-------------|
| c7g.large | 2 | 4 GB | $0.068 | $0.006 |
| c7a.large | 2 | 4 GB | $0.072 | $0.006 |
| c7g.xlarge | 4 | 8 GB | $0.136 | $0.011 |
| c7a.xlarge | 4 | 8 GB | $0.145 | $0.012 |
| c7a.2xlarge | 8 | 16 GB | $0.290 | $0.024 |

**Idle cost: effectively $0** (only S3 cache storage, ~$0.023/GB/month).

## AWS Resources

All resources are created by `forja init` and removed by `forja destroy`:

| Resource | Purpose |
|----------|---------|
| S3 bucket | BuildKit cache + ephemeral cert delivery |
| IAM role + instance profile | Instance permissions (S3, ECR, self-terminate) |
| Security group | Inbound on port 8372 (BuildKit), secured by mTLS |
| EC2 launch templates | One per architecture (arm64, amd64) |
| S3 lifecycle rule | Auto-expire cache after 14 days |

### Required IAM Permissions

The user running `forja` needs permissions for EC2, S3, IAM, and Pricing APIs. See [forja-spec.md](forja-spec.md#54-cli-user-iam-permissions) for the full list.

## Security

- **No SSH. No persistent servers.** Instances are ephemeral and self-destruct after 60 minutes.
- **Per-session mTLS certificates.** Every build generates a fresh CA + server/client cert chain. Certs are never reused.
- **No SaaS dependency.** Everything runs in your AWS account. Source code and build artifacts never leave your infrastructure.
- **Signal handling.** On Ctrl+C or SIGTERM, the CLI terminates all instances launched for the current build.
- **Self-destruct safety net.** If the CLI is killed hard, instances terminate themselves after 60 minutes via a systemd timer.

## Configuration

Config is stored at `~/.forja/config.yaml` after running `forja init`:

```yaml
region: us-east-1
default_platform: linux/amd64
instances:
  amd64: c7a.large
  arm64: c7g.large
registry: 123456789012.dkr.ecr.us-east-1.amazonaws.com
cache_bucket: forja-cache-123456789012-us-east-1
cache_ttl_days: 14
self_destruct_minutes: 60
```

## License

Open source. See [LICENSE](LICENSE) for details.
