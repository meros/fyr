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
          vendorHash = "sha256-+GV6f45A+AcPTjboUx0yQ1p62rywC3MKzs35EoQKspM=";
          subPackages = [ "cmd/fyrd" "cmd/fyr" ];
          env.CGO_ENABLED = 0;
          ldflags = [ "-s" "-w" "-X main.buildID=nix-${rev}" ];
          preBuild = ''
            cp -r ${fyr-web}/dist/. web/dist/
          '';
          # Fetching the Go modules does not need the web build.
          overrideModAttrs = _: { preBuild = ""; };
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

      # Evaluates the module in a minimal system and checks the units it
      # writes (run with `nix flake check`).
      checks.x86_64-linux.module =
        let
          sys = nixpkgs.lib.nixosSystem {
            system = "x86_64-linux";
            modules = [
              self.nixosModules.fyr
              {
                boot.isContainer = true;
                system.stateVersion = "25.11";
                users.users.u = { isNormalUser = true; home = "/home/u"; group = "users"; };
                services.tailscale.enable = true;
                services.fyr = { enable = true; user = "u"; tailnet.enable = true; scanRoots = [ "~/git" ]; };
              }
            ];
          };
          fyr = sys.config.systemd.services.fyr.serviceConfig;
          tn = sys.config.systemd.services.fyr-tailnet.serviceConfig;
          conf = builtins.fromJSON sys.config.environment.etc."fyr/config.json".text;
        in
        assert nixpkgs.lib.hasInfix "-seed " fyr.ExecStart;
        assert nixpkgs.lib.hasInfix "-- -config /etc/fyr/config.json" fyr.ExecStart;
        assert sys.config.systemd.services.fyr.restartIfChanged == false;
        assert conf.scanRoots == [ "~/git" ];
        assert nixpkgs.lib.hasInfix "--https=7443 http://127.0.0.1:7777" tn.ExecStart;
        nixpkgs.legacyPackages.x86_64-linux.runCommand "fyr-module-check" { } "touch $out";

      nixosModules.fyr = import ./nix/module.nix self;
      nixosModules.default = self.nixosModules.fyr;
    };
}
