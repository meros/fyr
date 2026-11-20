import AppKit
import WebKit

// Window shows Fyr's page in a WKWebView (decision 0037). It loads the
// origin it signed in on before; a 401 there (enforce mode, no session)
// asks the owner socket for a one-time link, so a new session is minted only
// when the old one is gone. Links to other origins open in the browser.
final class Window: NSObject, NSWindowDelegate, WKNavigationDelegate, WKUIDelegate {
  private var window: NSWindow?
  private var web: WKWebView!
  private var signingIn = false
  private var paints: [NSKeyValueObservation] = []

  // The origin of the last one-time link, e.g. http://fyr.localhost:7777.
  private var origin: URL? {
    get { UserDefaults.standard.url(forKey: "origin") }
    set { UserDefaults.standard.set(newValue, forKey: "origin") }
  }

  func show(path: String? = nil) {
    if window == nil { make() }
    NSApp.setActivationPolicy(.regular)
    window?.makeKeyAndOrderFront(nil)
    NSApp.activate(ignoringOtherApps: true)
    if let path, let o = origin { web.load(URLRequest(url: URL(string: path, relativeTo: o)!)) }
  }

  var isVisible: Bool { window?.isVisible ?? false }

  func reload() { web?.reload() }

  // openInBrowser signs the default browser in with its own one-time link.
  func openInBrowser() {
    let data = Agent.data
    DispatchQueue.global().async {
      let u = try? Owner.onceURL(data: data)
      DispatchQueue.main.async { NSWorkspace.shared.open(u ?? self.origin ?? URL(string: "http://127.0.0.1:7777")!) }
    }
  }

  private func make() {
    let cfg = WKWebViewConfiguration()
    cfg.websiteDataStore = .default() // the session cookie and the page's own storage persist
    // The page runs up to the window's top edge; .mac-app and --mac-titlebar
    // tell it to keep its controls below the traffic lights (app.css).
    cfg.userContentController.addUserScript(
      WKUserScript(
        source: "document.documentElement.classList.add('mac-app');" + inset(Window.titlebar),
        injectionTime: .atDocumentStart, forMainFrameOnly: true))
    web = WKWebView(frame: NSRect(x: 0, y: 0, width: 1200, height: 800), configuration: cfg)
    web.navigationDelegate = self
    web.uiDelegate = self
    web.allowsBackForwardNavigationGestures = true
    let w = NSWindow(
      contentRect: web.frame, styleMask: [.titled, .closable, .miniaturizable, .resizable, .fullSizeContentView],
      backing: .buffered, defer: false)
    w.title = "Fyr"
    // The title bar is transparent over the page, and the window takes the
    // page's colour (its theme-color, else its background) while it loads.
    // The title bar keeps its mouse events: its strip drags the window.
    w.titlebarAppearsTransparent = true
    w.titleVisibility = .hidden
    paints = [
      web.observe(\.themeColor) { [weak self] _, _ in self?.paint() },
      web.observe(\.underPageBackgroundColor) { [weak self] _, _ in self?.paint() },
    ]
    w.contentView = web
    w.delegate = self
    w.isReleasedWhenClosed = false
    w.setFrameAutosaveName("Fyr") // the size and place are restored
    if !w.setFrameUsingName("Fyr") { w.center() }
    window = w
    paint()
    if let o = origin { web.load(URLRequest(url: o)) } else { signIn() }
  }

  // The title bar's height, which the page keeps free.
  static let titlebar = NSWindow.frameRect(forContentRect: NSRect(x: 0, y: 0, width: 100, height: 100), styleMask: [.titled]).height - 100

  private func inset(_ h: CGFloat) -> String {
    "document.documentElement.style.setProperty('--mac-titlebar','\(Int(h))px');"
  }

  private func paint() { window?.backgroundColor = web.themeColor ?? web.underPageBackgroundColor }

  // signIn waits up to 30 s for fyrd (it may be starting) and loads a
  // one-time link from the owner socket.
  private func signIn() {
    guard !signingIn else { return }
    signingIn = true
    web.loadHTMLString(page("Starting Fyr…", ""), baseURL: nil)
    let data = Agent.data
    DispatchQueue.global().async {
      var last: Error?
      for _ in 0..<30 {
        do {
          let u = try Owner.onceURL(data: data)
          DispatchQueue.main.async {
            self.signingIn = false
            self.origin = URL(string: "/", relativeTo: u)?.absoluteURL
            self.web.load(URLRequest(url: u))
          }
          return
        } catch { last = error }
        sleep(1)
      }
      DispatchQueue.main.async {
        self.signingIn = false
        let why = (last.map { "\($0)" } ?? "").replacingOccurrences(of: "<", with: "&lt;")
        self.web.loadHTMLString(
          self.page("Fyr does not answer", "<p>\(why)</p><p>The log is in ~/Library/Logs/fyr.log.</p><p><a href=\"fyr-shell:retry\">Try again</a></p>"),
          baseURL: nil)
      }
    }
  }

