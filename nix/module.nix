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
    profiles = cfg.profiles;
    scanRoots = cfg.scanRoots;
    quota = lib.mapAttrs (_: q: lib.filterAttrs (_: v: v != null) q) cfg.quota;
  } // lib.optionalAttrs (cfg.role != "both") { role = cfg.role; }
    // lib.optionalAttrs (cfg.hosts != [ ]) { hosts = cfg.hosts; }
    // lib.optionalAttrs (cfg.hostListen != null) { hostListen = cfg.hostListen; });
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
          url = lib.mkOption { type = lib.types.str; example = "http://host-b:7778"; description = "Where the host serves fronts (its `hostListen`) on the tailnet."; };
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
        machines (V0.51). Only peers whose Tailscale identity is the owner's
        login are served. Null: loopback only.
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
          claudeAcme = { kind = "anthropic-oauth"; configDir = "~/.claude-acme"; };
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
      description = "MemoryHigh of the unit (daemon and agents). The governor (M4) refines this per session.";
    };
    memoryMax = lib.mkOption {
      type = lib.types.str;
      default = "14G";
      description = "MemoryMax of the unit.";
    };
  };

  config = lib.mkIf cfg.enable {
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
        ExecStart = "${config.services.tailscale.package}/bin/tailscale serve --bg --https=${toString cfg.tailnet.port} http://127.0.0.1:${toString cfg.port}";
        ExecStop = "${config.services.tailscale.package}/bin/tailscale serve --https=${toString cfg.tailnet.port} off";
      };
    };

    systemd.services.fyr = {
      description = "Fyr host daemon";
      after = [ "network-online.target" ];
      wants = [ "network-online.target" ];
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
        MemoryHigh = cfg.memoryHigh;
        MemoryMax = cfg.memoryMax;
        Nice = 10;
        IOWeight = 50;
      };
    };
  };
}
