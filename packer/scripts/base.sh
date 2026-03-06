#!/usr/bin/env bash
set -euo pipefail

export DEBIAN_FRONTEND=noninteractive

apt-get update
apt-get install -y --no-install-recommends \
  ca-certificates \
  curl \
  jq \
  tar \
  unzip \
  xz-utils

install -d -m 0755 /etc/buildkit/certs /var/lib/buildkit

curl -fsSL -o /tmp/awscliv2.zip "https://awscli.amazonaws.com/awscli-exe-linux-${AWSCLI_ARCH}-${AWSCLI_VERSION}.zip"
rm -rf /tmp/aws
unzip -q /tmp/awscliv2.zip -d /tmp
/tmp/aws/install --update

curl -fsSL -o /tmp/buildkit.tgz "https://github.com/moby/buildkit/releases/download/${BUILDKIT_VERSION}/buildkit-${BUILDKIT_VERSION}.linux-${BUILDKIT_ARCH}.tar.gz"
tar -xzf /tmp/buildkit.tgz -C /tmp
install -m 0755 /tmp/bin/buildctl /usr/local/bin/buildctl
install -m 0755 /tmp/bin/buildkitd /usr/local/bin/buildkitd
install -m 0755 /tmp/bin/buildkit-runc /usr/local/bin/buildkit-runc

cat >/etc/systemd/system/buildkitd.service <<'EOF'
[Unit]
Description=BuildKit daemon
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/buildkitd --addr tcp://0.0.0.0:8372
Restart=always
RestartSec=2
LimitNOFILE=1048576
TasksMax=infinity

[Install]
WantedBy=multi-user.target
EOF

cat >/etc/cloud/cloud.cfg.d/99-forja.cfg <<'EOF'
datasource_list: [ Ec2 ]
package_update: false
package_upgrade: false
package_reboot_if_required: false
EOF

systemctl daemon-reload
systemctl disable buildkitd.service
