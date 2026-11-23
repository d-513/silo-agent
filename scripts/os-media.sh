#!/usr/bin/env bash
# os-media.sh TYPE ARCH VERSION OUT DIST
#
# Turns what bootc-image-builder made (`make os-iso` / `make os-disk`, under
# OUT) into SiloOS release assets in DIST, named
# silo-os-<VERSION>-<ARCH>.<ext>, and writes each one's SHA-256 line to
# DIST/sums/<asset>.sha256 (the release joins them into SHA256SUMS).
#
#   iso    the unattended installer          .iso
#   qcow2  libvirt, Proxmox, QEMU, OpenStack .qcow2, and the Incus metadata
#          (`make os-incus-meta`)            -incus-metadata.tar.xz
#   raw    to dd onto a drive (zstd)         .raw.zst
#   vmdk   VMware                            .vmdk
#   vhd    Azure (zstd)                      .vhd.zst
#   gce    Google Cloud                      -gce.tar.gz
#
# GitHub refuses a release asset of 2 GiB or more, so a larger file is cut
# into .partNN pieces (cat them back together); its checksum is of the whole.
# OS_MEDIA_LIMIT_MB overrides the limit, for tests.
set -euo pipefail

type=${1:?type} arch=${2:?arch} version=${3:?version} out=${4:?out} dist=${5:?dist}
limit=$(( ${OS_MEDIA_LIMIT_MB:-2000} * 1024 * 1024 ))
piece=$(( limit * 19 / 20 ))
base="silo-os-$version-$arch"

mkdir -p "$dist/sums"

# one PATTERN: the one file bootc-image-builder made for this type.
one() {
  local found
  mapfile -t found < <(find "$out/$type" -type f -name "$1" ! -name 'manifest-*' 2>/dev/null)
  if [ "${#found[@]}" -ne 1 ]; then
    echo "os-media: expected one $1 under $out/$type, found ${#found[@]}: ${found[*]:-none}" >&2
    exit 1
  fi
  echo "${found[0]}"
}

# put SRC NAME [zstd]: SRC becomes DIST/NAME (compressed with zstd when asked),
# its checksum is written, and it is cut into pieces when too large.
put() {
  local src=$1 name=$2 dst="$dist/$2"
  if [ "${3:-}" = zstd ]; then
    zstd -q -T0 -10 -o "$dst" "$src"
  else
    mv -- "$src" "$dst"
  fi
  (cd "$dist" && sha256sum "$name" > "sums/$name.sha256")
  if [ "$(stat -c %s "$dst")" -ge "$limit" ]; then
    split -b "$piece" -d -a 2 "$dst" "$dst.part"
    rm -f -- "$dst"
    echo "os-media: $name is over the asset limit, cut into $(find "$dist" -maxdepth 1 -name "$name.part*" | wc -l) pieces"
  fi
}

# (Each lookup is its own assignment: a failing $(one …) inside another
# command's arguments would not stop the script.)
case "$type" in
  iso) src=$(one install.iso); put "$src" "$base.iso" ;;
  qcow2)
    src=$(one disk.qcow2); put "$src" "$base.qcow2"
    meta="$out/qcow2/metadata.tar.xz"
    [ -f "$meta" ] || { echo "os-media: no $meta (make os-incus-meta)" >&2; exit 1; }
    put "$meta" "$base-incus-metadata.tar.xz"
    ;;
  raw) src=$(one disk.raw); put "$src" "$base.raw.zst" zstd ;;
  vmdk) src=$(one disk.vmdk); put "$src" "$base.vmdk" ;;
  vhd) src=$(one disk.vhd); put "$src" "$base.vhd.zst" zstd ;;
  gce) src=$(one '*.tar.gz'); put "$src" "$base-gce.tar.gz" ;;
  *) echo "os-media: unknown type $type" >&2; exit 1 ;;
esac

ls -l "$dist"
