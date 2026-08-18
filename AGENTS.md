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

## Naming

- **`meshstack`** — the binary, so every invocation reads `meshstack buildingblock list`.
- **meshStack CLI** — the product name, used in prose and docs.
- `github.com/meshcloud/meshstack-cli` — the repository and Go module.

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

<rules id="command-tree">
In `cmd/`, **the package name is the subcommand and the file name is the leaf command**:
`cmd/buildingblock/list.go` holds `meshstack buildingblock list`. Each package exports a `New`
function returning its `*cobra.Command`, and `cmd/meshstack` wires children in with `AddCommand`.

`cmd/meshstack` is the one exception to that rule, and is not a subcommand: it is the binary's
`package main`, holding `main()` and the root command together.

Register commands **explicitly in `cmd/meshstack`, never from `init()`**, so the command tree reads in
one place and a command cannot appear in the binary just because its package was imported for some
other reason.
</rules>

## Dependency policy

The CLI is allowed **exactly one external dependency, cobra**, and only `cmd/` may use it. Everything
else is standard library, with `testify` permitted in tests.

This is not austerity for its own sake. The Terraform provider imports `client/` and `pkg/login`, so
every dependency added here lands in the provider's dependency tree, and from there in the public
checksum database. `depguard` in `.golangci.yml` enforces the boundaries per directory; read those
rules as the policy. Widening them is a deliberate decision, not a lint fix.

<rules id="client-package">
`client/` keeps the import path it had in the Terraform provider, so moving code between the two
repositories stays a plain path rewrite.

The login exchange lives in `client/internal/auth.go`, which posts to `/api/login`, caches the access
token and refreshes it before expiry. Go's internal rule keeps that code inside `client/`; reach it
through `client.NewApiKeyAuthorization`. Do **not** write a second login exchange elsewhere — a
hand-rolled one gets a static token and starts returning 401 once it expires.

`pkg/login` owns credential resolution only: it reads the environment and returns a
`client.Authorization`. It is also the single place that constructs one, which is where token caching
on disk will hook in later.
</rules>

## Always-on rules

<rules id="always-on">

- **Lean comments.** A comment earns its place only by saying what the code cannot — the *why*, a
  trade-off, a non-obvious constraint. Don't restate what a name, type or signature already conveys;
  prefer one sharp line over a paragraph.
- **Lint only via `task lint`** (golangci-lint, which also enforces gci import ordering and gofmt).
  It already runs `govet`, so **do not run `go vet` separately**. Auto-fix with `task lint -- --fix`.
- **Conventional Commits** for messages (`feat:`, `fix:`, `docs:`, `chore:`, `feat!:` for breaking).
- **Stress-test a plan before writing code.** For any non-trivial change, walk each branch of the
  decision tree and settle every open question with a recommended answer first. Catching a wrong turn
  at the plan stage is far cheaper than after the code and tests exist. (*meshcloud-internal*: the
  `grill-me` skill in `meshfed-release/.agents/skills/`.)

</rules>

## Commands

Everything runs through the Taskfile, inside `nix develop`:

```shell
task build   # ./meshstack
task test    # go test ./...
task lint    # golangci-lint run
task tidy    # go mod tidy
```

The Go version is pinned in `go.mod` and in `flake.nix`; **keep them in lock-step when bumping**, and
keep them aligned with the Terraform provider, which consumes this module.

## Authentication

`MESHSTACK_ENDPOINT`, `MESHSTACK_API_KEY` and `MESHSTACK_API_SECRET`, with `MESHSTACK_API_TOKEN` as
an alternative to the key and secret pair. The names are exported as consts from `pkg/login`, so the
provider and the CLI share one definition — use those consts rather than the string literals. The
Taskfile reads a git-ignored `.env` for local runs.

`MESHSTACK_SKIP_VERSION_CHECK=true` skips the minimum backend version check in `client/client.go`.
