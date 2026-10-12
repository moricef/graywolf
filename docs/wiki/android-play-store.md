# Android release pipeline (Play Store)

How the Graywolf APRS Android app gets built, signed, and shipped to the
Google Play Store. This page is the *navigation + intent* layer; the
authority for build mechanics is
[`../../.github/workflows/android.yml`](../../.github/workflows/android.yml)
and [`../../android/app/build.gradle.kts`](../../android/app/build.gradle.kts).

## One-paragraph overview

`make bump-point` (or `bump-minor`) is the only manual action a release
needs. It writes `VERSION`, commits, tags `vX.Y.Z`, and pushes. The tag
push fires two workflows in parallel: `release.yml` (goreleaser:
Linux/macOS/Windows binaries, Docker, `.deb`/`.rpm`, NSIS) and
`android.yml`. The Android workflow builds a release-signed `.aab` +
`.apk`, attaches both to the GitHub Release goreleaser created, and
uploads the `.aab` to the Play **Closed Testing** track (Play's `alpha`
track), where the ~15-person private beta lives. Promotion onward to
**Open Testing** (the `beta` track) is a separate, deliberate manual step
(`gh workflow run android.yml --field version=X.Y.Z --field track=beta`).
Production is never targeted -- the service account lacks the permission.
**One caveat right now:** the app is still a draft in the Play Console, so
the upload lands as a *draft* release and a human has to roll it out
before testers see it -- see [Release status and the draft
app](#release-status-and-the-draft-app).

## Workflow triggers and jobs (`android.yml`)

| Trigger | Jobs that run | Result |
|---|---|---|
| `pull_request` -> main | `build` | Unsigned debug APK artifact (sanity check) |
| `push` -> main | `build` | Same; catches breakage before a tag |
| `push` tag `v*` | `build` + `release-sign` | Signed `.aab`+`.apk` on the GH Release; `.aab` uploaded to Play Closed Testing (`alpha`) |
| `workflow_dispatch` (no `version`) | `build` | Manual build re-run |
| `workflow_dispatch` (`version=X.Y.Z`) | `promote-to-closed` | Promotes that release's `.aab` from Closed Testing (`alpha`) to the chosen track (default `beta` = open testing) |

`build` is skipped on `workflow_dispatch` (a promote run doesn't need a
fresh APK). `release-sign` only runs on tags. `promote-to-closed` only
runs on `workflow_dispatch` with a `version`.

## Signing (Play App Signing)

Graywolf uses **Play App Signing**: Google holds the real app-signing
key; the developer holds only an *upload* key. Losing the upload key is
recoverable via a Play Console reset; the app key can't be lost because
Google holds it.

- Upload keystore: `1Password -> "Graywolf Android Upload Keystore"`
  (alias `graywolf-upload`, PKCS12). SHA-256 fingerprint is stored
  alongside; compare it against the `apksigner --print-certs` line the
  `release-sign` job prints to confirm a build was signed with the right
  key.
- Gradle reads the keystore from env (see
  `android/app/build.gradle.kts` `signingConfigs`):
  `GRAYWOLF_KEYSTORE_BASE64` (CI) or `GRAYWOLF_KEYSTORE_PATH` (local),
  plus `GRAYWOLF_KEYSTORE_PASSWORD`, `GRAYWOLF_KEY_ALIAS`,
  `GRAYWOLF_KEY_PASSWORD`. With none set, the release build emits an
  *unsigned* APK -- which is why PR-build CI (no secrets) stays green.
- Those four values are GitHub Actions secrets on `chrissnell/graywolf`.

## Play upload service account

The tag-push upload (and the closed-beta promotion) authenticate to the Play
Developer API with a Google Cloud service account.

- GCP project `graywolf-play-upload`, service account `play-upload@...`,
  with the **Google Play Android Developer API** enabled.
- In Play Console -> Users and permissions, the account is granted
  **"Release apps to testing tracks"** on `com.nw5w.graywolf`. Production
  release permission is intentionally NOT granted, so CI can never ship
  to production.
- The JSON key is stored in `1Password -> "Graywolf Play Upload Service
  Account JSON"` and as the GH secret `GRAYWOLF_PLAY_SERVICE_ACCOUNT_JSON`.
- Both Play steps gate on that secret via a job-level
  `env: HAVE_PLAY_SA: ${{ secrets.GRAYWOLF_PLAY_SERVICE_ACCOUNT_JSON != '' }}`
  and `if: env.HAVE_PLAY_SA == 'true'`. (The `secrets` context is not
  allowed directly in `if:` -- it must be hopped through `env`.) When the
  secret is absent the steps skip, not fail.
- **Propagation:** after granting permission in Play Console, it can take
  up to 24h to take effect. Check without burning a release:
  `make android-play-check JSON=path/to/service-account.json` -- HTTP 200
  means ready, 403 means not propagated yet, 401 means a bad key.

## Release status and the draft app

Whether Play *commits* a release or parks it for a human is the `status:`
on the upload step. It is driven by a repo variable so the value can change
without editing the workflow:

```yaml
status: ${{ vars.GRAYWOLF_PLAY_RELEASE_STATUS || 'draft' }}
```

Unset or empty falls back to `draft`. Check Settings -> Secrets and
variables -> Actions -> Variables for the value in force; the run log also
prints it ("Play release status: ...") and emits a `::notice::` when a
build parked as a draft.

**Why the default is `draft`.** `com.nw5w.graywolf` has never completed its
first publish in the Play Console, so it is a *draft app*, and the Play
Developer API refuses any release on it that isn't itself a draft:

```
Only releases with status draft may be created on draft app.
```

With `status: completed` this failed the upload step on every tag from
v0.14.11 through v0.14.14 (GH #619). The rejection lands on `edits.commit`
-- the `.aab` bytes upload fine and the log even reaches "Committing the
Edit" first -- and a Play edit is transactional, so a failed commit applies
*nothing*. Those versionCodes were never consumed, which is why re-tagging
a failed version is safe here and doesn't hit the duplicate-versionCode
problem the [retag section](#retag-flow-on-ci-failure) warns about.

With `draft`, the commit succeeds and the build sits on the alpha track as
a draft release that someone rolls out from Play Console -> Closed testing.
Testers can install the signed `.apk` off the GitHub Release in the
meantime.

**The same variable covers promotion.** A promotion creates a release on
the *target* track, so `promote-to-closed` hits the identical rule.
`android.yml` passes the variable to fastlane as `PLAY_RELEASE_STATUS`,
which [`../../fastlane/Fastfile`](../../fastlane/Fastfile) reads for
`release_status` (falling back to `completed` for a local `bundle exec
fastlane promote`). One knob, both paths. Promotion still needs the
versionCode to actually be on `alpha`, so a tag whose upload step failed
has nothing to promote.

**To get back to hands-off publishing:** finish the app's first publish in
the Play Console -- store listing, content rating, data safety,
target audience and the rest of the "App content" checklist, then a first
rollout -- and set `GRAYWOLF_PLAY_RELEASE_STATUS` to `completed`. The next
tag picks it up; no code change or release needed.

## Version derivation

`android/app/build.gradle.kts` reads the repo-root `VERSION` file at
configure time, so `make bump-*` cascades into Android automatically:

- `versionName` = the `VERSION` string (e.g. `0.13.8`).
- `versionCode` = `M*1_000_000 + m*10_000 + p*100` (so `0.13.8` ->
  `130800`), leaving 100 slots per patch for hotfix re-uploads. Play
  requires `versionCode` to increase monotonically across uploads.

## Release notes ("What's new")

The closed-testing upload ships a per-locale "What's new" note so testers
see what changed. It is derived from the same
[`pkg/releasenotes/notes.yaml`](../../pkg/releasenotes/notes.yaml)
changelog that drives the in-app popup -- no second place to edit.

- `cmd/play-whatsnew` renders the note for the release version to plain
  text: title first, then the body with markdown links flattened to their
  text and bold/italic markers stripped, truncated to Play's 500-char
  per-locale limit at a sentence (else word) boundary.
- The `release-sign` job runs it into `staging/whatsnew/whatsnew-en-US`
  and points the upload's `whatsNewDirectory` there.
- It exits non-zero if `notes.yaml` has no entry for the tagged version,
  so a missing changelog fails the release loudly rather than shipping a
  blank note. (The `make bump-*` flow already refuses to tag without an
  entry, so this is a backstop.)
- Promotions (`promote-to-closed`) carry the existing release's notes
  forward; `fastlane supply` runs with `skip_upload_changelogs`, so the
  note is set once at the closed-testing upload.

## Tracks

Play's track IDs are fixed: `internal`, `alpha` (closed testing), `beta`
(open testing), `production`. A custom Console display name (e.g.
"graywolf-beta") does not change the API id -- the closed track is still
addressed as `alpha` by `supply` / `upload-google-play`.

- **Closed testing (`alpha`)** -- uploaded on every `v*` tag (as a draft
  release while the app is a draft, see [Release status and the draft
  app](#release-status-and-the-draft-app)); this is where the ~15-person
  private beta lives. Add testers in Play Console -> Closed testing ->
  Testers (email list); they install via the track's opt-in URL.
- **Open testing (`beta`)** -- public opt-in. A vetted closed build is
  promoted here with the manual `promote-to-closed` workflow
  (`--field track=beta`).
- **Internal** -- no longer auto-populated. Still reachable manually for a
  quick dev/team smoke check (`--field track=internal`).
- **Production** -- not enabled. The service account lacks the permission
  and the workflow never targets it.

## Retag flow on CI failure

If a workflow fails *after* the tag is pushed, follow the retag contract
in [`../../CLAUDE.md`](../../CLAUDE.md): fix the cause, delete and re-tag
the same version (`git tag -d vX.Y.Z && git push origin :refs/tags/vX.Y.Z`,
commit fix, re-tag, push), and **do not rewrite the release note**. A
re-tag re-runs both workflows. Note Play rejects a duplicate
`versionCode`, so if the upload to Closed Testing (`alpha`) already
*succeeded* and a later step failed, a plain re-tag's upload step will
conflict -- in that case bump to a new patch instead. A failure of the
upload step itself does not burn the versionCode: the step's Play edit
never commits, and an uncommitted edit applies nothing (see [Release
status and the draft app](#release-status-and-the-draft-app)).

## armv6 (Pi 1 / Pi Zero) -- temporarily dropped

Unrelated to Android, but it rides the same release: the Linux armv6
cross-build is currently removed from `release.yml` (a cross-rs upstream
glibc/libudev mismatch). Pi 1 / Pi Zero / Pi Zero W users skip these
releases until it's restored. Tracking + restoration options are in the
session memory `project_followup_armv6_release`; search `FOLLOWUP` in
`release.yml`, `.goreleaser.yml`.

## Screenshots and store graphics

`make android-screenshots` drives the SPA in Android mode against a
local graywolf launched with `-demo` (canned Salt Lake-metro stations +
counters), capturing tablet + phone screenshots and rendering the
512x512 icon + 1024x500 feature graphic. Output under `scratch/ss-work/`
(gitignored -- holds rendered assets only). The harness fakes the
`GraywolfWebInterface` bridge so `Platform.kind === 'android'` and the
SPA renders the Android-filtered UI. See
[`../../scripts/screenshots/`](../../scripts/screenshots/) and the
`-demo` flag in `android/app/build.gradle.kts` / `pkg/demoseed`. Uploading
the assets to Play is manual (or a future `fastlane supply` step).

## What's hidden on Android

The SPA hides surfaces that don't work on Android (sidebar + route map):
Actions (command handlers can't `execve` under the W^X sandbox), AGW,
Simulation, and the `/login` flow (Android authenticates via the
per-launch WebView bearer token). See `web/src/components/Sidebar.svelte`
(`HIDDEN_ON_ANDROID`) and `web/src/App.svelte` (route map).
