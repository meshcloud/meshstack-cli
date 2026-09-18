# AGENTS.md — meshStack CLI

<role>
You are an expert Go engineer working on the meshStack CLI: the `meshstack` binary, and the Go
client for the meshStack API that the
[meshStack Terraform provider](https://github.com/meshcloud/terraform-provider-meshstack) imports as
a library. This file is the always-on source of truth for both AI agents and humans.
</role>

> **This repository is public.** Write everything here so an external contributor with no meshcloud
> access can follow it. Tag meshcloud-internal shortcuts clearly as internal, and never let
> understanding a rule *depend* on them.

A relative path like `../meshfed-release` refers to a **sibling checkout**: meshcloud developers
clone the `meshcloud` org flat, so every repository in it is a sibling of this one. Write cross-repo
paths that way rather than bare, so they resolve as written.

<rules id="keep-this-lean">
**This file is loaded into every session, so keep it short.** A rule earns a place here only if it
has no closer home. Everything else belongs next to what it governs:

| Belongs in | Rather than here |
|---|---|
| `.golangci.yml` | Which dependency may reach which package |
| `Taskfile.yml` | What a command does |
| A doc comment on the code | Why a package, type or command is built the way it is |
| The file that holds the setting | Why the setting has that value: `flake.nix`, `go.mod`, `.goreleaser.yml`, `Dockerfile` |
| A skill | A procedure long enough to need its own steps, loaded only when the work starts |

Restating a rule in two places is worse than leaving it in one: the copies drift, and neither one
looks stale.
</rules>

## Naming

- **`meshstack`** — the binary, so every invocation reads `meshstack auth login`.
- **meshStack CLI** — the product name, used in prose and docs.
- `github.com/meshcloud/meshstack-cli` — the repository and Go module.

Everything published carries the repository name — the release archives, the checksum file and the
container image are all `meshstack-cli` — while the binary inside them is `meshstack`.

The binary gets its name from its directory, `cmd/meshstack`, which is what `task build` relies on.
**Do not add a `main.go` at the repository root**; that would name the binary after the module.

## Package layout

| Path | Holds |
|---|---|
| `cmd/meshstack/` | `package main`: `main()` and the root command. The only main package. |
| `cmd/<subcommand>/` | One package per subcommand of the cobra command tree. |
| `cmd/internal/` | What the command tree shares: flags, the session it resolves, the version. |
| `cmd/internal/testacc/` | The suite that drives the built binary against a live meshStack. |
| `pkg/` | `auth`, `io`, `profile` and `setting`, each wrapping the `internal/` package of the same name. |
| `client/` | The meshStack API client, imported as a git subtree. |
| `internal/` | Everything else. The `depguard` rules in `.golangci.yml` say which package may import which. |

`pkg/` and `client/` are the two import paths the Terraform provider's own `depguard` rule allows,
so a rename or a signature change in either breaks it. Go's internal rule closes `internal/` to the
provider, which is what makes the indirection through `pkg/` worth its cost.

<rules id="command-tree">
To add a command, put it in `cmd/`, where **the package name is the subcommand and the file name is
the leaf command**: `cmd/auth/login.go` holds `meshstack auth login`. The package exports a `New`
function returning its `*cobra.Command`, and the parent's constructor wires it in with `AddCommand`.

`cmd/meshstack` is the one exception, and is not a subcommand: it is the binary's `package main`,
holding `main()` and the root command together.

Four rules hold the tree together:

- Register a command **explicitly in its parent's constructor, never from `init()`**.
- A command with a **top-level shortcut** — `meshstack login` for `meshstack auth login` — is
  registered twice by calling its constructor twice. `Aliases` cannot do this.
- A constructor keeps its own flag targets in **locals captured by the closure**. The four
  persistent flags in `cmd/internal` are the exception: `SettingSources` reads their values back,
  so they are package-level vars.
- A **parent command sets `RunE` as well as `Args`**.
</rules>

## Dependency policy

The CLI runs on the standard library and a short list of external dependencies, with `testify` in
tests. `go.mod` is that list.

**The `depguard` rules in `.golangci.yml` are the policy**, not only its enforcement: each rule
confines a dependency to a smaller area than the module, so widening a boundary is a deliberate edit
rather than a lint fix. Adding a dependency therefore means editing both files, and the second edit
is where you argue for it.

<rules id="client-package">
`client/` is a **git subtree** of
[terraform-provider-meshstack](https://github.com/meshcloud/terraform-provider-meshstack). Carry
changes across with `git subtree`, not by copying files.

**A pull takes a split, not a branch.** The subtree's history carries the files at the *repository
root*, while in the provider the same files sit under `client/`, so pulling the provider's `main`
directly fails with *"refusing to merge unrelated histories"*. Split first, in a checkout of the
provider:

```shell
cd ../terraform-provider-meshstack
git subtree split --prefix=client -b client-split main

cd ../meshstack-cli
git subtree pull --prefix=client ../terraform-provider-meshstack client-split
git subtree push --prefix=client ../terraform-provider-meshstack <branch>
```

Reading the pre-import history takes both paths, since the split history carries the files at the
repository root and the import merge re-roots them under `client/`:

```shell
git log -- client/client.go client.go   # a path-limited log from client/ alone stops at the merge
git blame client/client.go              # traverses the merge on its own
```

**`client/` does not log in.** `client.Authorization` produces a bearer token and replaces one that
came back 401; resolving a credential, minting a token, caching it and refreshing it is `pkg/auth`.
Both front ends build their client through `auth.Session.Client`, so the endpoint and the
authorization always agree with what was resolved. Do **not** add a login exchange anywhere else: a
second one gets a static token and starts returning 401 once it expires, and for a browser login it
would end the user's session.

**`client/` does not own HTTP.** The client, the request options and the retry policy are
`internal/http`, one directory above, because `internal/oidc` and `pkg/auth` need them and Go's
internal rule closes `client/internal` to both. Its names carry no `Http` prefix — the package is
what says that — so it reads `http.Client`, `http.Error`, `http.NewClient`.

**`net/http` is always imported as `gohttp`**, which `importas` in `.golangci.yml` settles. That
leaves the plain name to `internal/http`, the package a meshStack call goes through, and `net/http`
to the status and method constants and to the loopback server. The `forbidigo` rule matches on the
type rather than on the written name, so it catches `gohttp.Client` and leaves `http.Client` alone.

**Logging goes through `slog`'s default logger**, on which each front end installs its own handler:
`cmd/meshstack` a `charmbracelet/log` one, the Terraform provider a `tflog` bridge. A handler
installed that late imposes two rules on every log call, and `internal/http/logging.go` states them.
</rules>

## Always-on rules

<rules id="always-on">

- **Self-explanatory code.** Move the fact into a name, a type, a check or a test: the compiler and
  CI keep those true, while a comment goes stale in silence. A comment stays only when you could not
  have written it by reading the code, and only when it changes what the reader does — the reason
  for a decision, a rejected alternative, an external constraint with its source, a link to code
  this must stay in step with. (*meshcloud-internal*: the `self-explanatory-code` skill of
  `../meshfed-release`.)
- **Lint and format only via `task lint`**, and **never run `gofmt` or `go vet` separately** — a
  differently built gofmt enforces different formatting. A `PostToolUse` hook in
  `.claude/settings.json` formats every `.go` file an agent writes, so it rarely reaches the gate.
- **Conventional Commits** for messages (`feat:`, `fix:`, `docs:`, `chore:`, `feat!:` for breaking).
- **Stress-test a plan before writing code.** For any non-trivial change, walk each branch of the
  decision tree and settle every open question with a recommended answer first. (*meshcloud-internal*:
  the `grill-me` skill of `../meshfed-release`.)

</rules>

## Commands

Everything runs through the Taskfile, inside `nix develop`. **`task --list` is the list.**

The Go version is pinned in **three** places that must agree — `go.mod`, `flake.nix` and the
`Dockerfile`'s base image, each of which says so at the pin — and is held in lock-step with the
Terraform provider's own pin.

`flake.nix` also builds the binary — `nix build .#meshstack` — and exports it as
`packages.<system>.meshstack` and as `overlays.default`, so another flake can put it in a dev shell.

## Acceptance tests

This repository is a **meshStack satellite**: `cmd/internal/testacc/` drives the built binary
against a live backend, and a whole meshStack only exists in the *meshcloud-internal* mono repo, so
that repository runs the suite and nothing here does.
`.github/workflows/test-acceptance.yml` asks for the run, and `meshstack-satellite.gradle` is
everything the run reads from here. The other half of the lane belongs to `../meshfed-release` and
changes without us, so read it there, in `satellite-suites.md` of the `acceptance-testing` skill,
rather than trusting a copy here.

## Authentication

`MESHSTACK_ENDPOINT`, `MESHSTACK_API_KEY` and `MESHSTACK_API_SECRET`, with `MESHSTACK_API_TOKEN` as
an alternative to the key and secret pair, plus `MESHSTACK_PROFILE`, `MESHSTACK_WORKSPACE`,
`MESHSTACK_CONFIG_DIR` and `MESHSTACK_SKIP_VERSION_CHECK`.

**Each one is declared once, in the domain package it belongs to**, as a `setting.Setting[T]` whose
`EnvKey` is both the variable name and the setting's identity. `internal/setting` resolves it from
the sources it is given, and each front end contributes exactly one source over its own flags or
block attributes. **No front end assembles a sentence out of an imported name**: every message that
has to mention a variable is produced in the package that owns the declaration. The Taskfile reads a
git-ignored `.env` for local runs.

## Releasing

Pushing a `v*` tag runs goreleaser, which publishes the archives and checksums, and then builds the
container image for the same tag. The image goes to GHCR only, as
`ghcr.io/meshcloud/meshstack-cli`, and its entrypoint is the `meshstack` binary, so the image takes
the same arguments a local `meshstack` does. A push to `main` refreshes `:main`, so an image exists
before the first release does.

<rules id="release-version">
The version reaches the binary through an ldflag on
`github.com/meshcloud/meshstack-cli/cmd/internal.Version`, set in **three places that must agree**:
`.goreleaser.yml`, the `Dockerfile` and `flake.nix`, all of which say so at the ldflag. The linker
ignores an `-X` whose path does not resolve and warns about nothing, so a stale path is silent.

A build with no ldflag falls back to what the go command stamped itself, which `cmd/internal` reads
from `debug.ReadBuildInfo`: the module version for `go install <path>@<version>`, and since Go 1.24
a pseudo-version derived from the commit for a build inside a git checkout. Only a source tree with
no VCS information, such as an extracted archive, reports `dev`. Check with `meshstack --version`
after `task release:snapshot`.
</rules>

Pin every GitHub Action by commit SHA with the version in a trailing comment, as the existing
workflows do.
