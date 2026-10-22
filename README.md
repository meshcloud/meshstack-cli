# meshStack CLI

`meshstack` is the command line interface for your [meshStack](https://www.meshcloud.io/en/product/) instance,
handling multiple meshStacks via [the `meshstack profile` command](docs/profile.md).

Start with `meshstack login --profile your-profile-name` and explore the CLI with `meshstack --help`, after you [install](#install) it.

The [meshStack Terraform provider](https://github.com/meshcloud/terraform-provider-meshstack) builds on this `login`:
a provider block takes the endpoint and the credential of the profile of the last login. A browser login also needs
the workspace in the block or in `MESHSTACK_WORKSPACE`, while an API key or an API token needs none. The workspace
only picks the role the provider acts with, since each resource names its own workspace:

```hcl
provider "meshstack" {
  workspace = "team-a-ab12c"
}
```

A browser login acts in one workspace per provider block. To manage several workspaces in one
configuration, add an aliased block per workspace, as you would add an `azurerm` block per subscription.
Each block gets a token for its own workspace from the same login, so you need a role in each of them;
`meshstack workspace list` shows them.

```hcl
provider "meshstack" {
  alias     = "team_a"
  workspace = "team-a-ab12c"
}

resource "meshstack_building_block" "team_a_sandbox" {
  provider = meshstack.team_a
  # ...
}
```

Commands meant for people, such as `meshstack auth status` and `meshstack profile list`, write Markdown.
The other commands write JSON, or one item per line with `--output ndjson`, for scripts and agentic AI
workflows that read it through a `| jq ...` pipe. A listing stops at 100 items unless `--limit` says otherwise.
AI agents get the [`meshstack-cli` skill](skills/meshstack-cli/SKILL.md): link it from your repository's agent
instructions.

On a terminal, the Markdown is rendered and the log is colored. [`NO_COLOR`](https://no-color.org) turns the
colors off, and `FORCE_COLOR` or [`CLICOLOR_FORCE`](https://bixense.com/clicolors/) turns them on where the
output is no terminal, such as in a CI job. GitHub Actions, GitLab CI and Azure Pipelines get a colored log
without being asked, and the log of a building block run stays plain, since meshPanel shows it as plain text.

`meshstack api` sends an authenticated, `curl`-like request to any path of the API. For a meshObject
endpoint it picks the latest version the [OpenAPI docs](https://docs.meshcloud.io/api/introduction/) list,
and `meshstack api-docs` shows what those docs say about a request.

![meshstack login picks a profile, logs in through the browser and selects a workspace, meshstack buildingblock list lists the first building blocks, meshstack api sends that request as it is, the same command line with api-docs in place of api describes it, as meshstack api-docs --describe bb.list does, meshstack profile edits a profile, and meshstack profile add completes the endpoint and the workspace of the logged-in profile](docs/demo.gif)


## Install

### With [Homebrew](https://brew.sh/)

```shell
brew install meshcloud/tap/meshstack-cli
brew upgrade meshstack-cli
```

### With [winget](https://learn.microsoft.com/windows/package-manager/winget/) on Windows

```powershell
winget install meshcloud.meshstack-cli
winget upgrade meshcloud.meshstack-cli
```

### With Go

```shell
go install github.com/meshcloud/meshstack-cli/cmd/meshstack@latest
```

...or without installing it:

```shell
go run github.com/meshcloud/meshstack-cli/cmd/meshstack@latest --version
```

### With [Nix](https://nixos.org/) (flakes enabled)

```shell
nix profile add github:meshcloud/meshstack-cli   # older Nix: `nix profile install`
nix profile upgrade meshstack-cli
```

...or without installing it:

```shell
nix run github:meshcloud/meshstack-cli -- --version
```

:warning: `nix profile upgrade` follows the `main` branch, not releases.

### With the install script

```shell
curl -fsSL https://raw.githubusercontent.com/meshcloud/meshstack-cli/main/install.sh | sh
```

Run it again to upgrade. `| sh -s -- --version v1.2.3 --dir ~/bin` pins a release and the directory,
and `| sh -s -- --help` lists the options.

On Windows, in PowerShell:

```powershell
irm https://raw.githubusercontent.com/meshcloud/meshstack-cli/main/install.ps1 | iex
```

It installs with winget when winget is available, and otherwise downloads the release into
`%LOCALAPPDATA%\Programs\meshstack-cli` and adds that directory to your user PATH. Run it again to
upgrade. To pass options, run it as a script block:

```powershell
& ([scriptblock]::Create((irm https://raw.githubusercontent.com/meshcloud/meshstack-cli/main/install.ps1))) -Version v1.2.3 -Dir ~\bin
```

`-NoWinget` downloads the release even when winget is available, and `-Help` lists the options.

## Development

This repository also holds the Go client for the meshStack API in [`client/`](client), which the
[meshStack Terraform provider](https://github.com/meshcloud/terraform-provider-meshstack) imports.

The Nix dev shell provides Go, `goreleaser` and `task`:

```shell
nix develop
task build            # writes ./meshstack
task test
task lint             # add -- --fix to apply the fixes it can make itself
task release:snapshot # the release artifacts, into dist/, without publishing them
```
