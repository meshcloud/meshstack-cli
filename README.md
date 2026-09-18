# meshStack CLI

`meshstack` is the command line interface for [meshStack](https://www.meshcloud.io/). This
repository also holds the Go client for the meshStack API, which the
[meshStack Terraform provider](https://github.com/meshcloud/terraform-provider-meshstack) imports.

## Install

```shell
go install github.com/meshcloud/meshstack-cli/cmd/meshstack@latest
```

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
