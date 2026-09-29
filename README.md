# meshStack CLI

`meshstack` is the command line interface for [meshStack](https://www.meshcloud.io/). This
repository also holds the Go client for the meshStack API, which the
[meshStack Terraform provider](https://github.com/meshcloud/terraform-provider-meshstack) imports.

## Install

With [Homebrew](https://brew.sh/), on macOS or Linux:

```shell
brew install meshcloud/tap/meshstack-cli
brew upgrade meshstack-cli
```

With Go:

```shell
go install github.com/meshcloud/meshstack-cli/cmd/meshstack@latest
```

With [Nix](https://nixos.org/) (flakes enabled), on Linux (x86_64, aarch64) or on an Apple Silicon Mac:

```shell
nix profile add github:meshcloud/meshstack-cli   # older Nix versions call this `nix profile install`
nix profile upgrade meshstack-cli
```

`nix profile upgrade` follows the `main` branch, not releases. To try it without installing it,
run `nix run github:meshcloud/meshstack-cli -- --version`.

## Development

The Nix dev shell provides Go, `goreleaser` and `task`. `task lint` builds `golangci-lint` from
the tool directive in `go.mod`, so the dev shell deliberately does not carry it:

```shell
nix develop
task build            # writes ./meshstack
task test
task lint             # add -- --fix to apply the fixes it can make itself
task release:snapshot # the release artifacts, into dist/, without publishing them
```
