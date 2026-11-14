{
  description = "Fyr: a calm control room for Claude coding agents";

  inputs.nixpkgs.url = "github:nixos/nixpkgs/nixos-unstable";

  outputs = { self, nixpkgs }:
    let
      systems = [ "x86_64-linux" "aarch64-linux" ];
      forAll = f: nixpkgs.lib.genAttrs systems (s: f nixpkgs.legacyPackages.${s});
      rev = self.shortRev or self.dirtyShortRev or "dirty";
    in
    {
      packages = forAll (pkgs: rec {
        # The web client (web/), built once and embedded into fyrd.
        fyr-web = pkgs.buildNpmPackage {
          pname = "fyr-web";
          version = "0-${rev}";
          src = ./web;
          npmDepsHash = "sha256-rykyCcVohrMCXg3EVUw9fQCX94XY0h3rXMjzO5wxW5o=";
          npmFlags = [ "--legacy-peer-deps" ];
          installPhase = ''
            mkdir -p $out
            cp -r dist $out/dist
          '';
        };

        # fyrd (the daemon) and fyr (the launcher). The NixOS module runs the
        # launcher and passes this fyrd as the seed (decision 0007).
        fyr = pkgs.buildGoModule {
          pname = "fyr";
          version = "0-${rev}";
          src = ./.;
          vendorHash = "sha256-n+WU7qcF6NauBw9DFU4VG1fnDQ387+nibJ/uzApqehk=";
          subPackages = [ "cmd/fyrd" "cmd/fyr" ];
          env.CGO_ENABLED = 0;
          ldflags = [ "-s" "-w" "-X main.buildID=nix-${rev}" ];
          preBuild = ''
            cp -r ${fyr-web}/dist/. web/dist/
          '';
          # The tests start child processes (a fake claude, fake daemons)
          # and pass in the sandbox; they also run in `make test`.
          doCheck = true;
          checkPhase = ''
            runHook preCheck
            go test ./internal/...
            runHook postCheck
          '';
          meta.mainProgram = "fyr";
        };

        default = fyr;
      });

      devShells = forAll (pkgs: {
        default = pkgs.mkShell {
          packages = with pkgs; [ go nodejs_24 gnumake git gh sqlite ];
          CGO_ENABLED = "0";
        };
      });

      nixosModules.fyr = import ./nix/module.nix self;
      nixosModules.default = self.nixosModules.fyr;
    };
}
