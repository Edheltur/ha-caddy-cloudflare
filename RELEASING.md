# Release checklist

## Repository setup

- Protect main and `v*` tags; require reviewed changes and successful CI.
- Configure the `ghcr-release` environment with required reviewers and version-tag
  deployment restrictions. The workflow declaration alone does not enforce approval.
- Configure `acme-staging` for main only, with a disposable zone-scoped
  `CF_API_TOKEN` secret and `ACME_TEST_DOMAIN` variable. Never use production
  credentials in public pull requests.
- Enable private vulnerability reporting and install/configure Renovate for this
  repository. `renovate.json` alone does not schedule update PRs.
- Use the workflow's GITHUB_TOKEN for GHCR; no personal access token is needed.

## Prepare and publish

1. Complete and record the acceptance checks in [tests/README.md](tests/README.md).
   Until HA OS installation, backup/restore and real ACME checks pass, retain
   `stage: experimental`. A candidate image can be published for this acceptance;
   do not describe it as production-validated.
2. Set `version` in `caddy/config.yaml`, update the changelog and Dockerfile's
   default BUILD_VERSION together. Keep `image` equal to
   `ghcr.io/edheltur/ha-caddy-cloudflare/caddy` without a tag.
3. Merge the reviewed PR after ARM64 CI succeeds. Create `v<version>` on that
   main commit. Dispatch **Build and security checks** on the tag with
   `publish=true`, then approve the release environment.
4. The workflow rejects existing version tags and fails on registry errors.
   It uploads a candidate by digest without a version tag, tests and scans that
   exact image, then assigns the version tag without rebuilding. The image index
   retains SBOM and provenance. A failed check can leave an untagged candidate
   in GHCR; it must not be installed or manually tagged as a release.
5. On first publication, set the GHCR package visibility to **Public** in package
   settings. Public repository visibility does not make the package public.
6. On a clean ARM64 client, verify anonymous access using empty Docker credentials:

   ```sh
   docker_config=$(mktemp -d)
   DOCKER_CONFIG="$docker_config" docker pull ghcr.io/edheltur/ha-caddy-cloudflare/caddy:0.1.0
   rm -rf "$docker_config"
   ```

   Substitute the actual version. Record the digest and successful ARM64 run in
   the GitHub release notes. Verify the manifest includes linux/arm64 and attestations.
7. Install using the repository URL in the HA app store and finish acceptance.
   Publish release notes with any outstanding experimental limitations.

The first merge enables the GHCR reference before the first image is available;
complete publication and the anonymous-pull check before announcing installation.
Do not overwrite tags. Registry checks are not an atomic lock against other
writers: restrict package write access and publish only through this workflow.

## Recovery

Retain previous version tags and encrypted cold backups. Restore the previous
app version and its backup using HA's supported restore flow; verify account and
certificate reuse. Never delete /data to repair an update. 
