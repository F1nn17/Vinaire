#!/usr/bin/env bash
# Verify a published release (Roadmap Phase 65): the signatures of the image and of the Helm chart,
# and that the chart installs. Needs cosign, helm and (for the install check) kubectl pointing at a
# throwaway cluster.
#
#   scripts/verify-release.sh 1.0.0                 # signatures + `helm template`
#   scripts/verify-release.sh 1.0.0 --install       # ... and a real install into a namespace, removed afterwards
#
# Override the repository with REPO=owner/name (default F1nn17/Vinaire).
set -euo pipefail

version="${1:-}"
install=false
[ "${2:-}" = "--install" ] && install=true
if [ -z "$version" ]; then
  echo "usage: $0 <version> [--install]" >&2
  exit 2
fi

repo="${REPO:-F1nn17/Vinaire}"
owner="${repo%%/*}"
owner_lc="$(echo "$owner" | tr '[:upper:]' '[:lower:]')"
name_lc="$(echo "${repo##*/}" | tr '[:upper:]' '[:lower:]')"
image="ghcr.io/${owner_lc}/${name_lc}:${version}"
chart="oci://ghcr.io/${owner_lc}/charts/vinaire"
identity="https://github.com/${repo}/\.github/workflows/release\.yml@.*"
issuer="https://token.actions.githubusercontent.com"

for tool in cosign helm; do
  command -v "$tool" >/dev/null || { echo "$tool is needed" >&2; exit 1; }
done

echo "== image signature: $image"
cosign verify "$image" --certificate-identity-regexp "$identity" --certificate-oidc-issuer "$issuer" >/dev/null
echo "   ok"

echo "== image SBOM attestation"
cosign verify-attestation "$image" --type cyclonedx \
  --certificate-identity-regexp "$identity" --certificate-oidc-issuer "$issuer" >/dev/null
echo "   ok"

echo "== chart signature: $chart:$version"
digest=$(helm pull "$chart" --version "$version" --destination /tmp 2>&1 | sed -n 's/^Digest: //p')
if [ -z "$digest" ]; then echo "   could not read the chart digest" >&2; exit 1; fi
cosign verify "${chart#oci://}@${digest}" --certificate-identity-regexp "$identity" --certificate-oidc-issuer "$issuer" >/dev/null
echo "   ok ($digest)"

echo "== the chart renders and points at the signed image"
rendered=$(helm template vinaire "$chart" --version "$version" --namespace vinaire)
echo "$rendered" | grep -q "image: .*${name_lc}:${version}" \
  || { echo "   the chart does not use ${image}" >&2; exit 1; }
echo "   ok"

if $install; then
  ns="vinaire-verify-$$"
  echo "== installing into namespace $ns (removed afterwards)"
  trap 'helm uninstall vinaire -n "$ns" >/dev/null 2>&1 || true; kubectl delete ns "$ns" --wait=false >/dev/null 2>&1 || true' EXIT
  helm install vinaire "$chart" --version "$version" --namespace "$ns" --create-namespace --wait --timeout 3m
  kubectl -n "$ns" get pods
  echo "   ok"
fi

echo "release $version verified"
