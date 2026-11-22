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
    # V0.agents-unit: with agentsUnit the budget sits on fyr-agents.slice
    # instead and each thread starts in a scope there; the values stay for a
    # host where the scopes do not work and fyrd falls back to the split.
    cgroups = {
      enable = true;
      inherit (cfg.cgroups) serverMemoryLow agentsMemoryHigh agentsMemoryMax agentsMemorySwapMax;
    } // lib.optionalAttrs (cfg.cgroups.agentsCpuCores != null) {
      agentsCpuMax = "${toString (cfg.cgroups.agentsCpuCores * 100000)} 100000";
    } // lib.optionalAttrs cfg.cgroups.agentsUnit {
      agentsSlice = agentsSlice;
      systemdRun = "${config.systemd.package}/bin/systemd-run";
      systemctl = "${config.systemd.package}/bin/systemctl";
    };
  } // lib.optionalAttrs (cfg.role != "both") { role = cfg.role; }
    // lib.optionalAttrs (cfg.hosts != [ ]) { hosts = cfg.hosts; }
    // lib.optionalAttrs (hostListen != null) { hostListen = hostListen; }
    // lib.optionalAttrs (cfg.peerPort != null) { peerListen = "0.0.0.0:${toString cfg.peerPort}"; }
    // lib.optionalAttrs (cfg.ownerLogin != null) { ownerLogin = cfg.ownerLogin; });
  # V0.54: hostPort listens on this machine's tailnet address, which fyrd
  # reads from tailscaled at start; hostListen names an address by hand.
  hostListen = if cfg.hostListen != null then cfg.hostListen
    else if cfg.hostPort != null then "tailnet:${toString cfg.hostPort}" else null;
  # V0.agents-unit (decision 0036 (B), #221): the agents' slice. Its parent
  # is fyr.slice (systemd's dash rule), so it is outside system.slice.
  agentsSlice = "fyr-agents.slice";
  agentsUnit = cfg.cgroups.enable && cfg.cgroups.agentsUnit;
  # V0.psi-trigger-slice: fyrd (the service user) writes its PSI trigger
  # (V0.guard-wake) to the slice's memory.pressure, which systemd makes
  # root:root 0644. This hands that one file to the user, as root, before
  # fyrd starts. The path is the one systemd reports for the slice. Writing
  # a trigger is the only thing the file takes, and the kernel lets an
  # unprivileged writer set only a window that is a multiple of 2 s; the
  # slice's limits (memory.high, cpu.max, ...) stay root's. A failure is
  # logged and does not stop the unit: fyrd then keeps the 10 s tick.
  agentsPressureChown = pkgs.writeShellScript "fyr-agents-pressure-chown" ''
    cg=$(${config.systemd.package}/bin/systemctl show -P ControlGroup ${agentsSlice})
    f=/sys/fs/cgroup$cg/memory.pressure
    if [ -z "$cg" ] || ! ${pkgs.coreutils}/bin/chown ${lib.escapeShellArg cfg.user} "$f"; then
      echo "fyr: could not give ${cfg.user} the PSI trigger on ${agentsSlice} ($f); the guard reads every 10 s" >&2
    fi
  '';
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
    peerPort = lib.mkOption {
      type = lib.types.nullOr lib.types.port;
      default = null;
      example = 7779;
      description = ''
        The peer port (decision 0030 3.6, V0.158): other Fyr installs reach this
        one here, over TLS 1.3 pinned to install keys, on any interface (the
        tailnet, a LAN). It serves only installs in this install's roster;
        installs of the same tailnet user pair by themselves (Tailscale Serve
        must be up on both). Null: no peer port.
      '';
    };
    openFirewall = lib.mkOption {
      type = lib.types.bool;
      default = false;
      description = "Open `peerPort` in the firewall. The port speaks only mutual TLS with keys from the roster.";
    };
    profiles = lib.mkOption {
      type = lib.types.listOf (lib.types.attrsOf lib.types.anything);
      default = [ ];
      description = ''
        Agent profiles (SPEC V0.11): a claude-compatible binary with a name,
        colour and model list. Empty means plain `claude`.
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
    # V0.agents-budget-share (#198): every memory default is a share of the
    # machine (systemd and fyrd read "40%" against the RAM, or the swap for
    # a swap limit), never a size that suits one machine.
    memoryHigh = lib.mkOption {
      type = lib.types.str;
      default = "40%";
      description = ''
        MemoryHigh of the whole unit when `cgroups.enable` is off. With
        cgroups the limits sit on agents/ only, so the daemon is never
        throttled for memory its agents use (V0.80).
      '';
    };
    memoryMax = lib.mkOption {
      type = lib.types.str;
      default = "46%";
      description = "MemoryMax of the whole unit when `cgroups.enable` is off.";
    };
    # V0.agents-unit (decision 0036 (B), #221), the default: fyrd runs alone
    # in fyr.service, which is root-owned, not delegated, and omitted by
    # systemd-oomd. Each thread's CLI starts in a scope of its own,
    #   /sys/fs/cgroup/fyr.slice/fyr-agents.slice/fyr-agent-<thread>.scope
    # (Delegate=yes, so fyrd makes job/ and undeclared/ in it, and
    # memory.oom.group=1, so oomd ends a whole thread), a self-deploy's
    # commands in fyr-deploy-<n>.scope. The budget and the oomd rule sit on
    # fyr-agents.slice. A host's own tooling may rewrite the CPU cap at
    #   /sys/fs/cgroup/fyr.slice/fyr-agents.slice/cpu.max
    #
    # With agentsUnit off, SPEC 0.5 V0.80: the unit delegates cpu and memory,
    # fyrd moves itself into server/ and starts every agent CLI in
    # agents/<thread id>/, the deploy's build and tests in agents/self-deploy/,
    # and the budget sits on agents/ (the CPU cap is then
    # /sys/fs/cgroup/system.slice/fyr.service/agents/cpu.max).
    cgroups = {
      enable = lib.mkOption {
        type = lib.types.bool;
        default = true;
        description = "Give each thread a cgroup of its own, under the budget below.";
      };
      agentsUnit = lib.mkOption {
        type = lib.types.bool;
        default = true;
        description = ''
          Run the agents outside fyrd's unit, each thread in a scope of
          fyr-agents.slice, which carries the budget below and the oomd rule
          (V0.agents-unit). fyrd's own unit is then not delegated and is
          omitted by systemd-oomd, so an out-of-memory kill never takes fyrd.
          A polkit rule lets the service user start and adjust these scopes,
          and nothing else. Off: the V0.80 split of fyr.service.
          Turning it on takes effect for new threads at the next reload; the
          unit's own settings apply at its next restart.
        '';
      };
      agentsOomPressureLimit = lib.mkOption {
        type = lib.types.nullOr lib.types.str;
        default = "80%";
        description = ''
          systemd-oomd kills a whole thread of the agents' slice once the
          slice's memory stall stays above this (ManagedOOMMemoryPressureLimit);
          null for no rule. Keep it above what Fyr's own throttling produces:
          the guard acts first (decision 0036 (B)). With agentsUnit only.
        '';
      };
      agentsOomPressureDuration = lib.mkOption {
        type = lib.types.nullOr lib.types.str;
        default = null;
        example = "20s";
        description = "ManagedOOMMemoryPressureDurationSec of the agents' slice; null is systemd-oomd's default (30 s).";
      };
      serverMemoryLow = lib.mkOption {
        type = lib.types.str;
        default = "1G";
        description = ''
          memory.low of fyrd and the launcher, kept out of reclaim: the
          unit's MemoryLow (with agentsUnit off, also server/'s, since a
          cgroup keeps no more protection than its parent has);
          system.slice needs one too.
        '';
      };
      agentsCpuCores = lib.mkOption {
        type = lib.types.nullOr lib.types.ints.positive;
        default = null;
        example = 4;
        description = ''
          CPU cap of the agents (the slice, or agents/), in cores: every thread
          together, the deploy included. null, the default, is no cap: the
          agents' low cpu.weight already gives the desktop and fyrd the CPU
          first, and the machine's core count is not known when the
          configuration is built (V0.agents-budget-share).
        '';
      };
      agentsMemoryHigh = lib.mkOption {
        type = lib.types.str;
        default = "40%";
        description = ''
          memory.high of the agents (the slice, or agents/): the kernel
          throttles the agents past it. A percentage is a share of the RAM;
          a size ("12G") is taken as it is.
        '';
      };
      agentsMemoryMax = lib.mkOption {
        type = lib.types.str;
        default = "46%";
        description = "memory.max of the agents (the slice, or agents/): the kernel OOM-kills past it (a whole thread with agentsUnit). Keep the gap to memoryHigh narrow.";
      };
      # V0.share-shadow: the thread share is gone; the option stays so a
      # host config that sets it still evaluates, and does nothing.
      threadMemoryHighPercent = lib.mkOption {
        type = lib.types.ints.between 0 100;
        default = 0;
        visible = false;
        description = "Removed (V0.share-shadow): ignored. A thread's memory.high is no longer set; the per-job limits bound a job.";
      };
      agentsMemorySwapMax = lib.mkOption {
        type = lib.types.str;
        default = "30%";
        description = ''
          memory.swap.max of the agents (the slice, or agents/). A percentage
          is a share of the swap. Too small a cap leaves the agents stalling
          between memory.high and memory.max; no cap lets their cold pages
          fill the disk swap.
        '';
      };
    };

    # Decision 0038: the Linux app.
    desktop = {
      enable = lib.mkEnableOption ''
        Fyr's Linux app (decision 0038): Fyr in its own window, a tray item
        (Open, Open in Browser, Pause or Resume, Show Log, Quit) and the
        desktop's notifications for what needs you. The service user may
        then stop, start and restart fyr.service without a password, which
        is what Pause and Resume do'';
      package = lib.mkOption {
        type = lib.types.package;
        default = self.packages.${pkgs.stdenv.hostPlatform.system}.fyr-desktop;
        description = "Provides fyr-desktop, its .desktop file and icon.";
      };
      autostart = lib.mkOption {
        type = lib.types.bool;
        default = true;
        description = ''
          Start the app at login in the tray, without its window
          (fyr-desktop -background), so notifications arrive from the start.
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
    warnings = lib.optional (cfg.cgroups.threadMemoryHighPercent != 0)
      "services.fyr.cgroups.threadMemoryHighPercent is ignored: Fyr no longer sets a thread's memory.high (V0.share-shadow).";
    networking.firewall.allowedTCPPorts = lib.mkIf (cfg.openFirewall && cfg.peerPort != null) [ cfg.peerPort ];
    networking.firewall.interfaces.tailscale0.allowedTCPPorts = lib.mkIf (cfg.hostPort != null && cfg.hostListen == null) [ cfg.hostPort ];
    environment.systemPackages = [ cfg.package ] ++ lib.optional cfg.desktop.enable cfg.desktop.package; # 0038: the Linux app
    environment.etc."fyr/config.json".text = configJSON;

    # Tailscale Serve keeps its config in tailscaled; this unit sets it at boot
    # so the address is declared here, not in a manual command.
    systemd.services.fyr-tailnet = lib.mkIf cfg.tailnet.enable {
      description = "Publish Fyr on the tailnet (Tailscale Serve)";
      after = [ "tailscaled.service" "fyr.service" ];
      wants = [ "tailscaled.service" ];
      wantedBy = [ "multi-user.target" ];
      # tailscaled started is not tailscaled Running: when a switch restarts
      # it, `tailscale serve` can run while the daemon is in NoState, exit 1
      # ("unexpected state: NoState"), and leave the port unpublished until
      # the next boot. Retry instead.
      startLimitIntervalSec = 120;
      startLimitBurst = 10;
      serviceConfig = {
        Type = "oneshot";
        RemainAfterExit = true;
        Restart = "on-failure";
        RestartSec = 5;
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
      # V0.psi-trigger-slice: with agentsUnit, the agents' slice too, so that
      # its cgroup exists before ExecStartPre hands over its memory.pressure
      # (a slice nothing has asked for is not started and has no cgroup).
      after = [ "network-online.target" ] ++ lib.optional (hostListen != null) "tailscaled.service" ++ lib.optional agentsUnit agentsSlice;
      wants = [ "network-online.target" ] ++ lib.optional (hostListen != null) "tailscaled.service" ++ lib.optional agentsUnit agentsSlice;
      wantedBy = [ "multi-user.target" ];
      # A switch must not restart the unit: that ends every running agent turn.
      # A new package applies at the next manual restart or boot.
      restartIfChanged = false;
      # V0.180 (#264): a changed config.json reloads instead: ExecReload sends
      # SIGHUP to the launcher, which restarts fyrd on the new file while its
      # runner keeps the agents (decision 0023). A config-only change differs in
      # X-Reload-Triggers alone, which switch-to-configuration reloads even with
      # restartIfChanged off. (Not reloadIfChanged: with reloadTriggers NixOS
      # warns, and it would reload on every package change too.)
      reloadTriggers = [ configJSON ];
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
        ExecReload = "${pkgs.coreutils}/bin/kill -HUP $MAINPID";
        Restart = "always";
        RestartSec = 5;
        # Agent turns are child processes; SIGINT to the group ends them cleanly.
        KillSignal = "SIGINT";
        TimeoutStopSec = 30;
        Nice = 10;
        IOWeight = 50;
      } // (if agentsUnit then {
        # V0.agents-unit: the unit holds fyrd and the launcher only. It is
        # not delegated, so its cgroup stays root's and systemd-oomd honours
        # the omit on every rule (/, system.slice, ...), not only one on this
        # unit; the kernel's OOM killer passes it over too. The agents reset
        # their oom_score_adj to 0 as they start.
        MemoryLow = cfg.cgroups.serverMemoryLow;
        ManagedOOMPreference = "omit";
        OOMScoreAdjust = -500;
        # V0.psi-trigger-slice: "+" runs it as root. wants/after below start
        # the slice first, so its cgroup exists. Each start of the unit runs
        # it again, so a slice that was stopped and made anew (its file root's
        # again) is handed over at fyrd's next start.
        ExecStartPre = "+${agentsPressureChown}";
      } else if cfg.cgroups.enable then {
        # V0.80: systemd hands the subtree to the user; fyrd splits it and
        # sets the budget on agents/ (see the cgroups options).
        Delegate = "cpu memory";
        MemoryLow = cfg.cgroups.serverMemoryLow;
      } else {
        MemoryHigh = cfg.memoryHigh;
        MemoryMax = cfg.memoryMax;
      });
    };

    # V0.agents-unit: the agents' budget and the oomd rule. fyrd starts each
    # thread in a scope here with systemd-run (Delegate=yes, OOMPolicy=kill,
    # PartOf=fyr.service, so stopping the unit stops its agents as before).
    systemd.slices.fyr-agents = lib.mkIf agentsUnit {
      description = "Fyr's agents (V0.agents-unit)";
      sliceConfig = lib.optionalAttrs (cfg.cgroups.agentsCpuCores != null) {
        CPUQuota = "${toString (cfg.cgroups.agentsCpuCores * 100)}%";
      } // {
        MemoryHigh = cfg.cgroups.agentsMemoryHigh;
        MemoryMax = cfg.cgroups.agentsMemoryMax;
        MemorySwapMax = cfg.cgroups.agentsMemorySwapMax;
      } // lib.optionalAttrs (cfg.cgroups.agentsOomPressureLimit != null) {
        ManagedOOMMemoryPressure = "kill";
        ManagedOOMMemoryPressureLimit = cfg.cgroups.agentsOomPressureLimit;
      } // lib.optionalAttrs (cfg.cgroups.agentsOomPressureDuration != null) {
        ManagedOOMMemoryPressureDurationSec = cfg.cgroups.agentsOomPressureDuration;
      };
    };

    # V0.agents-unit: the service user may start Fyr's scopes and set their
    # properties (cpu.weight, memory.high), nothing else: no other unit, no
    # stop, no kill. systemd passes the unit name and the verb to polkit on
    # StartTransientUnit and SetUnitProperties (src/core/dbus-util.c,
    # bus_verify_manage_units_async_impl). A scope runs no command of its
    # own; it only takes processes in.
    security.polkit.enable = lib.mkIf (agentsUnit || cfg.desktop.enable) (lib.mkDefault true);
    security.polkit.extraConfig = lib.mkMerge [ (lib.mkIf agentsUnit ''
      // Fyr (services.fyr.cgroups.agentsUnit): the agents' scopes.
      polkit.addRule(function (action, subject) {
        if (action.id == "org.freedesktop.systemd1.manage-units" && subject.user == ${builtins.toJSON cfg.user}) {
          var unit = action.lookup("unit"), verb = action.lookup("verb");
          if (unit && /^fyr-(agent|deploy)-[A-Za-z0-9_.-]+\.scope$/.test(unit) && (verb == "start" || verb == "set-property")) {
            return polkit.Result.YES;
          }
        }
      });
    '')
    # Decision 0038: the app's Pause and Resume, and nothing else of the unit.
    (lib.mkIf cfg.desktop.enable ''
      // Fyr (services.fyr.desktop): Pause and Resume in the tray.
      polkit.addRule(function (action, subject) {
        if (action.id == "org.freedesktop.systemd1.manage-units" && subject.user == ${builtins.toJSON cfg.user}) {
          var unit = action.lookup("unit"), verb = action.lookup("verb");
          if (unit == "fyr.service" && (verb == "start" || verb == "stop" || verb == "restart")) {
            return polkit.Result.YES;
          }
        }
      });
    '') ];

    environment.etc."xdg/autostart/io.github.meros.Fyr.desktop" = lib.mkIf (cfg.desktop.enable && cfg.desktop.autostart) {
      text = ''
        [Desktop Entry]
        Type=Application
        Name=Fyr
        Comment=Fyr in the tray, with its notifications
        Exec=${lib.getExe cfg.desktop.package} -background
        Icon=io.github.meros.Fyr
        Terminal=false
        X-GNOME-Autostart-enabled=true
      '';
    };
  };
}
