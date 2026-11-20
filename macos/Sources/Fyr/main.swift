import AppKit
import ServiceManagement

// Fyr.app's shell (decision 0037): registers fyrd's agent, keeps a menu-bar
// item and shows the window. Everything else is in the web page, which
// serves desktop and phone alike.
final class App: NSObject, NSApplicationDelegate, NSMenuDelegate {
  let window = Window()
  let notifier = Notifier()
  var item: NSStatusItem!
  var atLogin = false
  let stateItem = NSMenuItem(title: "", action: nil, keyEquivalent: "")
  let pauseItem = NSMenuItem(title: "Pause Fyr", action: #selector(togglePause), keyEquivalent: "")

  // macOS says in the open event whether it started the app as a login item.
  func applicationWillFinishLaunching(_ n: Notification) {
    let e = NSAppleEventManager.shared().currentAppleEvent
    atLogin = e?.eventID == kAEOpenApplication && e?.paramDescriptor(forKeyword: keyAEPropData)?.enumCodeValue == keyAELaunchedAsLogInItem
  }

  func applicationDidFinishLaunching(_ n: Notification) {
    let args = CommandLine.arguments
    if args.count == 3, args[1] == "--agent" { agentCommand(args[2]) }
    NSApp.mainMenu = mainMenu()
    item = NSStatusBar.system.statusItem(withLength: NSStatusItem.squareLength)
    item.button?.image = NSImage(named: "MenuIcon") ?? NSImage(systemSymbolName: "light.beacon.max", accessibilityDescription: "Fyr")
    item.button?.image?.isTemplate = true
    let m = NSMenu()
    m.delegate = self
    m.addItem(withTitle: "Open Fyr", action: #selector(open), keyEquivalent: "")
    m.addItem(withTitle: "Open in Browser", action: #selector(openInBrowser), keyEquivalent: "")
    m.addItem(.separator())
    stateItem.isEnabled = false
    m.addItem(stateItem)
    m.addItem(pauseItem)
    m.addItem(withTitle: "Show Log", action: #selector(showLog), keyEquivalent: "")
    m.addItem(.separator())
    m.addItem(withTitle: "Quit (Fyr keeps running)", action: #selector(NSApplication.terminate(_:)), keyEquivalent: "")
    item.menu = m
    if !Agent.paused { report(Agent.start()) }
    // V0.mac-notify: the app starts at login, so notifications arrive with
    // the window closed; at login it stays in the menu bar.
    if SMAppService.mainApp.status != .enabled { try? SMAppService.mainApp.register() }
    notifier.open = { [weak self] path in self?.window.show(path: path) }
    window.notifier = notifier
    notifier.start()
    if !atLogin { window.show() }
  }

  // A click on the Dock icon or a second open of the app shows the window.
  func applicationShouldHandleReopen(_ s: NSApplication, hasVisibleWindows: Bool) -> Bool {
    window.show()
    return false
  }

  func applicationShouldTerminateAfterLastWindowClosed(_ s: NSApplication) -> Bool { false }

  func menuNeedsUpdate(_ m: NSMenu) {
    switch Agent.state {
    case .running: stateItem.title = "Fyr is running"
    case .paused: stateItem.title = "Fyr is paused"
    case .needsApproval: stateItem.title = "Fyr waits for approval in Login Items"
    case .failed(let why): stateItem.title = "Fyr is not running: \(why)"
    }
    pauseItem.title = Agent.paused ? "Resume Fyr" : "Pause Fyr"
  }

  // report tells the owner what only they can fix: the agent needs their
  // approval in System Settings, or did not register.
  func report(_ s: Agent.State) {
    switch s {
    case .needsApproval:
      let a = NSAlert()
      a.messageText = "Allow Fyr in Login Items"
      a.informativeText = "macOS asks you to allow Fyr to run in the background. Turn Fyr on under System Settings → General → Login Items."
      a.addButton(withTitle: "Open Login Items")
      a.addButton(withTitle: "Later")
      if a.runModal() == .alertFirstButtonReturn { SMAppService.openSystemSettingsLoginItems() }
    case .failed(let why):
      let a = NSAlert()
      a.messageText = "Fyr could not start"
      a.informativeText = why
      a.runModal()
    default: break
    }
  }

  @objc func open() { window.show() }
  @objc func openInBrowser() { window.openInBrowser() }
  @objc func settings() { window.show(path: "/?settings") }
  @objc func reload() { window.reload() }
  @objc func showLog() { NSWorkspace.shared.open(URL(fileURLWithPath: "~/Library/Logs/fyr.log".expandingTilde)) }

  @objc func togglePause() {
    if Agent.paused {
      report(Agent.start())
      return
    }
    let a = NSAlert()
    a.messageText = "Pause Fyr?"
    a.informativeText = "Fyr stops on this Mac, with every running agent, until you resume it here."
    a.addButton(withTitle: "Pause")
    a.addButton(withTitle: "Cancel")
    guard a.runModal() == .alertFirstButtonReturn else { return }
    do { try Agent.pause() } catch { report(.failed(error.localizedDescription)) }
  }

  // mainMenu: the app menu, Edit (the page's copy and paste need it), View
  // and Window.
  func mainMenu() -> NSMenu {
    let main = NSMenu()
    func sub(_ title: String, _ items: [NSMenuItem]) {
      let i = NSMenuItem(title: title, action: nil, keyEquivalent: "")
      let m = NSMenu(title: title)
      items.forEach(m.addItem)
      i.submenu = m
      main.addItem(i)
    }
    func it(_ t: String, _ a: Selector?, _ k: String, _ mods: NSEvent.ModifierFlags = .command) -> NSMenuItem {
      let i = NSMenuItem(title: t, action: a, keyEquivalent: k)
      i.keyEquivalentModifierMask = mods
      return i
    }
    sub("Fyr", [
      it("About Fyr", #selector(NSApplication.orderFrontStandardAboutPanel(_:)), ""),
      .separator(),
      it("Settings…", #selector(settings), ","),
      .separator(),
      it("Hide Fyr", #selector(NSApplication.hide(_:)), "h"),
      it("Hide Others", #selector(NSApplication.hideOtherApplications(_:)), "h", [.command, .option]),
      .separator(),
      it("Quit Fyr", #selector(NSApplication.terminate(_:)), "q"),
    ])
    sub("Edit", [
      it("Undo", Selector(("undo:")), "z"),
      it("Redo", Selector(("redo:")), "z", [.command, .shift]),
      .separator(),
      it("Cut", #selector(NSText.cut(_:)), "x"),
      it("Copy", #selector(NSText.copy(_:)), "c"),
      it("Paste", #selector(NSText.paste(_:)), "v"),
      it("Select All", #selector(NSText.selectAll(_:)), "a"),
    ])
    sub("View", [it("Reload", #selector(reload), "r")])
    sub("Window", [
      it("Close", #selector(NSWindow.performClose(_:)), "w"),
      it("Minimize", #selector(NSWindow.performMiniaturize(_:)), "m"),
    ])
    return main
  }
}

// Fyr --agent register|unregister|status: the agent's registration without
// the window, for scripts (the install test, a developer's rebuild). It runs
// once the app has started: SMAppService's calls block before that.
func agentCommand(_ cmd: String) -> Never {
  switch cmd {
  case "register":
    if case .failed(let why) = Agent.start() { FileHandle.standardError.write("register: \(why)\n".data(using: .utf8)!) }
  case "unregister": // and out of Login Items: what an uninstall wants
    try? Agent.pause()
    try? SMAppService.mainApp.unregister()
  case "status": break
  default:
    FileHandle.standardError.write("usage: Fyr --agent register|unregister|status\n".data(using: .utf8)!)
    exit(64)
  }
  switch Agent.state {
  case .running: print("running")
  case .paused: print("paused")
  case .needsApproval: print("needs approval in Login Items")
  case .failed(let why): print("not running: \(why)")
  }
  exit(0)
}

let app = NSApplication.shared
let delegate = App()
app.delegate = delegate
app.setActivationPolicy(.accessory)
app.run()
