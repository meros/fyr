self:
{ config, lib, pkgs, ... }:

# Fyr as a NixOS service (SPEC 12.6, decision 0007).
#
# NixOS owns the frame: the unit, its user, PATH, environment, limits, the
# profiles and the launcher. The launcher owns the fyrd builds: it runs
# ~/.local/share/fyr/current/fyrd (a self-deploy, V0.13) when there is one,
# else this package's fyrd, the seed. Iterating on Fyr never needs a rebuild;
# a rebuild changes the frame or blesses a new seed.
let
  cfg = config.services.fyr;
  home = config.users.users.${cfg.user}.home;
  configJSON = builtins.toJSON ({
    listen = "127.0.0.1:${toString cfg.port}";
    permissionMode = cfg.permissionMode;
    idleStopMinutes = cfg.idleStopMinutes;
    slots = cfg.slots;
    profiles = cfg.profiles;
    scanRoots = cfg.scanRoots;
    quota = lib.mapAttrs (_: q: lib.filterAttrs (_: v: v != null) q) cfg.quota;
  } // lib.optionalAttrs cfg.cgroups.enable {
    # V0.80: fyrd splits the unit's cgroup and writes this budget itself.
    cgroups = {
      enable = true;
      inherit (cfg.cgroups) serverMemoryLow agentsMemoryHigh agentsMemoryMax agentsMemorySwapMax;
      agentsCpuMax = "${toString (cfg.cgroups.agentsCpuCores * 100000)} 100000";
    };
  } // lib.optionalAttrs (cfg.role != "both") { role = cfg.role; }
    // lib.optionalAttrs (cfg.hosts != [ ]) { hosts = cfg.hosts; }
    // lib.optionalAttrs (hostListen != null) { hostListen = hostListen; }
    // lib.optionalAttrs (cfg.ownerLogin != null) { ownerLogin = cfg.ownerLogin; });
  # V0.54: hostPort listens on this machine's tailnet address, which fyrd
  # reads from tailscaled at start; hostListen names an address by hand.
  hostListen = if cfg.hostListen != null then cfg.hostListen
    else if cfg.hostPort != null then "tailnet:${toString cfg.hostPort}" else null;
  optStr = description: lib.mkOption { type = lib.types.nullOr lib.types.str; default = null; inherit description; };
