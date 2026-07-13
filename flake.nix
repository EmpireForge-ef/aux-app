{
  description = "Aux — Spotify & Anthropic API Go wrapper with a web frontend";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
        lib = pkgs.lib;

        backend = pkgs.buildGoModule {
          pname = "aux-backend";
          version = "0.1.0";
          # Only Go sources — frontend changes shouldn't rebuild the backend.
          src = lib.fileset.toSource {
            root = ./.;
            fileset = lib.fileset.difference ./. (lib.fileset.unions [
              ./frontend
              ./flake.nix
              ./flake.lock
            ]);
          };
          # Recompute with `nix build .#backend` whenever go.sum changes.
          vendorHash = "sha256-BMyBFR7pwI5i0iB4wpQVio4L+r80deY5fbq5E5cz758=";
          subPackages = [ "cmd/server" ];
          meta.mainProgram = "server";
        };

        frontend = pkgs.buildNpmPackage {
          pname = "aux-frontend";
          version = "0.1.0";
          src = ./frontend;
          # Update with `nix run nixpkgs#prefetch-npm-deps -- frontend/package-lock.json`
          # whenever package-lock.json changes.
          npmDepsHash = "sha256-Cnk98dF90GPsPmynkooep/P7achhaNtpBxrR/3NO36Q=";
          installPhase = ''
            runHook preInstall
            cp -r dist $out
            runHook postInstall
          '';
        };

        # Backend wrapped to serve the built frontend.
        aux = pkgs.symlinkJoin {
          name = "aux";
          paths = [ backend ];
          nativeBuildInputs = [ pkgs.makeWrapper ];
          postBuild = ''
            wrapProgram $out/bin/server \
              --set-default AUX_STATIC_DIR ${frontend}
          '';
          meta.mainProgram = "server";
        };
      in
      {
        packages = {
          inherit backend frontend;
          default = aux;
        };

        devShells.default = pkgs.mkShell {
          packages = with pkgs; [
            # Go backend
            go
            gopls              # Go language server
            gotools            # goimports, godoc, etc.
            go-tools           # staticcheck
            golangci-lint      # meta-linter
            delve              # debugger
            gofumpt            # stricter gofmt
            air                # live reload for the Go server

            # Frontend (npm ships with nodejs; lockfile is package-lock.json)
            nodejs_22
            typescript
            typescript-language-server

            # General tooling
            git
            curl               # poking at the Spotify/Anthropic APIs
            jq                 # inspecting JSON responses
            prefetch-npm-deps  # recompute npmDepsHash after lockfile changes
          ];

          shellHook = ''
            echo "Aux dev shell — go $(go version | cut -d' ' -f3), node $(node --version)"
          '';
        };
      });
}
