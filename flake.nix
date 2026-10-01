{
  description = "meshStack CLI";

  inputs = {
    # nixpkgs-unstable rather than nixos-unstable: the former only advances once the Darwin
    # builds pass as well, and a flake reference spelled out in full does not depend on the
    # consumer's flake registry.
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
  };

  outputs = { self, nixpkgs }:
    let
      # No x86_64-darwin: nixpkgs 26.11 dropped it, so this flake cannot build for Intel Macs.
      # The release archives still cover them.
      supportedSystems = [ "x86_64-linux" "aarch64-linux" "aarch64-darwin" ];
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
        # directory's tests. docs/demo is a module of its own.
        excludedPackages = [ "docs/demo" ];

        vendorHash = "sha256-tQ9LvCSoYwwCnOH9XKOVbTfibAP5QSiuCNy0JnV18S0=";

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
          license = pkgs.lib.licenses.asl20;
          mainProgram = "meshstack";
        };
      };
    in
    {
      # terraform-provider-meshstack's dev shell reads packages.<system>.meshstack, so a rename
      # breaks that flake.
      packages = forEachSupportedSystem ({ pkgs }: rec {
        meshstack = meshstackPackage pkgs;
        default = meshstack;
      });

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

            vhs
            # VHS starts the first bash on PATH, and docs/demo/demo.tape recalls a command line
            # with the up arrow, which needs readline. The bash of stdenv is built without it.
            bashInteractive
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
