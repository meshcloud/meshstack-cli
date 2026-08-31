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
has no closer home. Everything else belongs next to what it governs, and this file links to it:

| Belongs in | Rather than here |
|---|---|
| `.golangci.yml` | Which dependency may reach which package, and why |
| `Taskfile.yml` | What a command does, and what surprises it holds — `task --list` prints the list |
| A doc comment on the code | Why a package, type or command is built the way it is |
| The file that holds the setting | Why the setting has that value: `flake.nix`, `go.mod`, `.goreleaser.yml`, `Dockerfile` |
| A skill | A procedure long enough to need its own steps, loaded only when the work starts |

Before adding a section, check whether one of those places already covers it. Before adding a
*paragraph* of reasoning, move the reasoning to the code or config it explains and leave a link.
Restating a rule in two places is worse than leaving it in one: the copies drift, and neither one
looks stale.
</rules>

## Naming

- **`meshstack`** — the binary, so every invocation reads `meshstack buildingblock list`.
- **meshStack CLI** — the product name, used in prose and docs.
- `github.com/meshcloud/meshstack-cli` — the repository and Go module.

Everything *published* carries the repository name — the release archives, the checksum file and the
container image are all `meshstack-cli` — while the binary inside them is `meshstack`.

The binary gets its name from its directory, `cmd/meshstack`, which is why there is no `-o` flag
anywhere: `go build ./cmd/meshstack` and
`go install github.com/meshcloud/meshstack-cli/cmd/meshstack@latest` both produce `meshstack`. **Do
not add a `main.go` at the repository root**; that would name the binary after the module and bring
the flag back.

## Package layout

| Path | Holds |
|---|---|
| `cmd/meshstack/` | `package main`: `main()` and the root command. The only main package. |
| `cmd/<subcommand>/` | One package per subcommand of the cobra command tree. |
| `pkg/` | Logic that does not need a CLI process, and that the Terraform provider can import. |
| `client/` | The meshStack API client. Path-identical to the provider's former `client/`. |
| `internal/http` | The process's one HTTP client, and the request building both front ends share. |
| `internal/cli` | The CLI's half of `auth.Input`: flags, stdin, a terminal prompt, the browser login. |

<rules id="command-tree">
In `cmd/`, **the package name is the subcommand and the file name is the leaf command**:
`cmd/buildingblock/list.go` holds `meshstack buildingblock list`. Each package exports a `New`
function returning its `*cobra.Command`, and `cmd/meshstack` wires children in with `AddCommand`.

`cmd/meshstack` is the one exception to that rule, and is not a subcommand: it is the binary's
`package main`, holding `main()` and the root command together.

Three rules hold the tree together, and `cmd/meshstack/meshstack.go` and `cmd/auth/login.go` carry
the reasoning for each in their doc comments:

- Register commands **explicitly in `cmd/meshstack`, never from `init()`**.
- A command with a **top-level shortcut** — `meshstack login` for `meshstack auth login` — is
  registered twice by calling its constructor twice. `Aliases` cannot do this.
- A constructor keeps its flag targets in **locals captured by the closure, never package-level
  vars**, and a **parent command sets `RunE` as well as `Args`**.
</rules>

## Dependency policy

The CLI is allowed **two external dependencies**: cobra, and `charmbracelet/log`. Each is confined to
a smaller area than the module. Everything else is standard library, with `testify` in tests.

**The policy lives in `.golangci.yml`**, in the comments on the `depguard` rules — which package may
import what, and why each boundary is where it is. `depguard` does not merely enforce the policy,
it *is* the policy, so widening a boundary is a deliberate edit rather than a lint fix.

