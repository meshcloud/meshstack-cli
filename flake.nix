{
  description = "meshStack CLI";

  inputs = {
    nixpkgs.url = "nixpkgs/nixos-unstable";
  };

  outputs = { self, nixpkgs }:
    let
      supportedSystems = [ "x86_64-linux" "x86_64-darwin" "aarch64-darwin" ];
      forEachSupportedSystem = f: nixpkgs.lib.genAttrs supportedSystems (system: f {
        pkgs = import nixpkgs { inherit system; };
      });

      # Only a release carries a tag, so a flake build reports the commit it was built
      # from and falls back to `dev` the way a `go build` without the ldflag does.
      version = self.shortRev or self.dirtyShortRev or "dev";

      # Takes pkgs so overlays.default can build it from the *consumer's* nixpkgs, while
      # packages.<system> below builds it from this flake's locked one.
      #
      # go 1.27 (pinned, in lock-step with go.mod — and with the dev shell below); 1.27
      # is the floor because internal/http declares generic methods. The override is
      # what carries the pin: buildGoModule ignores a `go` attribute in the argument set
      # and would quietly build against nixpkgs' default Go instead.
      meshstackPackage = pkgs: (pkgs.buildGoModule.override { go = pkgs.go_1_27; }) {
        pname = "meshstack";
        inherit version;
        src = self;

        # There is deliberately no subPackages: cmd/meshstack is the module's only main
        # package, so it is the only binary installed either way, and an unrestricted set
        # is what lets doCheck below run the whole suite instead of one directory's
        # tests. That directory is also what names the binary `meshstack`, which is why
        # no build here or anywhere else passes -o. See AGENTS.md.

        # Derived from go.mod and go.sum: when it goes stale the build fails and prints
        # the value to paste back in.
        vendorHash = "sha256-5LI/e9+Vh82qqJiAosn4casajqszVai3mvLuwVy4kDs=";

        # The third place setting -X main.Version, after .goreleaser.yml and the
        # Dockerfile; all three have to agree. A build without it reports `dev`.
        ldflags = [ "-s" "-w" "-X main.Version=${version}" ];

        # The suite passes in the sandbox — every test that wants $HOME, a config file
        # or a credentials directory points itself at a temp dir — so building the
        # package is a test gate for consumers too. Should a test ever need a real
        # $HOME, turn this off rather than teaching the sandbox to provide one.
        doCheck = true;

        meta = {
          description = "Command line interface for meshStack";
          homepage = "https://github.com/meshcloud/meshstack-cli";
          license = nixpkgs.lib.licenses.asl20;
          mainProgram = "meshstack";
        };
      };
    in
    {
      # Two lines make the binary available to another flake — this is how the meshStack
      # Terraform provider's dev shell gets it:
      #
      #   inputs.meshstack-cli.url = "github:meshcloud/meshstack-cli";
      #   # then, in a devShell:  packages = [ meshstack-cli.packages.${system}.meshstack ];
      packages = forEachSupportedSystem ({ pkgs }: rec {
        meshstack = meshstackPackage pkgs;

        # `nix build github:meshcloud/meshstack-cli` and `nix run … -- --version`.
        default = meshstack;
      });

      # The alternative to the line above: a consumer that adds this overlay to its own
      # nixpkgs writes `meshstack` in a `with pkgs; [ … ]` list like any other package.
      overlays.default = final: _prev: {
        meshstack = meshstackPackage final;
      };

      devShells = forEachSupportedSystem ({ pkgs }: {
        default = pkgs.mkShell {
          packages = with pkgs; [
            # go 1.27 (pinned, in lock-step with go.mod — and with the meshStack
            # Terraform provider, which consumes this repository's client package).
            # 1.27 is the floor because internal/http declares generic methods.
            go_1_27

            # goimports, godoc, etc.
            gotools

            # https://github.com/golangci/golangci-lint
            golangci-lint

            # https://taskfile.dev
            go-task

            # https://goreleaser.com — task release:check / release:snapshot
            goreleaser
          ];

          shellHook = ''
            # Explicitly set GOROOT to Nix-installed Go
            export GOROOT="${pkgs.go_1_27}/share/go"

            # Isolate Go environment from system
            export GOPATH="$PWD/.nix-go"
            export GOCACHE="$PWD/.nix-go/cache"
            export GOMODCACHE="$PWD/.nix-go/mod"
            export GOBIN="$PWD/.nix-go/bin"
            export PATH="$GOBIN:$PATH"

            mkdir -p "$GOPATH" "$GOCACHE" "$GOMODCACHE" "$GOBIN"
          '';
        };
      });
    };
}
