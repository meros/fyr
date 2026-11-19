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
          # Some tests make throwaway git repos (workspaces, the contract fixture).
          nativeCheckInputs = [ pkgs.git ];
          checkPhase = ''
            runHook preCheck
            export HOME=$TMPDIR GIT_AUTHOR_NAME=fyr GIT_AUTHOR_EMAIL=fyr@localhost GIT_COMMITTER_NAME=fyr GIT_COMMITTER_EMAIL=fyr@localhost
            go test ./internal/...
            runHook postCheck
          '';
          meta.mainProgram = "fyr";
        };

        default = fyr;
      });

      devShells = forAll (pkgs: {
        default = pkgs.mkShell {
          # nodejs is nixpkgs' current LTS (the Makefile refuses any other).
          packages = with pkgs; [ go nodejs gnumake git gh sqlite ];
          CGO_ENABLED = "0";
        };
      });

      # Evaluates the module in a minimal system and checks the units it
      # writes (run with `nix flake check`).
      checks.x86_64-linux.module =
        let
          mkSys = fyr: nixpkgs.lib.nixosSystem {
            system = "x86_64-linux";
            modules = [
              self.nixosModules.fyr
              {
                boot.isContainer = true;
                system.stateVersion = "25.11";
                users.users.u = { isNormalUser = true; home = "/home/u"; group = "users"; };
                services.tailscale.enable = true;
                services.fyr = { enable = true; user = "u"; } // fyr;
              }
            ];
          };
          sys = mkSys {
            tailnet.enable = true; scanRoots = [ "~/git" ];
            quota.claudeDeepseek = { kind = "deepseek"; keyFile = "/run/secrets/deepseek"; };
          };
          # V0.54: host-b as docs/API-v0.md configures it: a host on its
          # tailnet address and the phone's front, which also lists the laptop.
          server = mkSys {
            tailnet.enable = true; hostPort = 7778;
            hosts = [ { name = "host-a"; url = "http://host-a:7778"; } ];
          };
          unit = mkSys { cgroups.agentsUnit = false; };
          unitFyr = unit.config.systemd.services.fyr.serviceConfig;
          unitConf = builtins.fromJSON (builtins.unsafeDiscardStringContext unit.config.environment.etc."fyr/config.json".text);
          slice = sys.config.systemd.slices.fyr-agents.sliceConfig;
          sconf = builtins.fromJSON (builtins.unsafeDiscardStringContext server.config.environment.etc."fyr/config.json".text);
          fyr = sys.config.systemd.services.fyr.serviceConfig;
          tn = sys.config.systemd.services.fyr-tailnet.serviceConfig;
          conf = builtins.fromJSON (builtins.unsafeDiscardStringContext sys.config.environment.etc."fyr/config.json".text);
        in
        assert nixpkgs.lib.hasInfix "-seed " fyr.ExecStart;
        assert nixpkgs.lib.hasInfix "-- -config /etc/fyr/config.json" fyr.ExecStart;
        assert sys.config.systemd.services.fyr.restartIfChanged == false;
        # V0.180: a changed config reloads (SIGHUP to the launcher, the agents stay).
        assert sys.config.systemd.services.fyr.reloadTriggers == [ sys.config.environment.etc."fyr/config.json".text ];
        assert nixpkgs.lib.hasSuffix "/bin/kill -HUP $MAINPID" fyr.ExecReload;
        assert conf.scanRoots == [ "~/git" ];
        assert conf.quota == { claudeDeepseek = { kind = "deepseek"; keyFile = "/run/secrets/deepseek"; }; };
        assert nixpkgs.lib.hasInfix "--https=7443 unix:/home/" tn.ExecStart && nixpkgs.lib.hasSuffix "/.local/share/fyr/serve.sock" tn.ExecStart;
        # A switch that restarts tailscaled must not leave Serve unpublished.
        assert tn.Restart == "on-failure";
        # V0.51: the default role (both) writes no role, hosts or hostListen.
        assert !(conf ? role) && !(conf ? hosts) && !(conf ? hostListen);
        assert sconf.hostListen == "tailnet:7778" && !(sconf ? role);
        assert sconf.hosts == [ { name = "host-a"; url = "http://host-a:7778"; } ];
        assert server.config.networking.firewall.interfaces.tailscale0.allowedTCPPorts == [ 7778 ];
        assert builtins.elem "tailscaled.service" server.config.systemd.services.fyr.after;
        assert !(builtins.elem "tailscaled.service" sys.config.systemd.services.fyr.after);
        # V0.agents-unit: fyrd's unit is not delegated and oomd omits it; the
        # budget and the oomd rule sit on fyr-agents.slice; polkit lets the
        # user start Fyr's scopes only.
        assert !(fyr ? Delegate) && fyr.MemoryLow == "1G" && fyr.ManagedOOMPreference == "omit" && fyr.OOMScoreAdjust < 0;
        assert !(fyr ? MemoryMax) && !(fyr ? MemoryHigh);
        assert slice.CPUQuota == "400%" && slice.MemoryHigh == "13G" && slice.MemoryMax == "14G" && slice.MemorySwapMax == "16G";
        assert slice.ManagedOOMMemoryPressure == "kill" && slice.ManagedOOMMemoryPressureLimit == "80%";
        assert nixpkgs.lib.hasInfix "fyr-(agent|deploy)-" sys.config.security.polkit.extraConfig;
        assert nixpkgs.lib.hasInfix ''subject.user == "u"'' sys.config.security.polkit.extraConfig;
        assert conf.cgroups.agentsSlice == "fyr-agents.slice" && nixpkgs.lib.hasSuffix "/bin/systemd-run" conf.cgroups.systemdRun;
        assert removeAttrs conf.cgroups [ "agentsSlice" "systemdRun" "systemctl" ] == { enable = true; serverMemoryLow = "1G"; agentsCpuMax = "400000 100000";
          agentsMemoryHigh = "13G"; agentsMemoryMax = "14G"; agentsMemorySwapMax = "16G"; threadMemoryHighPercent = 0; };
        # V0.80 with agentsUnit off: the unit delegates cpu and memory; the budget is fyrd's to write on agents/.
        assert unitFyr.Delegate == "cpu memory" && unitFyr.MemoryLow == "1G" && !(unitFyr ? ManagedOOMPreference);
        assert !(unitConf.cgroups ? agentsSlice) && !(unit.config.systemd.slices ? fyr-agents);
        nixpkgs.legacyPackages.x86_64-linux.runCommand "fyr-module-check" { } "touch $out";

      nixosModules.fyr = import ./nix/module.nix self;
      nixosModules.default = self.nixosModules.fyr;
    };
}