<rules id="client-package">
`client/` is a **git subtree** imported from
[terraform-provider-meshstack](https://github.com/meshcloud/terraform-provider-meshstack), where it
used to live, and it keeps the `client` path prefix it had there. Carry changes across with
`git subtree`, not by copying files:

**A pull takes a split, not a branch.** The import was `git subtree split` followed by
`git subtree add`, so the history this subtree descends from carries the files at the *repository
root*. Pulling the provider's `main` directly fails with *"refusing to merge unrelated histories"*,
because there the same files sit under `client/`. Split first, in a checkout of the provider:

```shell
cd ../terraform-provider-meshstack
git subtree split --prefix=client -b client-split main

cd ../meshstack-cli
git subtree pull --prefix=client ../terraform-provider-meshstack client-split
git subtree push --prefix=client ../terraform-provider-meshstack <branch>
```

A pull conflicts only where a file genuinely diverged, because the one local edit the move needed was
rewriting the client's own import path.

Reading the pre-import history takes both paths, since the split history carries the files at the
repository root and the import merge re-roots them under `client/`:

```shell
git log -- client/client.go client.go   # a path-limited log from client/ alone stops at the merge
git blame client/client.go              # traverses the merge on its own
```

**`client/` no longer knows how to log in.** `client.Authorization` produces a bearer token and
replaces one that came back 401, and everything behind it — resolving a credential, minting a token,
caching it in a profile, refreshing before expiry — is `pkg/auth`. Both front ends build their client
through `auth.Session.Client`, so the endpoint and the authorization always agree with what was
resolved. Do **not** add a login exchange here or anywhere else: a second one gets a static token and
starts returning 401 once it expires, and for a browser login it would end the user's session.

**`client/` no longer owns HTTP either.** The client, the request options and the retry policy are
`internal/http`, one directory above, because `pkg/oidc` and `pkg/auth` need them and Go's internal
rule closes `client/internal` to both. Its names carry no `Http` prefix — the package is what says
that — so it reads `http.Client`, `http.Error`, `http.NewClient`.

**A file that needs both packages imports `net/http` as `gohttp`.** `internal/http` takes the plain
name, because it is the one a meshStack call goes through; `net/http` is left for the status and
method constants and for the loopback server. The `forbidigo` rule in `.golangci.yml` matches on the
type, not on the written name, so it catches `gohttp.Client` and leaves `http.Client` alone.

**`client/` has no logging seam.** It logs through `slog`'s default logger like everything else, so
there is no `client.SetLogger` any more. `cmd/meshstack` installs a `charmbracelet/log` handler and
the Terraform provider a `tflog` bridge, each before the first request. Log with the `Context` form —
`slog.DebugContext` — because the provider's handler reads terraform's logger out of the context, and
drops a record that arrives without one.
</rules>

## Always-on rules

<rules id="always-on">

- **Lean comments.** A comment earns its place only by saying what the code cannot — the *why*, a
  trade-off, a non-obvious constraint. Don't restate what a name, type or signature already conveys;
  prefer one sharp line over a paragraph.
- **Lint and format only via `task lint`**, and **never run `gofmt` or `go vet` separately** — a
  differently built gofmt enforces different formatting. `Taskfile.yml` and
  `.github/workflows/test.yml` explain why at the settings that depend on it. A `PostToolUse` hook in
  `.claude/settings.json` formats every `.go` file an agent writes, so it rarely reaches the gate.
- **Conventional Commits** for messages (`feat:`, `fix:`, `docs:`, `chore:`, `feat!:` for breaking).
- **Stress-test a plan before writing code.** For any non-trivial change, walk each branch of the
  decision tree and settle every open question with a recommended answer first. Catching a wrong turn
  at the plan stage is far cheaper than after the code and tests exist. (*meshcloud-internal*: the
  `grill-me` skill in `../meshfed-release/.agents/skills/`.)

</rules>

## Commands

Everything runs through the Taskfile, inside `nix develop`. **`task --list` is the list**, and
`Taskfile.yml` comments the tasks that hold a surprise.

The Go version is pinned in **two** places, `go.mod` and `flake.nix`, which both say so at the pin.
Keep them in lock-step when bumping, and keep them aligned with the Terraform provider.

`flake.nix` also builds the binary — `nix build .#meshstack` — and exports it as
`packages.<system>.meshstack` and as `overlays.default`, so another flake can put it in a dev shell.
The comment above that `packages` output shows the two lines a consumer needs.

## Authentication

`MESHSTACK_ENDPOINT`, `MESHSTACK_API_KEY` and `MESHSTACK_API_SECRET`, with `MESHSTACK_API_TOKEN` as
an alternative to the key and secret pair, plus `MESHSTACK_PROFILE`, `MESHSTACK_WORKSPACE`,
`MESHSTACK_NO_INPUT`, `MESHSTACK_CONFIG_FILE` and `MESHSTACK_CREDENTIALS_DIR`.

**No `MESHSTACK_*` name is exported.** Each is a private const in the package that consults it —
`pkg/auth`, `pkg/profile`, `pkg/meshstack`, `pkg/tty` — and every message that has to mention one is
produced there too, so neither front end assembles a sentence out of a constant it imported. A front
end that needs the *value* of a secret variable gets it through `auth.SecretFromEnv` or
`auth.TokenFromEnv`. The Taskfile reads a git-ignored `.env` for local runs.

`MESHSTACK_SKIP_VERSION_CHECK=true` skips the minimum backend version check in `client/client.go`.

## Releasing

Pushing a `v*` tag runs goreleaser, which publishes the archives and checksums, and then builds the
container image for the same tag. The image goes to GHCR only, as
`ghcr.io/meshcloud/meshstack-cli`, and its entrypoint is the `meshstack` binary, so
`docker run ghcr.io/meshcloud/meshstack-cli buildingblock list` reads like the local invocation. A
push to `main` refreshes `:main`, so an image exists before the first release does.

<rules id="release-version">
The version reaches the binary through an ldflag on `main.Version`, set in **three places that must
agree**: `.goreleaser.yml`, the `Dockerfile` and `flake.nix`, all of which say so at the ldflag. A
build without it reports `dev` — check with `meshstack --version` after `task release:snapshot`.
</rules>

Pin every GitHub Action by commit SHA with the version in a trailing comment, as the existing
workflows do.