  private func page(_ title: String, _ body: String) -> String {
    """
    <!doctype html><meta charset="utf-8"><style>body{font:15px -apple-system,sans-serif;margin:20vh auto;max-width:34em;padding:0 16px;color:#222;background:#fff}
    @media (prefers-color-scheme:dark){body{color:#ddd;background:#1c1c1e}a{color:#6cf}}</style><h2>\(title)</h2>\(body)
    """
  }

  private func isFyr(_ u: URL) -> Bool {
    guard let h = u.host else { return false }
    if let o = origin?.host, h == o { return true }
    return h == "127.0.0.1" || h == "localhost" || h.hasSuffix(".localhost")
  }

  // MARK: WKNavigationDelegate

  func webView(
    _ w: WKWebView, decidePolicyFor a: WKNavigationAction, decisionHandler: @escaping (WKNavigationActionPolicy) -> Void
  ) {
    guard let u = a.request.url else { return decisionHandler(.cancel) }
    if u.scheme == "fyr-shell" {
      decisionHandler(.cancel)
      if u.absoluteString == "fyr-shell:retry" { signIn() }
      return
    }
    if ["about", "data", "blob"].contains(u.scheme ?? "") || isFyr(u) || a.targetFrame?.isMainFrame == false {
      return decisionHandler(.allow)
    }
    decisionHandler(.cancel)
    NSWorkspace.shared.open(u) // another site: the default browser
  }

  func webView(
    _ w: WKWebView, decidePolicyFor r: WKNavigationResponse, decisionHandler: @escaping (WKNavigationResponsePolicy) -> Void
  ) {
    if r.isForMainFrame, (r.response as? HTTPURLResponse)?.statusCode == 401 {
      decisionHandler(.cancel)
      signIn()
      return
    }
    decisionHandler(.allow)
  }

  func webView(_ w: WKWebView, didFailProvisionalNavigation n: WKNavigation!, withError e: Error) {
    if (e as NSError).code == NSURLErrorCancelled { return }
    signIn() // fyrd is down or moved to another port: the owner socket knows
  }

  func webViewWebContentProcessDidTerminate(_ w: WKWebView) { w.reload() }

  // MARK: WKUIDelegate

  // target=_blank and window.open: the default browser.
  func webView(
    _ w: WKWebView, createWebViewWith c: WKWebViewConfiguration, for a: WKNavigationAction, windowFeatures: WKWindowFeatures
  ) -> WKWebView? {
    if let u = a.request.url { NSWorkspace.shared.open(u) }
    return nil
  }

  func webView(
    _ w: WKWebView, runOpenPanelWith p: WKOpenPanelParameters, initiatedByFrame f: WKFrameInfo,
    completionHandler: @escaping ([URL]?) -> Void
  ) {
    let panel = NSOpenPanel()
    panel.allowsMultipleSelection = p.allowsMultipleSelection
    panel.canChooseDirectories = p.allowsDirectories
    panel.beginSheetModal(for: window!) { completionHandler($0 == .OK ? panel.urls : nil) }
  }

  func webView(
    _ w: WKWebView, runJavaScriptAlertPanelWithMessage m: String, initiatedByFrame f: WKFrameInfo,
    completionHandler: @escaping () -> Void
  ) {
    let a = NSAlert()
    a.messageText = m
    a.beginSheetModal(for: window!) { _ in completionHandler() }
  }

  func webView(
    _ w: WKWebView, runJavaScriptConfirmPanelWithMessage m: String, initiatedByFrame f: WKFrameInfo,
    completionHandler: @escaping (Bool) -> Void
  ) {
    let a = NSAlert()
    a.messageText = m
    a.addButton(withTitle: "OK")
    a.addButton(withTitle: "Cancel")
    a.beginSheetModal(for: window!) { completionHandler($0 == .alertFirstButtonReturn) }
  }

  func webView(
    _ w: WKWebView, runJavaScriptTextInputPanelWithPrompt p: String, defaultText d: String?, initiatedByFrame f: WKFrameInfo,
    completionHandler: @escaping (String?) -> Void
  ) {
    let a = NSAlert()
    a.messageText = p
    let field = NSTextField(frame: NSRect(x: 0, y: 0, width: 280, height: 24))
    field.stringValue = d ?? ""
    a.accessoryView = field
    a.addButton(withTitle: "OK")
    a.addButton(withTitle: "Cancel")
    a.beginSheetModal(for: window!) { completionHandler($0 == .alertFirstButtonReturn ? field.stringValue : nil) }
  }

  // MARK: NSWindowDelegate

  // Full screen hides the title bar, and the page takes the strip back.
  func windowDidEnterFullScreen(_ n: Notification) { web.evaluateJavaScript(inset(0)) }
  func windowDidExitFullScreen(_ n: Notification) { web.evaluateJavaScript(inset(Window.titlebar)) }

  // Closing the window keeps the app in the menu bar, without a Dock icon.
  func windowWillClose(_ n: Notification) { NSApp.setActivationPolicy(.accessory) }
}
