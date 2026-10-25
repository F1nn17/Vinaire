# Releasing

[По-русски](./releasing.ru.md)

A release is one version number that appears in three places and has to agree: the chart's `version`
and `appVersion` (`charts/vinaire/Chart.yaml`), a `## [x.y.z]` section of `CHANGELOG.md`, and the git tag
`vx.y.z`. The **Release** workflow (`.github/workflows/release.yml`) checks that they agree, builds the image,
pushes it and the chart to GitHub's registry, signs both with cosign (keyless), attaches an SBOM and a
provenance attestation, and creates the tag and the GitHub Release — all in one run, so there is nothing to
forget and no tag to push by hand.

## Making a release

1. **Prepare** — on a branch, run the script with the new version:

   ```bash
   git switch -c release/v1.0.1
   scripts/release-prepare.sh 1.0.1
   git commit -am "release: v1.0.1"
   git push -u origin release/v1.0.1
   ```

   It sets `version` and `appVersion` in the chart and turns `## [Unreleased]` of the changelog into
   `## [1.0.1] - <today>` (with a fresh empty `[Unreleased]` above it), and moves the version named in the README's
   install and verify commands (`README.md`, `README.ru.md`; not for a pre-release) to the one being cut. It refuses an empty
   `[Unreleased]` — there would be nothing to release.
2. **Review and merge** the pull request. The checks (CI, Security) must pass; `main` is protected.
3. **Release** — Actions → **Release** → *Run workflow*, branch `main`, version `1.0.1`; or
   `gh workflow run release.yml --ref main -f version=1.0.1`.

The workflow refuses to run from any other branch, for a version that is not the chart's, for a version
without a changelog section, and for a tag that already exists (a released version is never replaced). The
release notes are the changelog section, and a version with a suffix (`1.1.0-rc.1`) is published as a
pre-release.

Why one workflow instead of "tag, and a tag starts the release": a tag created by the default
`GITHUB_TOKEN` does **not** start other workflows. Creating the tag in the release workflow itself needs no
extra token or GitHub App. Pushing a `v*` tag by hand still works and does the same.

## What guards the version

- **The chart's defaults and `--reuse-values`.** Helm replaces the new chart's defaults with the old release's values when an
  upgrade uses `--reuse-values`, so every template first fills in the keys that are *absent* from the release's values
  (`vinaire.defaults`, from `charts/vinaire/files/defaults.yaml`). That file is an exact copy of `values.yaml`: **when you change
  `values.yaml`, copy it** (`cp charts/vinaire/values.yaml charts/vinaire/files/defaults.yaml`); a test fails if they differ.
  The `chart-upgrade` job of CI installs the oldest and the latest earlier release and upgrades each with `--reuse-values`.
- **The tools CI installs** (k3d, Helm, govulncheck, gosec) are pinned in `.github/tool-versions.env`, k3d and Helm with the
  sha256 of the binary; to update one, change the version (and the hash) there — see the file's header.

## Checking a release

```bash
scripts/verify-release.sh 1.0.1             # signatures of image and chart, SBOM, the chart renders
scripts/verify-release.sh 1.0.1 --install   # ... and installs it into a throwaway namespace
```

## The first release (1.0.0) — checklist for opening the repository

Done in the repository already: the version is `1.0.0` everywhere, the changelog has its `[1.0.0]` section, the
release workflow, the verification script, `SECURITY.md` and `CONTRIBUTING.md`, the README, a full-history secret
scan (gitleaks: no findings) and a Go module path that matches the repository (`github.com/f1nn17/vinaire`).

On GitHub, in this order:

1. **Protect `main`** — Settings → Rules → Rulesets → new branch ruleset on `main`: require a pull request,
   require the status checks (the jobs of *CI* and *Security*), block force pushes and deletion.
2. **Allow the workflow to write packages** — Settings → Actions → General → Workflow permissions: *Read and write*
   (the release job also asks for `packages: write` itself).
3. **Make the repository public** — Settings → General → Danger zone → *Change visibility*. Turn on *Private
   vulnerability reporting* (Settings → Code security) — `SECURITY.md` points people to it.
4. **Release** — run the workflow as above with version `1.0.0`. (The first run creates the tag `v1.0.0`.)
5. **Make the packages public** — ghcr.io packages start private: your profile → Packages → `vinaire` and
   `charts/vinaire` → Package settings → *Change visibility* → Public. Until then, nobody else can pull them.
6. **Verify from a clean machine** — `scripts/verify-release.sh 1.0.0 --install` (or the README's quick start on a
   throwaway cluster).
7. Optionally: add a repository description and topics (`kubernetes`, `k3s`, `dashboard`, `helm`), and pin the
   release.