in
{
  options.services.fyr = {
    enable = lib.mkEnableOption "the Fyr host daemon";
    package = lib.mkOption {
      type = lib.types.package;
      default = self.packages.${pkgs.stdenv.hostPlatform.system}.fyr;
      description = "Provides the launcher and the seed fyrd.";
    };
    user = lib.mkOption {
      type = lib.types.str;
      description = "The user Fyr and its agents run as (they use this user's logins and repositories).";
    };
    port = lib.mkOption {
      type = lib.types.port;
      default = 7777;
      description = "Loopback port (v0 serves loopback only, decision 0002).";
    };
    ownerLogin = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      example = "you@example.com";
      description = ''
        The Tailscale login that owns this Fyr over the tailnet (V0.14). Unset,
        fyrd takes this machine's Tailscale user. A tagged node (tag:server)
        has no user, so it must be set there, or Fyr serves no browser over
        the tailnet (decision 0028, slice 0).
      '';
    };
    permissionMode = lib.mkOption {
      type = lib.types.str;
      default = "auto";
      description = "claude --permission-mode for every profile that does not set its own.";
    };
    idleStopMinutes = lib.mkOption {
      type = lib.types.int;
      default = 10;
      description = "Stop an agent process after this long without a run (SPEC V0.7).";
    };
    slots = lib.mkOption {
      type = lib.types.ints.unsigned;
      default = 2;
      description = "How many heavy jobs (test suites, builds, e2e runs) the agents run at once across all threads; the rest wait in a queue that survives a restart. 0 turns slots off: no limit, nothing queues, and the deploy's tests take no slot (SPEC V0.86).";
    };
    role = lib.mkOption {
      type = lib.types.enum [ "both" "host" "front" ];
      default = "both";
      description = ''
        What this fyrd does (SPEC 0.5 V0.51, decision 0016): `host` runs
        threads and serves no page; `front` serves the client from `hosts`;
        `both` is a host with its own front, which reaches it over a loopback
        link through the host protocol.
      '';
    };
    hosts = lib.mkOption {
      type = lib.types.listOf (lib.types.submodule {
        options = {
          name = lib.mkOption { type = lib.types.str; description = "The host's name, shown in the client."; };
          url = lib.mkOption { type = lib.types.str; example = "http://meros-server:7778"; description = "Where the host serves fronts (its `hostListen`) on the tailnet."; };
        };
      });
      default = [ ];
      description = "A front's other hosts (V0.51). In `both` the local host comes first and needs no entry.";
    };
    hostListen = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      example = "100.101.102.103:7778";
      description = ''
        A tailnet address where this host also serves fronts on other
        machines (V0.51), or `tailnet:PORT` for this machine's tailnet IPv4
        address (V0.54). Only peers whose Tailscale identity is the owner's
        login are served. Null: loopback only (or `hostPort`).
      '';
    };
    hostPort = lib.mkOption {
      type = lib.types.nullOr lib.types.port;
      default = null;
      example = 7778;
      description = ''
        Serve fronts on other machines on this port of this machine's
        tailnet address (V0.54): sets `hostListen` to `tailnet:PORT` and
        opens the port on the tailscale0 interface only. `hostListen` wins.
      '';
    };
    profiles = lib.mkOption {
      type = lib.types.listOf (lib.types.attrsOf lib.types.anything);
      default = [ ];
      description = ''
        Agent profiles (SPEC V0.11): a claude-compatible binary with a name,
        colour and model list. Empty means: read T3 Code's provider instances,
        else plain `claude`. Ids should match T3's instance ids so adopted
        threads keep their profile.
      '';
    };
    scanRoots = lib.mkOption {
      type = lib.types.listOf lib.types.str;
      default = [ ];
      example = [ "~/git" "~/src" ];
      description = ''
        Extra folders the host folder picker scans for git repositories, two
        levels deep (SPEC V0.42). The projects' folders are always scanned.
        `~` is the user's home folder; only folders inside it are scanned.
      '';
    };
    quota = lib.mkOption {
      type = lib.types.attrsOf (lib.types.submodule {
        options = {
          kind = lib.mkOption {
            type = lib.types.enum [ "anthropic-oauth" "deepseek" "openrouter" "none" ];
            description = ''
              anthropic-oauth: a Claude subscription, read with the CLI's stored
              login. deepseek, openrouter: a prepaid balance, read with an API key.
              none: show nothing.
            '';
          };
          configDir = optStr ''
            anthropic-oauth: the CLI config folder holding .credentials.json.
            Default: the profile's CLAUDE_CONFIG_DIR, else ~/.claude.
          '';
          keyFile = optStr ''
            deepseek, openrouter: a file holding the API key (a runtime path such
            as /run/secrets/deepseek, never a Nix path: that would copy the key
            into the store). Wins over keyEnv. `~` is the user's home folder.
          '';
          keyEnv = optStr ''
            deepseek, openrouter: the environment variable with the API key,
            looked up in the profile's env, then the daemon's. Default
            DEEPSEEK_API_KEY or OPENROUTER_API_KEY.
          '';
          baseURL = optStr "Overrides the provider's API root.";
        };
      });
      default = { };
      example = lib.literalExpression ''
        {
          claudeNorban = { kind = "anthropic-oauth"; configDir = "~/.claude-norban"; };
          claudeDeepseek = { kind = "deepseek"; keyFile = "/run/secrets/deepseek-api-key"; };
        }
      '';
      description = ''
        Where each profile's provider quota is read (SPEC 0.5 V0.44), by
        profile id. Wins over a profile's own `quota`. A profile in neither is
        anthropic-oauth when its binary is plain `claude` and its env sets no
        ANTHROPIC_BASE_URL, else none. fyrd reads the logins at runtime and
        never sends a token or key to the client.
      '';
    };
    path = lib.mkOption {
      type = lib.types.listOf lib.types.package;
      default = [ ];
      description = "Extra packages on the unit PATH, such as claude wrappers.";
    };
    environment = lib.mkOption {
      type = lib.types.attrsOf lib.types.str;
      default = { };
      description = "Extra environment for the daemon and the agents it starts.";
    };
    tailnet = {
      enable = lib.mkEnableOption "publishing Fyr on the tailnet with Tailscale Serve (SPEC 0.5 V0.14)";
      port = lib.mkOption {
        type = lib.types.port;
        default = 7443;
        description = "HTTPS port on the tailnet. Only the owner's Tailscale login is served.";
      };
    };
    memoryHigh = lib.mkOption {
      type = lib.types.str;
      default = "13G";
      description = ''
        MemoryHigh of the whole unit when `cgroups.enable` is off. With
        cgroups the limits sit on agents/ only, so the daemon is never
        throttled for memory its agents use (V0.80).
      '';
    };
    memoryMax = lib.mkOption {
      type = lib.types.str;
      default = "14G";
      description = "MemoryMax of the whole unit when `cgroups.enable` is off.";
    };
    # SPEC 0.5 V0.80, after T3's launcher (t3code.nix in the machine's
    # config): the unit delegates cpu and memory, fyrd moves itself into
    # server/ and starts every agent CLI in agents/<thread id>/, the
    # deploy's build and tests in agents/self-deploy/. The budget sits on
    # agents/. A machine-wide QoS script may rewrite the CPU cap at
    #   /sys/fs/cgroup/system.slice/fyr.service/agents/cpu.max
    # (T3's agents-lock-cap lifts it while the screen is locked).
    cgroups = {
      enable = lib.mkOption {
        type = lib.types.bool;
        default = true;
        description = "Split the unit into server/ and agents/<thread>/ (Delegate=cpu memory) with the budget below.";
      };
      serverMemoryLow = lib.mkOption {
        type = lib.types.str;
        default = "1G";
        description = ''
          memory.low of server/ (the launcher and fyrd), kept out of reclaim.
          The unit gets the same MemoryLow, since a cgroup keeps no more
          protection than its parent has; system.slice needs one too.
        '';
      };
      agentsCpuCores = lib.mkOption {
        type = lib.types.ints.positive;
        default = 4;
        description = "cpu.max of agents/, in cores: every thread together, the deploy included.";
      };
      agentsMemoryHigh = lib.mkOption {
        type = lib.types.str;
        default = "13G";
        description = "memory.high of agents/: the kernel throttles the agents past it.";
      };
      agentsMemoryMax = lib.mkOption {
        type = lib.types.str;
        default = "14G";
        description = "memory.max of agents/: the kernel OOM-kills an agent process past it. Keep the gap to memoryHigh narrow.";
      };
      agentsMemorySwapMax = lib.mkOption {
        type = lib.types.str;
        default = "16G";
        description = ''
          memory.swap.max of agents/. T3's notes (2026-11-13): an 8 GiB cap left
          the agents stalling between high and max; 24G spilled swap onto disk.
        '';
      };
    };
  };

  config = lib.mkIf cfg.enable {
    assertions = [
      { assertion = cfg.role != "front" || cfg.hosts != [ ]; message = "services.fyr: role front needs hosts."; }
      { assertion = !(cfg.tailnet.enable && cfg.role == "host"); message = "services.fyr: tailnet publishes the front; role host serves no page."; }
      { assertion = hostListen == null || cfg.role != "front"; message = "services.fyr: hostListen and hostPort need the host role (host or both)."; }
    ];
    networking.firewall.interfaces.tailscale0.allowedTCPPorts = lib.mkIf (cfg.hostPort != null && cfg.hostListen == null) [ cfg.hostPort ];
    environment.systemPackages = [ cfg.package ];
    environment.etc."fyr/config.json".text = configJSON;

    # Tailscale Serve keeps its config in tailscaled; this unit sets it at boot
    # so the address is declared here, not in a manual command.
    systemd.services.fyr-tailnet = lib.mkIf cfg.tailnet.enable {
      description = "Publish Fyr on the tailnet (Tailscale Serve)";
      after = [ "tailscaled.service" "fyr.service" ];
      wants = [ "tailscaled.service" ];
      wantedBy = [ "multi-user.target" ];
      serviceConfig = {
        Type = "oneshot";
        RemainAfterExit = true;
        # V0.130 (decision 0028): Serve proxies to fyrd's own socket, the only
        # place fyrd believes the Tailscale-User-Login header; on the loopback
        # port any local process could send it.
        ExecStart = "${config.services.tailscale.package}/bin/tailscale serve --bg --https=${toString cfg.tailnet.port} unix:${home}/.local/share/fyr/serve.sock";
        ExecStop = "${config.services.tailscale.package}/bin/tailscale serve --https=${toString cfg.tailnet.port} off";
      };
    };

    systemd.services.fyr = {
      description = "Fyr host daemon";
      # A host on the tailnet listens on its tailnet address: tailscaled first.
      # Until the address is up fyrd exits and the unit restarts it.
      after = [ "network-online.target" ] ++ lib.optional (hostListen != null) "tailscaled.service";
      wants = [ "network-online.target" ] ++ lib.optional (hostListen != null) "tailscaled.service";
      wantedBy = [ "multi-user.target" ];
      # A switch must not restart Fyr: that ends every running agent turn.
      # Changes apply at the next manual restart or boot, as with t3code.
      restartIfChanged = false;
      # nix: self-deploys build with `nix develop <checkout>` (decision 0007).
      path = cfg.path ++ (with pkgs; [
        claude-code git gh openssh nix gnumake bash coreutils gnugrep gnused findutils ripgrep
      ]);
      environment = {
        HOME = home;
        TZ = if config.time.timeZone != null then config.time.timeZone else "UTC";
        NIX_PATH = lib.concatStringsSep ":" config.nix.nixPath;
      } // cfg.environment;
      serviceConfig = {
        User = cfg.user;
        Group = config.users.users.${cfg.user}.group;
        WorkingDirectory = home;
        ExecStart = lib.concatStringsSep " " [
          "${cfg.package}/bin/fyr"
          "-seed ${cfg.package}/bin/fyrd"
          "-- -config /etc/fyr/config.json"
        ];
        Restart = "always";
        RestartSec = 5;
        # Agent turns are child processes; SIGINT to the group ends them cleanly.
        KillSignal = "SIGINT";
        TimeoutStopSec = 30;
        Nice = 10;
        IOWeight = 50;
      } // (if cfg.cgroups.enable then {
        # V0.80: systemd hands the subtree to the user; fyrd splits it and
        # sets the budget on agents/ (see the cgroups options).
        Delegate = "cpu memory";
        MemoryLow = cfg.cgroups.serverMemoryLow;
      } else {
        MemoryHigh = cfg.memoryHigh;
        MemoryMax = cfg.memoryMax;
      });
    };
  };
}
