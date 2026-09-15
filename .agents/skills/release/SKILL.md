---
name: release
description: Cut a Faultline release end to end - preflight the repository, prepare the version bumps, push the tag, watch the four publishing targets and verify the result. Use when asked to release, cut, ship or tag a version of Faultline ("release version 0.2.0", "cut 0.1.0", "tag a release", "ship 0.3.0-rc1", "publish the next version"). Faultline only; this is a maintainer runbook, not something an application's own repository has any use for.
---

# Releasing Faultline

One tag publishes to four places at once: the GitHub release, the Homebrew
tap, GHCR and npm. None of them come back cleanly, so the order below is
preflight first, irreversible step late, verification after.

AGENTS.md says never to perform git actions unless the human asks explicitly.
A request to release is that ask, for the commits and the tag this runbook
describes and nothing else.

## 1. Settle the version

Take it from the request. It must be semver with a leading `v`: `v0.2.0`, or
`v0.2.0-rc1` for a candidate. If the request is ambiguous about which part to
bump, ask rather than guess.

A prerelease behaves differently on purpose, and this is worth saying back to
the human before going further:

- it is marked as a prerelease on GitHub, so `/releases/latest` skips it
- the Homebrew cask is not updated (`skip_upload: "auto"`)
- npm gets the `next` dist-tag, not `latest`
- the `latest` container tag does not move
- the moving `v0` action tag does not move

There is no version constant to edit. The binary takes its version from the
tag through ldflags, so `cmd/faultline/version.go` is never touched.

## 2. Preflight

Stop at the first failure and report it. Do not work around any of these.

```
git rev-parse --abbrev-ref HEAD          # must be main
git status --porcelain                   # must be empty
git fetch origin && git status -sb       # must not be behind
git tag -l <version>                     # must be empty
gh run list --workflow=ci.yml --branch=main --limit=1   # must be success on HEAD
goreleaser check                         # config still valid
```

The Release and Image workflows both refuse to publish a tag whose commit has
no green CI run, so a red `main` cannot become a release. Checking here is
still worth it: failing the gate means a tag already exists pointing at a
commit that cannot ship, and deleting a pushed tag is messier than not
pushing one.

`make test` and `make lint` are not re-run locally. CI runs both on Linux and
macOS for the commit being released, and that is the same check.

Confirm the publishing credentials exist. `gh secret list` needs a token with
repository admin scope and may well be refused; if it is, say so rather than
reporting the secrets as present:

- `NPM_TOKEN` - npm. Never yet exercised by a real release.
- `HOMEBREW_TAP_DEPLOY_KEY` - the tap. Known good.

## 3. Prepare the repository

`main` is protected and requires a pull request with a code-owner review, so
these changes go up as a PR and must be merged before the tag is pushed.
Tagging an unmerged commit would publish a version whose own repository does
not describe it.

- `.claude-plugin/plugin.json` - set `version` to the release without its
  leading `v`. Nothing generates this and nothing checks it, so it is the one
  file that silently goes stale.
- `docs/` - if anything user-visible changed and was not documented, it is
  documented now, not after.

Anything that depends on the release already existing belongs in step 8, not
here. `.github/workflows/example.yml` is the standing example: while the only
release is a candidate, `latest` resolves to nothing, so a PR that unpins it
before the tag fails its own Example check.

Open the PR, wait for CI, get it merged, then pull `main` again and re-run the
preflight on the new HEAD.

## 4. Tag

Confirm with the human before this step. Say the version, the commit it will
point at, and which of the five publishing effects above apply. This is the
irreversible part.

```
git tag -a <version> -m "<version>"
git push origin <version>
```

## 5. Watch it publish

Three workflows react to the tag. Follow them rather than assuming:

```
gh run list --limit 5
gh run watch <id>
```

- **Release** - gates on CI, builds the six archives, signs the checksums with
  cosign, attests them, pushes the cask (stable only), publishes the npm
  packages, then runs the smoke job on Linux and macOS.
- **Image** - gates on CI, builds and pushes both the default and the
  `-transparent` images for amd64 and arm64, then runs the published image and
  checks the version it reports.
- **major-tag** - runs behind the smoke job, stable only, and moves `v0`.

The npm publish is safe to re-run on its own: it skips versions already on the
registry. The job around it is not, and this is the trap. Once GoReleaser has
created the GitHub release, re-running the job fails at the Release step with
`422 already_exists` on every asset, and never reaches npm. `release.mode`
defaults to `keep-existing`, which keeps the release but still attempts every
upload; it does not skip assets that are already there.

So to retry a Release run that got past GoReleaser and failed later:

```
gh release delete <version> --yes      # keeps the tag
gh run rerun <id> --failed
```

GoReleaser then recreates the release, re-signs, re-pushes the cask and
carries on to npm, and everything in the release comes from one consistent
run. Never answer this with a new tag.

## 6. Verify

The smoke job already proves `install.sh` and `npx` against the real release,
and the Image workflow proves the container. What it cannot check from inside
the run:

```
gh release view <version>                        # notes read sensibly, 6 archives + checksums
npm view faultline-proxy dist-tags               # latest moved, or next for a candidate
gh api repos/josipmusa/homebrew-tap/commits --jq '.[0].commit.message'
git ls-remote --tags origin v0                   # stable only
```

Read the generated release notes properly. Dependabot bumps are filtered out
by `^Bump `; anything else in the history appears verbatim, so a careless
subject line is now published.

## 7. After the release

The things that could not be done earlier because they need the release to
exist. These go up as an ordinary PR.

- `.github/workflows/example.yml` - once a stable release exists, replace the
  pinned `version:` with `latest`, so the example stops naming a version that
  ages. Only after the first stable release; a prerelease does not satisfy
  `latest`.
- Anything else that resolves against `/releases/latest` or the moving `v0`
  tag, which likewise only mean something once the release has landed.

## 8. If it goes wrong

Be honest with the human about what is and is not recoverable.

- **Recoverable**: a failed workflow job, before publishing. Re-run it.
- **Recoverable with effort**: a bad GitHub release. `gh release delete` and
  the tag can go, though anyone who already fetched it has it.
- **Not recoverable**: an npm version. npm does not allow republishing a
  version, and unpublishing is restricted and disruptive. The fix is a new
  patch version, never a re-tag.
- **Not recoverable cleanly**: the cask and the container tags are overwritten
  by the next release, so the fix is again to release forward.

Releasing forward is almost always right. Say so plainly rather than
attempting a rollback that leaves the four targets disagreeing with each other.
