# Publishing `seamless-cors` for Go, npm, and GitHub Releases

Research date: 2026-07-28

## Executive recommendation

Use one full Semantic Version tag, such as `v0.4.0`, as the release identity for all three distribution channels:

1. **Go toolchain:** the pushed Git tag is the publication. There is no Go registry upload.
2. **GitHub Releases:** GoReleaser builds the native executables once and publishes the four archives plus checksums. The npm job then consumes the preserved build output independently.
3. **npm:** publish a small, unscoped `seamless-cors` launcher package and four scoped packages containing the native executables. The launcher uses exact-version `optionalDependencies`; npm filters the binary packages using their `os` and `cpu` declarations.

Keep packaging source and tests in `packaging/npm/`. Keep workflow orchestration, credentials, permissions, and ordering in `.github/workflows/release.yml`. Keep the native build matrix, archive names, checksums, SBOMs, signing, and GitHub Release artifact configuration in `.goreleaser.yaml`. Do not create a generic `pipeline/` directory and do not commit generated `dist/` output.

The repository should continue to publish downloadable macOS and Windows archives on GitHub. The binary copied into each npm platform package should be the exact GoReleaser-built executable used in the corresponding GitHub archive, not a second build.

## What “publish to Go” means

Go modules are decentralized: a module is published by putting source at the repository path named by `go.mod` and pushing a compatible version tag. Go tools fetch it directly from the repository or through a module proxy; there is no registry upload corresponding to `npm publish`. The official Go documentation describes both the [decentralized publishing model](https://go.dev/doc/modules/developing#decentralized) and the concrete sequence of tidy, test, tag, push, and optionally asking a proxy to index the version with `go list` ([Publishing a module](https://go.dev/doc/modules/publishing)).

For an executable, users should run:

```sh
go install github.com/QzCurious/seamless-cors/cmd/seamless-cors@latest
```

or pin a release:

```sh
go install github.com/QzCurious/seamless-cors/cmd/seamless-cors@v0.4.0
```

`go install` with an `@version` suffix builds a `main` package independently of the caller's current module and installs it in `GOBIN` (or the documented default); this is the supported executable-install flow ([Go Modules Reference: `go install`](https://go.dev/ref/mod#go-install)). `go get` should not be documented as the CLI installer because executable installation moved to `go install` ([Go's deprecation notice](https://go.dev/doc/go-get-install-deprecation)).

### Completed Go module prerequisite

The repository now declares its public download location:

```go
module github.com/QzCurious/seamless-cors
```

This is the conventional public module identity because a published module's
path identifies the location from which Go downloads it
([`go.mod` reference](https://go.dev/doc/modules/gomod-ref#module)). Internal
imports and both linker symbol paths now use:

```text
github.com/QzCurious/seamless-cors/internal/version.Version
```

This repository is one root module, which is Go's simplest recommended repository arrangement and allows root tags such as `v0.4.0`; moving the module into a subdirectory would instead require tags prefixed by that subdirectory ([Managing module source](https://go.dev/doc/modules/managing-source#multiple-module-source)).

The existing tags are `v0.1`, `v0.2`, and `v0.3`. Future shared releases must use all three SemVer components, starting with a tag such as `v0.4.0`. Go explicitly defines a published `v0` version as having major, minor, and patch parts ([Module version numbering](https://go.dev/doc/modules/version-numbers#v0-number)). npm also uses package name plus version as the package's unique identifier ([npm `package.json`: name and version](https://docs.npmjs.com/cli/configuring-npm/package-json/#name)).

This is an intentional clean break. Existing abbreviated tags should not be moved or repurposed. Go warns not to change a tagged version after publication because module authentication can then reject it ([Publishing a module](https://go.dev/doc/modules/publishing)).

### Version output for `go install`

The CLI reports a variable whose source default is `dev`; GoReleaser replaces
it with a linker flag. For binaries created by a user's `go install`, the
implemented fallback reads the main module version from
`runtime/debug.ReadBuildInfo`.

Go embeds module version information in binaries, and the same information is
exposed by `go version -m` and `runtime/debug.ReadBuildInfo`
([Go Modules Reference: `go version -m`](https://go.dev/ref/mod#go-version-m)).
This makes `go install ...@v0.4.0` report `v0.4.0` while local untagged builds
still report a development value.

Go installation is a source build on the user's machine. It is a distinct installation route from the signed/prebuilt bytes delivered by npm and GitHub Releases.

## Recommended npm topology

Publish five packages at the same exact npm version:

| Package | Purpose | `os` | `cpu` |
| --- | --- | --- | --- |
| `seamless-cors` | JavaScript command launcher | unrestricted | unrestricted |
| `@seamless-cors/darwin-arm64` | macOS Apple Silicon executable | `darwin` | `arm64` |
| `@seamless-cors/darwin-x64` | macOS Intel executable | `darwin` | `x64` |
| `@seamless-cors/win32-arm64` | Windows ARM64 executable | `win32` | `arm64` |
| `@seamless-cors/win32-x64` | Windows x64 executable | `win32` | `x64` |

The exact scope depends on which npm organization or user scope the maintainer controls; `@seamless-cors` is the recommended shape, not an assertion that the scope has already been reserved. npm requires an npm organization before publishing organization-scoped packages, and scoped public packages must be published with public access ([Creating and publishing scoped public packages](https://docs.npmjs.com/creating-and-publishing-scoped-public-packages/)).

The launcher exposes `seamless-cors` through its `bin` field and declares all four native packages in `optionalDependencies`. Use exact versions, not ranges, so one launcher release selects binaries from the same source tag. Each platform package declares its supported platform. npm defines `os` against `process.platform`, `cpu` against `process.arch`, and allows installation to continue when an optional dependency is unavailable or incompatible ([npm `package.json`: `os`, `cpu`, and `optionalDependencies`](https://docs.npmjs.com/cli/configuring-npm/package-json/#os)).

This produces two-stage selection:

1. npm installs only the optional binary package compatible with the host.
2. The launcher maps `process.platform + "-" + process.arch` to that package, resolves its installed path, and starts the executable with inherited standard streams and the original arguments.

The launcher must handle missing optional dependencies because users can explicitly install with `--omit=optional`; npm's documentation makes that the program's responsibility ([`optionalDependencies`](https://docs.npmjs.com/cli/configuring-npm/package-json/#optionaldependencies)). Its diagnostic should distinguish:

- an unsupported target, such as Linux while this project only builds macOS and Windows; and
- a supported target whose optional package is absent, with advice to reinstall without omitting optional dependencies.

The platform-package design is preferable here to an install script that downloads a GitHub asset. It keeps the native payload inside the package manager's lock/integrity model, avoids a second network origin during installation, and still works when install scripts are disabled. GoReleaser's native npm publisher currently uses a `postinstall` download, warns that it fails with `--ignore-scripts`, is marked alpha, and is GoReleaser Pro-only ([GoReleaser npm publisher](https://www.goreleaser.com/customization/publish/npm/)). Therefore the repository should build this small npm topology itself with the free GoReleaser build artifacts rather than adopt that publisher.

## Where publishing code belongs

Recommended source layout:

```text
.github/workflows/{ci.yml,release.yml}
.goreleaser.yaml
packaging/npm/
  package.json                  # private Nub runner
  publish.ts                    # the only release interface
  publish.test.mjs
  launcher/
    package.json
    bin/seamless-cors.js
  launcher.test.mjs
```

The private root pins Nub and exposes the packaging commands, but is never
published. Nub runs TypeScript directly, implements npm-compatible
`pack`/`view`/`publish`, and supports npm trusted publishing through GitHub
OIDC in the released v0.6.0 engine
([Nub package-manager overview](https://github.com/nubjs/nub/blob/v0.6.0/README.md#package-manager--nub-install),
[publishing implementation](https://github.com/nubjs/nub/blob/v0.6.0/vendor/aube/docs/package-manager/publishing.md)).
The folder remains named `packaging/npm/` because it defines packages for the
npm registry and npm package format; Nub is the tool used to build and publish
them.

Responsibilities remain explicit:

- **`packaging/npm/`:** the launcher and one publisher that generates the four native packages from GoReleaser output.
- **`.goreleaser.yaml`:** Go build targets, `CGO_ENABLED`, linker flags, reproducibility inputs, archive composition/naming, checksum, optional SBOM/signing, and GitHub Release settings.
- **`.github/workflows/release.yml`:** event trigger, least-privilege permissions, GitHub Release publication, artifact handoff, and the independently retryable npm job.
- **Temporary directories:** generated npm package trees. Never source-controlled.

GoReleaser documents `dist/artifacts.json` as the supported machine-readable way for integrations to locate artifacts; it explicitly says internal target directory names are not guaranteed ([GoReleaser build documentation](https://goreleaser.com/customization/builds/builders/go/#a-note-about-directory-names-inside-dist), [artifact schema](https://goreleaser.com/customization/general/artifacts/)). `publish.ts` parses that file instead of globbing private GoReleaser directory names.

A generic `pipeline/` folder would blur configuration, package source, and generated output. A specifically named `packaging/npm/` folder answers the useful part of “dedicated pipeline folder” while leaving the actual CI pipeline in GitHub's conventional workflow location.

## Tag-driven release flow

The current repository already triggers `.github/workflows/release.yml` on `v*` tags and uses GoReleaser for four targets. Keep the tag trigger, but tighten it to full SemVer or explicitly validate the tag before any write:

```text
vMAJOR.MINOR.PATCH
vMAJOR.MINOR.PATCH-prerelease
```

Derive npm `0.4.0` by removing only the leading `v` from Go/Git `v0.4.0`. Never independently edit five package versions.

Recommended flow:

1. **Pre-tag CI on the commit:** run Go tests, cross-build checks, and the focused npm publisher/launcher tests.
2. **Build and publish once with GoReleaser:** produce the four executables, archives, checksum file, and GitHub Release ([GoReleaser releases](https://goreleaser.com/customization/publish/scm/)).
3. **Preserve `dist/` as a workflow artifact:** the dependent npm job receives the same binaries without rebuilding.
4. **Generate all five npm packages in a temporary directory:** `publish.ts` validates the tag and `dist/artifacts.json`, injects one version, and pins the launcher's optional dependencies exactly.
5. **Validate each generated package with `nub pack --dry-run`.**
6. **Publish all four platform packages first, then the launcher.** This prevents a visible launcher from pointing at native packages that do not exist.
7. **Confirm Go discovery:** request `GOPROXY=proxy.golang.org go list -m github.com/QzCurious/seamless-cors@v0.4.0`, as recommended in Go's publishing steps ([Publishing a module](https://go.dev/doc/modules/publishing)).

GitHub Actions jobs run in parallel unless connected by `needs`; a failed prerequisite skips dependent jobs, which is the correct mechanism for enforcing build → platform packages → launcher → finalize ordering ([GitHub workflow syntax: `needs`](https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax#jobsjob_idneeds)). Workflow artifacts can pass the single build output between jobs, rather than rebuilding it ([GitHub workflow artifacts](https://docs.github.com/en/actions/concepts/workflows-and-actions/workflow-artifacts)).

### There is no cross-registry transaction

Pushing the tag immediately publishes the Go module in the decentralized Go sense, before GitHub Actions completes. npm publishes are also individually immutable: once a package name/version has been used it cannot be replaced, even after unpublishing ([npm unpublish policy](https://docs.npmjs.com/policies/unpublish/)). GitHub and npm cannot be committed atomically.

The simpler workflow accepts that the GitHub Release may be public while npm
publication is incomplete. The npm job checks each `package@version` before
publishing and skips versions already present, so GitHub's **Re-run failed
jobs** button can resume a partial npm release without rerunning GoReleaser.
It deliberately does not compare tarball integrity on retries; if a published
version is wrong, release a new patch version.

For prereleases, publish npm with a non-`latest` dist-tag such as `next`; do not let a prerelease advance `latest`. Final releases use `latest`.

## Retaining downloadable GitHub binaries

The present GoReleaser matrix already builds `darwin/amd64`, `darwin/arm64`, `windows/amd64`, and `windows/arm64`, packages macOS as `.tar.gz` and Windows as `.zip`, and emits a checksum file. Keep those archives and friendly names. GoReleaser is designed to attach target archives to GitHub Releases ([quick start](https://www.goreleaser.com/getting-started/quick-start/)) and upload checksums for validation ([checksums](https://goreleaser.com/customization/package/checksum/)).

The npm preparation step should take the raw binary from the same `dist/artifacts.json` build entry that feeds the archive. That gives:

```text
tagged source
    └── one GoReleaser build per target
          ├── GitHub archive + checksum
          └── npm platform package
```

Do not make npm install from GitHub at runtime, and do not compile a second npm-specific executable.

## Authentication, provenance, and signing

### npm

Use Nub's npm trusted-publishing implementation from a GitHub-hosted runner.
It exchanges the job's OIDC identity for a short-lived, package-scoped npm
token, avoiding a permanent npm write token. The workflow needs
`id-token: write`; every package's `repository` field must exactly identify
this public GitHub repository. Pin both Nub and the official setup action:
`nubjs/setup-nub` provisions Nub and Node and supports registry configuration
([setup-nub v0.4.0](https://github.com/nubjs/setup-nub/tree/v0.4.0),
[npm trusted publishing](https://docs.npmjs.com/trusted-publishers/)).

Trusted publisher configuration is per package and names the GitHub owner, repository, workflow filename, and optional GitHub environment. Because npm's setup begins from each existing package's settings page, bootstrap the five package names with a controlled first publish, then configure the same trusted `release.yml` publisher on all five and revoke the bootstrap automation token. npm's own migration guidance says to verify trusted publishing and then revoke old automation tokens ([Trusted publisher migration](https://docs.npmjs.com/trusted-publishers/#migration-tip)).

For a higher-assurance release, bind the trusted publisher to a GitHub `npm` environment with selected tag rules and a required reviewer. GitHub environments can restrict deployment tags and require approval before a publishing job runs ([GitHub deployments and environments](https://docs.github.com/en/actions/reference/workflows-and-actions/deployments-and-environments)).

Publish with `nub publish --provenance`. Nub uses the ambient CI OIDC identity
to attach a Sigstore SLSA provenance bundle, while npm trusted publishing
authenticates the registry write. Provenance links the package to its source
and build instructions; it does not prove the code is safe
([Nub provenance support](https://github.com/nubjs/nub/blob/v0.6.0/vendor/aube/docs/package-manager/publishing.md#provenance),
[npm provenance](https://docs.npmjs.com/generating-provenance-statements/)).

### GitHub Release artifacts

Keep SHA-256 checksums. Add GitHub artifact attestations for the downloadable archives or checksum manifest using `actions/attest`; GitHub documents `contents: read`, `id-token: write`, and `attestations: write` for binary provenance, and consumers can verify with `gh attestation verify` ([Using artifact attestations](https://docs.github.com/en/actions/how-tos/secure-your-work/use-artifact-attestations/use-artifact-attestations#generating-build-provenance-for-binaries)).

If available for the repository, enable immutable releases. GitHub then locks the release's tag and assets and automatically creates a release attestation containing the tag, commit, and release assets ([Immutable releases](https://docs.github.com/en/code-security/concepts/supply-chain-security/immutable-releases)).

Optionally add an SBOM for the archives. Attestations and SBOMs establish origin and contents, not absence of vulnerabilities; GitHub explicitly makes this distinction ([Artifact attestations](https://docs.github.com/en/actions/concepts/security/artifact-attestations)).

Use least-privilege job permissions, and pin third-party actions (including GoReleaser's action) to reviewed full commit SHAs in the release workflow. GitHub explains that a tag can move while a full SHA fixes the exact action code executed ([GitHub Actions security guidance](https://docs.github.com/en/code-security/tutorials/secure-your-organization/protect-against-threats#harden-your-github-actions-workflows)).

### Native operating-system trust

Provenance is not a replacement for platform code signing. The README currently warns that Windows releases are unsigned. Plan macOS signing/notarization and Windows Authenticode signing as a separate hardening milestone. GoReleaser supports detached artifact/checksum signing ([signing](https://goreleaser.com/customization/sign/sign/)) and documents macOS executable signing/notarization ([notarization](https://www.goreleaser.com/customization/sign/notarize/)). Detached attestations and OS-native signatures solve different trust problems.

## Concrete target architecture for this repository

### Source changes before the first shared release

1. Keep the completed Go module path, internal imports, linker symbol paths,
   and module-aware `version` fallback.
2. Keep one launcher template and generate the four platform packages only during publication.
3. Have `publish.ts` parse `dist/artifacts.json`; never glob GoReleaser's internal build directories.
4. Add focused launcher and publisher tests to normal CI.
5. Update English and Traditional Chinese READMEs with three supported installation routes:
   - `npm install --global seamless-cors`
   - `go install github.com/QzCurious/seamless-cors/cmd/seamless-cors@latest`
   - GitHub Release archive download
6. Cut the next release with a full tag such as `v0.4.0`; do not continue the abbreviated tag pattern.

### Recommended publication state machine

```text
tag pushed (Go source is published)
  → test
  → GoReleaser builds once and publishes the GitHub Release
  → preserve dist/ for the dependent npm job
  → generate and validate five temporary npm packages
  → publish/skip four npm platform packages with Nub
  → publish/skip the npm launcher with Nub
  → verify Go proxy discovery and all public endpoints
```

This preserves the repository's existing Git-tag release experience, adds both Go and npm installation without duplicating native builds, keeps GitHub binaries downloadable, and isolates ecosystem packaging without turning the repository into a generic pipeline framework.
