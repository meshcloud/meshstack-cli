# meshStack CLI

`meshstack` is the command line interface for [meshStack](https://www.meshcloud.io/).

## Install

```shell
go install github.com/meshcloud/meshstack-cli/cmd/meshstack@latest
```

## Development

The Nix dev shell provides Go, `goreleaser` and `task`. `task lint` builds `golangci-lint` from
the tool directive in `go.mod`, so the dev shell deliberately does not carry it:

```shell
nix develop
task build            # ./meshstack
task test             # go test ./...
task lint             # golangci-lint run, add -- --fix to apply fixes
task release:snapshot # build the release artifacts without publishing
```

The Go version is pinned in `go.mod` and in `flake.nix`, and is kept in lock-step with the
[meshStack Terraform provider](https://github.com/meshcloud/terraform-provider-meshstack), which
imports this repository's client package.
