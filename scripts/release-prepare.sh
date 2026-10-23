#!/usr/bin/env bash
# Prepare a release (Roadmap Phase 64): set the version in the chart and cut the CHANGELOG.
#
#   scripts/release-prepare.sh 1.0.1
#
# It edits three kinds of file and nothing else — charts/vinaire/Chart.yaml (version and appVersion, which
# must stay equal), CHANGELOG.md (the [Unreleased] section becomes [1.0.1], a fresh empty
# [Unreleased] goes above it) and the version named in the README's install and verify commands (README.md,
# README.ru.md; skipped for a pre-release) — then tells you what to do next. Commit the result on a branch, open
# a pull request, merge it, and run the "Release" workflow from main with the same version (docs/releasing.md).
set -euo pipefail

version="${1:-}"
if ! [[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$ ]]; then
  echo "usage: $0 <version>   e.g. $0 1.0.1   (or 1.1.0-rc.1)" >&2
  exit 2
fi

cd "$(dirname "$0")/.."
chart=charts/vinaire/Chart.yaml
log=CHANGELOG.md

current=$(grep '^version:' "$chart" | head -1 | tr -d ' "\r' | cut -d: -f2)
if [ "$current" = "$version" ]; then
  echo "the chart is already at $version" >&2
  exit 1
fi
if grep -q "^## \[$version\]" "$log"; then
  echo "CHANGELOG.md already has a section for $version" >&2
  exit 1
fi
if ! grep -q '^## \[Unreleased\]' "$log"; then
  echo "CHANGELOG.md has no '## [Unreleased]' heading to cut" >&2
  exit 1
fi
# an empty [Unreleased] means nothing to release
body=$(awk '/^## \[Unreleased\]/{on=1;next} on&&/^## \[/{exit} on{print}' "$log" | tr -d '[:space:]')
if [ -z "$body" ]; then
  echo "[Unreleased] in CHANGELOG.md is empty: there is nothing to release" >&2
  exit 1
fi

today=$(date +%Y-%m-%d)
# the two chart fields, kept equal
sed -i.bak -E "s/^version:.*/version: $version/; s/^appVersion:.*/appVersion: \"$version\"/" "$chart"
# cut the changelog
sed -i.bak -E "s/^## \[Unreleased\]/## [Unreleased]\n\n## [$version] - $today/" "$log"
rm -f "$chart.bak" "$log.bak"
# the README's quick start names the version it installs and verifies: keep it on the release being cut
# (a pre-release suffix is not a version people should copy from a README)
if [[ ! "$version" =~ - ]]; then
  for readme in README.md README.ru.md; do
    [ -f "$readme" ] || continue
    sed -i.bak -E "s#(charts/vinaire --version )[0-9]+\.[0-9]+\.[0-9]+#$version#; s#(ghcr\.io/f1nn17/vinaire:)[0-9]+\.[0-9]+\.[0-9]+#$version#" "$readme"
    rm -f "$readme.bak"
  done
fi

echo "chart $current -> $version, CHANGELOG cut as [$version] - $today"
echo
echo "next:"
echo "  git switch -c release/v$version"
echo "  git commit -am \"release: v$version\""
echo "  git push -u origin release/v$version   # open a pull request, wait for the checks, merge it"
echo "  then: Actions -> Release -> Run workflow (branch main), version $version"
echo "     or: gh workflow run release.yml --ref main -f version=$version"
