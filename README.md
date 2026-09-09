# meshStack CLI

`meshstack` will be the command line interface for [meshStack](https://www.meshcloud.io/).

It is being built. Nothing here does anything useful yet: the binary prints one line and exits, and
the repository exists so far to carry the build, the linter, the test workflow and the acceptance
suite that meshStack's own CI runs against a live backend.

## Development

The Nix dev shell provides Go and `task`:

```shell
nix develop
task build   # ./meshstack
task test    # go test ./...
task lint    # golangci-lint run, add -- --fix to apply fixes
```

The Go version is pinned in `go.mod` and in `flake.nix`, and both have to be bumped together.
