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

      # A flake input carries no tag, only a revision, so a nix build reports the commit it
      # was built from. A nix derivation version carries no leading v, so reportedVersion
      # adds one as a Go pseudo-version.
      version = self.shortRev or self.dirtyShortRev or "dev";
      reportedVersion = if version == "dev" then version else "v0.0.0-${version}";

      # Takes pkgs so overlays.default can build it from the *consumer's* nixpkgs, while
      # packages.<system> below builds it from this flake's locked one.
      #
      # go 1.27 (pinned, in lock-step with go.mod and with terraform-provider-meshstack).
      # The override is what carries the pin: buildGoModule ignores a `go` attribute in the
      # argument set and builds against nixpkgs' default Go instead.
      meshstackPackage = pkgs: (pkgs.buildGoModule.override { go = pkgs.go_1_27; }) {
        pname = "meshstack";
        inherit version;
        src = self;

        # No subPackages, so that doCheck below runs the whole suite rather than one
        # directory's tests.

        vendorHash = "sha256-vvO0VufdztbH0PCXGwJ1yEfB4Xo1Ot/b5JkrVe0YTE0=";

        # .goreleaser.yml and the Dockerfile set the same ldflag, and all three have to
        # agree. The linker ignores an -X whose path does not resolve and warns about
        # nothing, so a stale path here is silent.
        ldflags = [ "-s" "-w" "-X github.com/meshcloud/meshstack-cli/cmd/internal.Version=${reportedVersion}" ];

        # Every test points itself at a temp dir for $HOME and for its config, so the
        # suite passes in the nix sandbox. Should a test ever need a real $HOME, turn
        # this off rather than teaching the sandbox to provide one.
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
        default = meshstack;
      });

      # The alternative to the lines above: a consumer that adds this overlay to its own
      # nixpkgs writes `meshstack` in a `with pkgs; [ … ]` list like any other package.
      overlays.default = final: _prev: {
        meshstack = meshstackPackage final;
      };

      devShells = forEachSupportedSystem ({ pkgs }: {
        default = pkgs.mkShell {
          packages = with pkgs; [
            # go 1.27 (pinned, in lock-step with go.mod and with terraform-provider-meshstack)
            go_1_27

            gotools

            # No golangci-lint here: it is a tool directive in go.mod, so `task lint` builds it
            # with the pinned Go rather than taking whatever nixpkgs built it with.

            go-task
            goreleaser
          ];

          shellHook = ''
            export GOROOT="${pkgs.go_1_27}/share/go"

            # Keep the Go caches out of the developer's home directory.
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
