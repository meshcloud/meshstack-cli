# meshStack CLI

`meshstack` is the command line interface for your [meshStack](https://www.meshcloud.io/en/product/) instance,
handling multiple meshStacks via [the `meshstack profile` command](docs/profile.md).

Start with `meshstack login --profile your-profile-name` and explore the CLI with `meshstack --help`.

The [meshStack Terraform provider](https://github.com/meshcloud/terraform-provider-meshstack) builds on this `login` as follows:

```hcl
provider "meshstack" {
  profile   = "your-profile-name" # login creates 'default' initially
  workspace = "your-workspace"    # use 'meshstack workspace list' to find one, 
                                  # also profile may specify default
}
```

Other commands typically use `--output [nd]json` by default. 
They are intended to be using within Agentic AI workflows with a `| jq ...` pipe and probed with `--limit`. 

![meshstack login picks a profile, logs in through the browser and selects a workspace, meshstack buildingblock list lists the first building blocks, meshstack api sends that request as it is, the same command line with api-docs in place of api describes it, as meshstack api-docs --describe bb.list does, meshstack profile edits a profile, and meshstack profile add completes the endpoint and the workspace of the logged-in profile](docs/demo.gif)


## Install

### With [Homebrew](https://brew.sh/)

```shell
brew install meshcloud/tap/meshstack-cli
brew upgrade meshstack-cli
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
