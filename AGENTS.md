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

- **`meshstack`** — the binary, so every invocation reads `meshstack login`.
- **meshStack CLI** — the product name, used in prose and docs.
- `github.com/meshcloud/meshstack-cli` — the repository and Go module.

Everything published carries the repository name — the release archives, the checksum file and the
container image are all `meshstack-cli` — while the binary inside them is `meshstack`.

The binary gets its name from its directory, `cmd/meshstack`, which is what `task build` relies on.
**Do not add a `main.go` at the repository root**; that would name the binary after the module.

## Development

**Load the `development` skill before you write or review Go code here.** It holds the package
layout, the split of a command into a front end in `cmd/` and its logic in `internal/`, the command
tree, the dependency policy, and what `client/`, `internal/http` and the settings must keep.

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
- **Test against a live meshStack.** A command's behaviour is tested by an acceptance test in
  `cmd/internal/testacc`, and a `client/` method by the provider's or the CLI's acceptance tests, so
  request plumbing gets no unit test against a fake meshStack. A unit test pins only delicate
  behaviour an acceptance test cannot reach reliably — concurrency, locking, token refresh, the
  precedence of settings or credentials, parsing edge cases, exit codes, guards — and never
  restates the code: many small unit tests cost more to keep than they catch. Write a test as few
  top-level scenarios whose `t.Run` steps build on each other and share one setup.
- **Behaviour goes on its type.** A function whose main parameter is a type of its own package is a
  method of that type: `status.IsTerminal()`, not `isTerminal(status)`. A package-level function
  needs a reason: it is a constructor, it has no natural receiver (`auth.Login`), or it has type
  parameters, which a Go method cannot have. Inline a helper of a few lines that has one caller. A
  string or map that several functions pass around gets a named type with methods, as
  `internal.KindCommand` is.
- **Conventional Commits** for messages (`feat:`, `fix:`, `docs:`, `chore:`). While the CLI is at
  0.x, a breaking change, of `client/` included, takes no `!`: every minor release may break.
  goreleaser writes the release notes from these subjects, as `changelog:` in `.goreleaser.yml`
  sets out, so the subject is what a user reads there. Give every change of `client/` the `client`
  scope, as in `refactor(client): …`: it puts the change in the group the Terraform provider's
  maintainers read before they bump the module.
- **Stress-test a plan before writing code.** For any non-trivial change, walk each branch of the
  decision tree and settle every open question with a recommended answer first. (*meshcloud-internal*:
  the `grill-me` skill of `../meshfed-release`.)

</rules>

## Commands

Everything runs through the Taskfile, inside `nix develop`. **`task --list` is the list.**

The Go version is pinned in `go.mod`, in `flake.nix` and in the `Dockerfile`'s base image. **They
must agree**, each says so at the pin, and all three are held in lock-step with the Terraform
provider's own pin.

## Acceptance tests

This repository is a **meshStack satellite**: `cmd/internal/testacc/` runs the commands against a
live backend. It builds **no binary**: it runs the cobra commands in process, and talks to them only
through stdin, stdout, stderr and the environment, as a person or a script uses the binary.
A whole meshStack only exists in the *meshcloud-internal* mono repo, so
CI here does not run the suite. `.github/workflows/test-acceptance.yml` asks that repository for the
run, and `meshstack-satellite.gradle` is everything the run reads from here. The other half of the
lane belongs to `../meshfed-release` and changes without us, so read it there, in
`satellite-suites.md` of the `acceptance-testing` skill, rather than trusting a copy here.

To run the suite yourself, bring up the local stack of `../meshfed-release` (its `local-dev-stack`
skill); `./gradlew :meshstack-cli:satelliteEnv` there writes `../.env-testacc-meshstack-cli`. Then run
the suite from here with plain `go test`:

```bash
set -a; . ../.env-testacc-meshstack-cli; set +a
go test ./cmd/internal/testacc/... -run TestAcc
```

## Releasing

Pushing a `vN.N.N` tag runs `.github/workflows/release.yml`: goreleaser publishes the archives and
checksums, and the image goes to GHCR only, as `ghcr.io/meshcloud/meshstack-cli`. Every other
channel the release publishes to is a job of that workflow or a publish section of `.goreleaser.yml`,
and its one-time setup is described there.

<rules id="release-version">
The version reaches the binary through an ldflag on
`github.com/meshcloud/meshstack-cli/cmd/internal.Version`, set in `.goreleaser.yml`, in the
`Dockerfile` and in `flake.nix`. **They must agree**, and all three say so at the ldflag. The linker
ignores an `-X` whose path does not resolve and warns about nothing, so a stale path is silent.

A build with no ldflag reports the version the go command stamped, a pseudo-version inside a
checkout, so a missed ldflag fails nothing. Check `dist/*/meshstack --version` after
`task release:snapshot`.
</rules>

Pin every GitHub Action by commit SHA with the version in a trailing comment, as the existing
workflows do.
