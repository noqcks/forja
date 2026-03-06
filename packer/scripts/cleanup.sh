#!/usr/bin/env bash
set -euo pipefail

export DEBIAN_FRONTEND=noninteractive

apt-get autoremove -y
apt-get clean
rm -rf /var/lib/apt/lists/*
rm -rf /tmp/* /var/tmp/*

cloud-init clean --logs --seed || true
rm -rf /var/lib/cloud/*

journalctl --rotate || true
journalctl --vacuum-time=1s || true

truncate -s 0 /etc/machine-id
rm -f /var/lib/dbus/machine-id

find /var/log -type f -exec truncate -s 0 {} \;
sync
