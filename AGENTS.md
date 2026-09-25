# Repository instructions

These instructions apply to the whole repository. Read `CLAUDE.md` and
`docs/wiki/README.md` before changing a cross-system workflow.

## Release lanes

Before changing a version, creating a tag, or publishing anything, identify
the release lane. Do not infer a stable upstream release from a request to
prepare or publish Graywolf while working on the RXT fork.

### RXT fork test builds

The normal publication from `moricef/graywolf` on the
`feature/rxt-telemetry` branch is a GitHub **pre-release**, not a stable
Graywolf release.

- Target repository: `moricef/graywolf`.
- Keep the repository's current base `VERSION`. Do not run `make bump-point`,
  `make bump-minor`, or `make bump-beta` for an RXT test build.
- Do not add a versioned entry to `pkg/releasenotes/notes.yaml`; that file is
  for versioned Graywolf releases shown in the application.
- Use the exact commit being packaged. Let `<sha8>` be the first eight
  hexadecimal characters of its full commit SHA.
- Tag: `rxt-<sha8>`.
- GitHub release title: `Graywolf RXT test build <sha8>`.
- GitHub release state: pre-release (`prerelease=true`), never "Latest".
- Package filenames retain the current base Graywolf version. The RXT commit
  is identified by the tag, title, release notes, and embedded commit ID.
- Write operator-facing release notes that state the tested behavior,
  configuration or restart requirements, and known limits. Mention the
  relevant documentation at the tagged commit.
- Before publishing, show the user the exact tag, title, release state, and
  release-note text and obtain explicit approval.

Validate an RXT test build before tagging it:

1. Ensure the worktree contains no unintended tracked changes. Preserve
   user-owned untracked files and directories, including `local-systemd/`
   and `packages/`.
2. Push the intended commit to the fork branch.
3. Run `.github/workflows/release.yml` with `workflow_dispatch` on that
   branch and verify that the workflow's `headSha` is the intended full SHA.
4. Wait for every build job to succeed.
5. Download the `snapshot-packages` artifact and verify
   `sha256sum -c checksums.txt` before publishing.
6. Confirm that `rxt-<sha8>` does not already exist locally or remotely.
7. Create and push the tag on the validated commit, then create the GitHub
   pre-release and attach exactly the verified snapshot artifacts.
8. Re-read the published release and verify its tag, title, pre-release flag,
   target commit, notes, asset list, and checksums.

RXT test builds must not publish OCI images and must not create or move the
GHCR `latest` tag. The `rxt-*` tag does not trigger the `v*` release workflow.
Use the snapshot artifacts from `workflow_dispatch`; do not change the
workflow trigger merely to publish an RXT test build.

### Versioned Graywolf releases

A `vX.Y.Z` release is a stable/versioned Graywolf release. Create one only
when the user explicitly approves that release lane and the exact version.
Then follow the release workflow in `CLAUDE.md`, including user approval of
the release note, the appropriate `make bump-*` target, and monitoring every
workflow to completion.

Before running a bump target, state all external destinations that it will
change: GitHub repository, Git tag, GitHub Release, package coordinates,
GHCR image names/tags (including `latest`), Android publication if enabled,
and AUR metadata. Verify that each destination belongs to the intended
release lane.

## Release correction and cleanup

Deleting a GitHub Release is not sufficient cleanup. For an erroneous
publication, resolve the exact targets first and, when the user authorizes
removal, handle every surface created by the publication:

- GitHub Release;
- local and remote Git tags;
- GHCR architecture tags, manifest tag, and any `latest` tag moved by it;
- generated version-bump commit (revert it; do not rewrite shared history);
- other uploaded artifacts or package metadata.

After cleanup, verify each surface independently and report anything that
could not be removed. Never claim that a release has been fully removed while
its images or aliases remain published.
