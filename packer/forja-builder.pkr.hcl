packer {
  required_version = ">= 1.11.0"

  required_plugins {
    amazon = {
      source  = "github.com/hashicorp/amazon"
      version = ">= 1.3.0, < 2.0.0"
    }
  }
}

variable "region" {
  type        = string
  description = "AWS region to build the AMI in."
  default     = "us-east-1"
}

variable "architecture" {
  type        = string
  description = "Target architecture: amd64 or arm64."

  validation {
    condition     = contains(["amd64", "arm64"], var.architecture)
    error_message = "Architecture must be amd64 or arm64."
  }
}

variable "instance_type" {
  type        = string
  description = "Optional EC2 instance type to use during the build."
  default     = ""
}

variable "buildkit_version" {
  type        = string
  description = "Pinned BuildKit release to install."
  default     = "v0.28.0"
}

variable "awscli_version" {
  type        = string
  description = "Pinned AWS CLI v2 release to install."
  default     = "2.27.50"
}

variable "ami_name_prefix" {
  type        = string
  description = "Prefix for generated AMI names."
  default     = "forja-builder"
}

variable "commit_sha" {
  type        = string
  description = "Git commit used for traceability tags."
  default     = "dev"
}

variable "publish_public" {
  type        = bool
  description = "Whether to publish the resulting AMI publicly."
  default     = false
}

variable "copy_regions" {
  type        = list(string)
  description = "Additional regions to copy the AMI to after building."
  default     = []
}

variable "ssh_username" {
  type        = string
  description = "SSH username for the source image."
  default     = "ubuntu"
}

locals {
  arch_to_source = {
    amd64 = {
      source_name   = "ubuntu/images/hvm-ssd-gp3/ubuntu-noble-24.04-amd64-server-*"
      instance_type = "t3.large"
      awscli_arch   = "x86_64"
      ssm_arch      = "amd64"
      buildkit_arch = "amd64"
    }
    arm64 = {
      source_name   = "ubuntu/images/hvm-ssd-gp3/ubuntu-noble-24.04-arm64-server-*"
      instance_type = "t4g.large"
      awscli_arch   = "aarch64"
      ssm_arch      = "arm64"
      buildkit_arch = "arm64"
    }
  }

  build     = local.arch_to_source[var.architecture]
  timestamp = formatdate("YYYYMMDDhhmmss", timestamp())
  ami_name  = "${var.ami_name_prefix}-${var.architecture}-${local.timestamp}"
}

source "amazon-ebs" "forja_builder" {
  region                      = var.region
  instance_type               = var.instance_type != "" ? var.instance_type : local.build.instance_type
  ssh_username                = var.ssh_username
  associate_public_ip_address = true
  ssh_interface               = "public_ip"
  ena_support                 = true

  source_ami_filter {
    filters = {
      virtualization-type = "hvm"
      name                = local.build.source_name
      root-device-type    = "ebs"
    }
    most_recent = true
    owners      = ["099720109477"]
  }

  launch_block_device_mappings {
    device_name           = "/dev/sda1"
    volume_size           = 20
    volume_type           = "gp3"
    delete_on_termination = true
  }

  metadata_options {
    http_endpoint = "enabled"
    http_tokens   = "required"
  }

  ami_name        = local.ami_name
  ami_description = "Forja remote BuildKit builder (${var.architecture})"
  ami_groups      = var.publish_public ? ["all"] : []
  ami_regions     = var.copy_regions

  tags = {
    Name              = local.ami_name
    "forja:managed"   = "true"
    "forja:component" = "builder-ami"
    "forja:arch"      = var.architecture
    "forja:commit"    = var.commit_sha
  }

  run_tags = {
    Name              = "packer-${local.ami_name}"
    "forja:managed"   = "true"
    "forja:component" = "packer-build"
    "forja:arch"      = var.architecture
    "forja:commit"    = var.commit_sha
  }
}

build {
  name    = "forja-builder"
  sources = ["source.amazon-ebs.forja_builder"]

  provisioner "shell" {
    execute_command = "chmod +x {{ .Path }}; {{ .Vars }} sudo -E {{ .Path }}"
    environment_vars = [
      "FORJA_ARCH=${var.architecture}",
      "AWSCLI_ARCH=${local.build.awscli_arch}",
      "AWSCLI_VERSION=${var.awscli_version}",
      "SSM_ARCH=${local.build.ssm_arch}",
      "BUILDKIT_ARCH=${local.build.buildkit_arch}",
      "BUILDKIT_VERSION=${var.buildkit_version}",
    ]
    scripts = [
      "packer/scripts/base.sh",
      "packer/scripts/optimize-boot.sh",
      "packer/scripts/cleanup.sh",
    ]
  }

  post-processor "manifest" {
    output = "packer/manifest.json"
  }
}
