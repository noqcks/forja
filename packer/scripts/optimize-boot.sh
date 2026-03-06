#!/usr/bin/env bash
set -euo pipefail

export DEBIAN_FRONTEND=noninteractive

fstab_tmp="$(mktemp)"
awk '
  BEGIN { OFS="\t" }
  $0 ~ /^[[:space:]]*#/ || NF < 4 { print; next }
  {
    if (($3 == "ext4" || $3 == "xfs") && $4 !~ /(^|,)noatime(,|$)/) {
      $4 = $4 ",noatime"
    }
    print
  }
' /etc/fstab >"${fstab_tmp}"
cat "${fstab_tmp}" >/etc/fstab
rm -f "${fstab_tmp}"

append_grub_arg() {
  local arg="$1"
  if ! grep -q "${arg}" /etc/default/grub; then
    sed -i "s/^GRUB_CMDLINE_LINUX=\"\\(.*\\)\"/GRUB_CMDLINE_LINUX=\"\\1 ${arg}\"/" /etc/default/grub
  fi
}

append_grub_arg "fsck.mode=skip"
append_grub_arg "audit=0"
update-grub

install -d -m 0755 /etc/systemd/network/10-netplan-forja.network.d
cat >/etc/systemd/network/10-netplan-forja.network.d/override.conf <<'EOF'
[Network]
IPv6DuplicateAddressDetection=0
EOF

systemctl disable --now apt-daily.timer apt-daily-upgrade.timer || true
systemctl mask apt-daily.service apt-daily-upgrade.service apt-daily.timer apt-daily-upgrade.timer || true

systemctl disable --now systemd-networkd-wait-online.service || true
systemctl mask systemd-networkd-wait-online.service || true

systemctl mask systemd-journal-flush.service || true
systemctl mask multipathd.service multipathd.socket || true
systemctl mask lvm2-monitor.service lvm2-lvmpolld.service lvm2-lvmpolld.socket || true
systemctl mask snapd.service snapd.socket snapd.seeded.service snapd.apparmor.service || true

if command -v snap >/dev/null 2>&1; then
  snap list --all | awk 'NR > 1 {print $1}' | sort -u | xargs -r -n1 snap remove --purge || true
fi
apt-get purge -y snapd || true
rm -rf /snap /var/snap /var/lib/snapd /var/cache/snapd
rm -f /usr/lib/udev/rules.d/66-azure-ephemeral.rules /lib/udev/rules.d/66-azure-ephemeral.rules || true

systemctl disable --now apparmor.service || true
systemctl mask apparmor.service || true

update-initramfs -u || true
