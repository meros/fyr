import Foundation
import ServiceManagement

// Agent is fyrd's launchd agent, registered with SMAppService (decision
// 0037): a per-user agent from the plist inside the bundle, no admin rights,
// listed under Login Items. Its first process is Contents/MacOS/fyr-host.
enum Agent {
  static let info = Bundle.main.infoDictionary ?? [:]
  static let plistName = info["FyrAgentPlist"] as? String ?? "fyr.agent.plist"
  static let label = (plistName as NSString).deletingPathExtension
  static var service: SMAppService { .agent(plistName: plistName) }
  // The data folder and log file, as the agent's plist gives them to fyr-host.
  static let data = ((info["FyrData"] as? String) ?? "~/.local/share/fyr" as String).expandingTilde
  static let log = ((info["FyrLog"] as? String) ?? "~/Library/Logs/fyr.log" as String).expandingTilde
  // The script install's launchd labels (scripts/install-macos.sh) that this
  // app takes over: the same data folder, so only one may run.
  static let replaces = info["FyrReplaces"] as? [String] ?? []

  // The owner paused Fyr from the menu; a new start of the app keeps it so.
  static var paused: Bool {
    get { UserDefaults.standard.bool(forKey: "paused") }
    set { UserDefaults.standard.set(newValue, forKey: "paused") }
  }

  enum State { case running, paused, needsApproval, failed(String) }

  static var state: State {
    switch service.status {
    case .enabled: return .running
    case .requiresApproval: return .needsApproval
    default: return paused ? .paused : .failed("not registered")
    }
  }

  // start takes over a script install, then registers the agent (which
  // starts it). It is safe to call when it already runs.
  static func start() -> State {
    paused = false
    replaceScriptInstall()
    if service.status == .enabled {
      if launchctl(["print", "gui/\(getuid())/\(label)"]) == 0 { return .running }
      // Enabled, but launchd holds no job: the registration names an older
      // signature (an ad hoc build replaced in place). Register it anew.
      try? service.unregister()
    }
    do { try service.register() } catch {
      if service.status == .requiresApproval { return .needsApproval }
      return .failed(error.localizedDescription)
    }
    return state
  }

  // pause unregisters the agent: launchd stops fyr-host, the launcher and
  // the agents under it.
  static func pause() throws {
    paused = true
    try service.unregister()
  }

  // uninstall unregisters both the background agent and Login Items, turns
  // off Fyr's Tailscale Serve if configured, cleans up any leftover script
  // install, and optionally removes Fyr's data and log (decision 0037).
  static func uninstall(removeData: Bool) {
    try? service.unregister()
    try? SMAppService.mainApp.unregister()
    replaceScriptInstall()
    turnOffTailscaleServe()
    if let id = Bundle.main.bundleIdentifier {
      UserDefaults.standard.removePersistentDomain(forName: id)
      UserDefaults.standard.synchronize()
    }
    if removeData {
      let fm = FileManager.default
      try? fm.removeItem(atPath: data)
      try? fm.removeItem(atPath: log)
    }
  }

  // trashSelf moves the running .app bundle to the Trash during uninstall.
  static func trashSelf() {
    let url = Bundle.main.bundleURL
    guard url.pathExtension == "app" else { return }
    try? FileManager.default.trashItem(at: url, resultingItemURL: nil)
  }

  private static func turnOffTailscaleServe() {
    let candidates = [
      "/opt/homebrew/bin/tailscale",
      "/usr/local/bin/tailscale",
      "/Applications/Tailscale.app/Contents/MacOS/Tailscale",
    ]
    guard let ts = candidates.first(where: { FileManager.default.isExecutableFile(atPath: $0) }) else { return }
    let p = Process()
    p.executableURL = URL(fileURLWithPath: ts)
    p.arguments = ["serve", "--https=7443", "off"]
    p.standardOutput = FileHandle.nullDevice
    p.standardError = FileHandle.nullDevice
    try? p.run()
    p.waitUntilExit()
  }

  // replaceScriptInstall stops the script install's jobs, removes their
  // plists and the script's ~/Applications/Fyr.app, and keeps the data
  // (decision 0037, "Moving script installs").
  static func replaceScriptInstall() {
    let fm = FileManager.default
    let agents = "~/Library/LaunchAgents".expandingTilde
    for l in replaces {
      for job in [l, l + ".update"] {
        let plist = "\(agents)/\(job).plist"
        guard fm.fileExists(atPath: plist) else { continue }
        NSLog("Fyr: replacing the script install's \(job)")
        launchctl(["bootout", "gui/\(getuid())/\(job)"])
        for _ in 0..<50 where launchctl(["print", "gui/\(getuid())/\(job)"]) == 0 { usleep(200_000) }
        try? fm.removeItem(atPath: plist)
      }
      let app = "~/Applications/Fyr.app".expandingTilde
      if app != Bundle.main.bundlePath, Bundle(path: app)?.bundleIdentifier == l + ".app" {
        try? fm.removeItem(atPath: app)
      }
    }
  }

  @discardableResult
  static func launchctl(_ args: [String]) -> Int32 {
    let p = Process()
    p.executableURL = URL(fileURLWithPath: "/bin/launchctl")
    p.arguments = args
    p.standardOutput = FileHandle.nullDevice
    p.standardError = FileHandle.nullDevice
    do { try p.run() } catch { return -1 }
    p.waitUntilExit()
    return p.terminationStatus
  }
}

extension String {
  var expandingTilde: String { (self as NSString).expandingTildeInPath }
}
