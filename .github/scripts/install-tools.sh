#!/usr/bin/env bash
# Installs the CI tools at the versions pinned in .github/tool-versions.env, checking each download against the
# sha256 written there (Roadmap Phase 97). Usage: install-tools.sh k3d helm
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../tool-versions.env
. "$here/../tool-versions.env"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

fetch() { curl -fsSL --retry 5 --retry-all-errors -o "$2" "$1"; }

verify() { # <sha256> <file>
  echo "$1  $2" | sha256sum -c - >/dev/null || { echo "::error::$2 does not match the pinned sha256 — refusing to install it"; exit 1; }
}

install_k3d() {
  fetch "https://github.com/k3d-io/k3d/releases/download/v${K3D_VERSION}/k3d-linux-amd64" "$work/k3d"
  verify "$K3D_SHA256_LINUX_AMD64" "$work/k3d"
  sudo install -m 0755 "$work/k3d" /usr/local/bin/k3d
  k3d version
}

install_helm() {
  fetch "https://get.helm.sh/helm-v${HELM_VERSION}-linux-amd64.tar.gz" "$work/helm.tgz"
  verify "$HELM_SHA256_LINUX_AMD64" "$work/helm.tgz"
  tar -xzf "$work/helm.tgz" -C "$work" linux-amd64/helm
  sudo install -m 0755 "$work/linux-amd64/helm" /usr/local/bin/helm
  helm version --short
}

if [ "$#" -eq 0 ]; then
  echo "usage: $0 k3d|helm ..." >&2
  exit 2
fi
for tool in "$@"; do
  case "$tool" in
    k3d) install_k3d ;;
    helm) install_helm ;;
    *) echo "unknown tool: $tool" >&2; exit 2 ;;
  esac
done
