---
name: development
description: How the meshStack CLI's Go code is laid out and built - where a command, its logic and an HTTP call go, and what the client and settings must keep. Use when writing, changing or reviewing Go code here - a new command or package, a change to client/, pkg/ or internal/http, an HTTP request, a new setting or dependency, or a depguard or forbidigo finding.
---

# Developing the meshStack CLI

For Go idioms newer than your training, follow the `use-modern-go` skill of the
`modern-go-guidelines` plugin, which `.claude/settings.json` enables. `go.mod` pins the Go version
it applies to.

## Package layout

| Path | Holds |
|---|---|
| `cmd/meshstack/` | `package main`: `main()` and the root command. The only main package. |
| `cmd/<subcommand>/` | One package per subcommand of the cobra command tree, nested as the tree is: `cmd/buildingblock/tfstate/` holds `meshstack buildingblock tfstate`. |
| `cmd/internal/` | What several commands share as a front end: the global and shared flags, the client and session they resolve, output, prompts, the version. |
| `cmd/internal/testacc/` | The suite that runs the commands against a live meshStack. |
| `pkg/` | Each package here wraps the `internal/` package of the same name, and nothing else. |
| `client/` | The meshStack API client, which the Terraform provider imports. |
| `internal/` | Everything else. The `depguard` rules in `.golangci.yml` say which package may import which. |

`pkg/` and `client/` are the two import paths the Terraform provider's own `depguard` rule allows,
so a rename or a signature change in either breaks it. Go's internal rule closes `internal/` to the
provider, which is what makes the indirection through `pkg/` worth its cost.

<rules id="front-end-adapters">
**A command is a front end over an `internal/` package.** The command owns its flags, arguments,
prompts and output, and the hints that name a flag, such as "run again with --force". What it does
is the `internal/` package's, such as which workspace holds a state, or whether a run of the
building block blocks a write. `cmd/buildingblock/tfstate` over `internal/tfstate` is the pattern. So
the logic stays testable without cobra, and a second front end does not copy it.

`cmd/internal` is no place for that logic either: it holds only the front-end parts that several
commands share.
</rules>

<rules id="command-tree">
To add a command, put it in `cmd/`, where **the package name is the subcommand and the file name is
the leaf command**: `cmd/auth/login.go` holds `meshstack auth login`. The package exports a `New`
function returning its `*cobra.Command`, and the parent's constructor wires it in with `AddCommand`.
`cmd/meshstack` is the one exception: it is the binary's `package main`, with `main()` and the root
command. For a command over a meshObject kind, the `meshobject-command` skill has the rest.

- Register a command **explicitly in its parent's constructor, never from `init()`**.
- A command with a **top-level shortcut** — `meshstack login` for `meshstack auth login` — is
  registered twice by calling its constructor twice. `Aliases` cannot do this.
- A constructor keeps its own flag targets in **locals captured by the closure**. The persistent
  flags in `cmd/internal` are the exception: `SettingSources` reads their values back, so they are
  package-level vars.
- A **parent command sets `RunE` as well as `Args`**.
</rules>

## Dependencies

**The `depguard` rules in `.golangci.yml` are the policy**, not only its enforcement: each rule
confines a dependency to a smaller area than the module, so widening a boundary is a deliberate edit
rather than a lint fix. Adding a dependency therefore means editing `go.mod` and `.golangci.yml`,
and the second edit is where you argue for it.

<rules id="client-package">
**This repository is the client's only home.** `client/` moved here from
[terraform-provider-meshstack](https://github.com/meshcloud/terraform-provider-meshstack) as a
one-time `git subtree` import, and the provider requires this module at a released version. A
change here reaches the provider when it bumps its `meshstack-cli` requirement, so a break surfaces
there, later, and not in this repository's CI.

**Read the API docs before extending `client/`**:
[meshstack-openapi-docs.json](https://docs.meshcloud.io/api/meshstack-openapi-docs.json), or the
[dev variant](https://docs.dev.meshcloud.io/api/meshstack-openapi-docs.json) for what is merged to
`develop` but not released. The source is the controllers and meshObjects of `../meshfed-release`
(*meshcloud-internal*).

**The provider implements the client's interfaces.** Its tests plug the mocks of its
`internal/clientmock` into `client.Client`, so a method added to a `Mesh…Client` interface stops
the provider compiling at its next bump. Put a method only the CLI calls on a new `client.Client`
field instead: an interface of its own, or a concrete type such as `*client.RawClient` when nothing
mocks it. `client.New` sets the field for every caller, while the provider's mock client fills the
struct by field name and leaves a new field unset.

Reading the pre-import history takes both paths, since the import merge re-roots the files under
`client/`:

```shell
git log -- client/client.go client.go   # a path-limited log from client/ alone stops at the merge
git blame client/client.go              # traverses the merge on its own
```

**`client/` does not log in.** `client.Authorization` produces a bearer token and replaces one that
came back 401; resolving a credential, minting a token, caching it and refreshing it is `pkg/auth`.
Both front ends build their client through `auth.ResolveClient`, so the endpoint and the
authorization always agree with what was resolved. Do **not** add a login exchange anywhere else: a
second one gets a static token and starts returning 401 once it expires, and for a browser login it
would end the user's session.

**`client/` does not own HTTP.** The client, the request options and the retry policy are
`internal/http`, because `internal/oidc` and `pkg/auth` need them and Go's internal rule closes
`client/internal` to both. Its names carry no `Http` prefix: `http.Client`, `http.Error`.

**Every request names the front end and version that sent it.** `http.NewClient` takes an
`http.UserAgent`, which `auth.ResolveSessionOptions` embeds: the front end sets its `GitHubRepo` and
`Version` once, the CLI in `cmd/internal.ResolveClientOptions`, and the release check reads the same
two. So code below `cmd/` takes its client from its caller, and a command that needs one for
anything but a meshStack client calls `internal.ResolveClientOptions().HttpClient()`. `forbidigo`
keeps `http.UserAgent` to those options, and the transport to `internal/http`.

**`net/http` is always imported as `gohttp`**, which `importas` settles. That leaves the plain name
to `internal/http`, and `net/http` to the status and method constants and to the loopback server.
The `forbidigo` rule matches on the type rather than the written name, so it catches `gohttp.Client`
and leaves `http.Client` alone.

**Logging goes through `slog`'s default logger**, on which each front end installs its own handler:
`cmd/meshstack` a `charmbracelet/log` one, the Terraform provider a `tflog` bridge. A handler
installed that late constrains every log call, and `internal/http/logging.go` states how.
</rules>

## Settings

Every setting the CLI reads is a `MESHSTACK_`-prefixed environment variable;
`grep -rn 'setting\.Setting\['` finds them all, each next to the code that uses it.

**Each one is declared once, in the domain package it belongs to**, as a `setting.Setting[T]` whose
`EnvKey` is both the variable name and the setting's identity. `internal/setting` resolves it from
the sources it is given, and each front end contributes exactly one source over its own flags or
block attributes. **No front end assembles a sentence out of an imported name**: every message that
has to mention a variable is produced in the package that owns the declaration. The Taskfile reads a
git-ignored `.env` for local runs.
