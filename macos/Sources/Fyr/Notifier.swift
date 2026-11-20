import AppKit
import Foundation
import UserNotifications

// Notifier shows fyrd's pushes as macOS notifications (decision 0037,
// V0.mac-notify). WKWebView has no Web Push, so the app is a push device of
// its own: it stores itself on the owner socket (POST /push/native) and
// reads its pushes there (GET /push/native/stream), one JSON line each.
// fyrd decides what to push, as for every device: presence, quiet hours,
// and this device's own settings.
final class Notifier: NSObject, UNUserNotificationCenterDelegate {
  // open shows the window on a path (a click on a notification).
  var open: (String) -> Void = { _ in }

  private var endpoint: String? {
    get { UserDefaults.standard.string(forKey: "pushEndpoint") }
    set { UserDefaults.standard.set(newValue, forKey: "pushEndpoint") }
  }

  // The owner turned this device's notifications off in Settings; the app
  // then neither reads nor stores itself again until they turn them on.
  private var off: Bool {
    get { UserDefaults.standard.bool(forKey: "pushOff") }
    set { UserDefaults.standard.set(newValue, forKey: "pushOff") }
  }
  private let wake = DispatchSemaphore(value: 0)

  func start() {
    let c = UNUserNotificationCenter.current()
    c.delegate = self
    c.requestAuthorization(options: [.alert, .sound, .badge]) { ok, err in
      if !ok { NSLog("Fyr: notifications not allowed: \(err.map { "\($0)" } ?? "the owner said no")") }
    }
    Thread.detachNewThread { self.run() }
  }

  // run keeps the stream open: it stores the device, reads, and starts
  // again after a pause when fyrd restarts or the link drops.
  private func run() {
    var pause: UInt32 = 1
    while true {
      if off {
        wake.wait()
        continue
      }
      do {
        let ep = try register()
        pause = 1
        try read(ep)
      } catch {
        NSLog("Fyr: notifications: \(error)")
      }
      sleep(pause)
      pause = min(pause * 2, 30)
    }
  }

  // The page's Settings asks through the window's fyrPush bridge
  // (pwa.ts): status, on and off, each answered with { on, endpoint?,
  // denied?, error? }.
  func status(_ reply: @escaping ([String: Any]) -> Void) {
    UNUserNotificationCenter.current().getNotificationSettings { st in
      var r: [String: Any] = ["on": !self.off && self.endpoint != nil, "denied": st.authorizationStatus == .denied]
      if !self.off, let ep = self.endpoint { r["endpoint"] = ep }
      reply(r)
    }
  }

  func turnOn(_ reply: @escaping ([String: Any]) -> Void) {
    let center = UNUserNotificationCenter.current()
    center.requestAuthorization(options: [.alert, .sound, .badge]) { ok, err in
      guard ok else {
        center.getNotificationSettings { st in
          if st.authorizationStatus == .denied {
            reply(["on": false, "denied": true])
          } else if let err {
            reply(["on": false, "error": err.localizedDescription])
          } else {
            reply(["on": false, "denied": true])
          }
        }
        return
      }
      DispatchQueue.global().async {
        self.off = false
        do {
          let ep = try self.register()
          self.wake.signal()
          reply(["on": true, "endpoint": ep])
        } catch {
          reply(["on": false, "error": "\(error)"])
        }
      }
    }
  }

  // openSettings opens macOS System Settings → Notifications so the owner
  // can allow Fyr when macOS has blocked notifications for the app.
  func openSettings() {
    DispatchQueue.main.async {
      if let u = URL(string: "x-apple.systempreferences:com.apple.Notifications-Settings.extension") {
        NSWorkspace.shared.open(u)
      }
    }
  }

  // turnOff stops reading; the page then removes the device, which ends
  // the stream, and the loop waits for turnOn.
  func turnOff(_ reply: @escaping ([String: Any]) -> Void) {
    off = true
    var r: [String: Any] = ["on": false]
    if let ep = endpoint { r["endpoint"] = ep }
    reply(r)
  }

  private func register() throws -> String {
    let host = Host.current().localizedName ?? "this Mac"
    let body = try JSONSerialization.data(withJSONObject: ["endpoint": endpoint ?? "", "device": "Fyr.app on \(host)"])
    let reply = try Owner.request(socket: Owner.socket(data: Agent.data), method: "POST", route: "/push/native", body: body)
    guard let sep = reply.range(of: Data("\r\n\r\n".utf8)),
      let j = try? JSONSerialization.jsonObject(with: reply[sep.upperBound...]) as? [String: Any],
      let ep = j["endpoint"] as? String
    else { throw Owner.Failure(description: "no device from /push/native") }
    endpoint = ep
    return ep
  }

  // read follows the stream until it ends; each line is a push.Payload, an
  // empty line the 30 s sign of life (90 s without one ends the read).
  private func read(_ ep: String) throws {
    let q = ep.addingPercentEncoding(withAllowedCharacters: .urlQueryAllowed) ?? ep
    try Owner.stream(socket: Owner.socket(data: Agent.data), route: "/push/native/stream?endpoint=\(q)", idle: 90) { line in
      guard !line.isEmpty, let j = try? JSONSerialization.jsonObject(with: Data(line.utf8)) as? [String: Any] else { return }
      self.show(j)
    }
  }

  // show posts one push as the service worker would (push.ts
  // notificationOptions): one notification per thread, which a newer push
  // replaces; a question stays until handled.
  private func show(_ p: [String: Any]) {
    let c = UNMutableNotificationContent()
    c.title = p["title"] as? String ?? "Fyr"
    c.body = p["body"] as? String ?? ""
    c.sound = .default
    let thread = p["threadId"] as? String ?? ""
    let host = p["host"] as? String ?? ""
    let state = p["state"] as? String ?? ""
    let ref = thread.isEmpty ? "" : (host.isEmpty ? thread : "\(host)/\(thread)")
    c.userInfo = ["ref": ref, "threadId": thread, "state": state]
    if !ref.isEmpty { c.threadIdentifier = ref }
    if state == "asking" { c.interruptionLevel = .timeSensitive }
    let id = ref.isEmpty ? UUID().uuidString : ref
    UNUserNotificationCenter.current().add(UNNotificationRequest(identifier: id, content: c, trigger: nil))
  }

  // clear removes delivered notifications for threadId once the thread left
  // the state they were shown for (pwa.ts clearThreadNotification, push.ts
  // notificationIsStale).
  func clear(threadId: String, state: String) {
    guard !threadId.isEmpty else { return }
    let center = UNUserNotificationCenter.current()
    center.getDeliveredNotifications { list in
      let ids = list.compactMap { n -> String? in
        let info = n.request.content.userInfo
        let tid = info["threadId"] as? String ?? ""
        let ref = info["ref"] as? String ?? n.request.identifier
        let matches = tid == threadId || ref == threadId || ref.hasSuffix("/\(threadId)")
        guard matches else { return nil }
        let shown = info["state"] as? String
        return Self.isStale(shown: shown, current: state) ? n.request.identifier : nil
      }
      if !ids.isEmpty { center.removeDeliveredNotifications(withIdentifiers: ids) }
    }
  }

  static func isStale(shown: String?, current state: String) -> Bool {
    guard let shown, !shown.isEmpty else { return state != "asking" && state != "failed" }
    return shown != state
  }

  // A click opens the thread in the window.
  func userNotificationCenter(
    _ c: UNUserNotificationCenter, didReceive r: UNNotificationResponse, withCompletionHandler done: @escaping () -> Void
  ) {
    let ref = r.notification.request.content.userInfo["ref"] as? String ?? ""
    let path = ref.isEmpty ? "/" : "/?open=" + (ref.addingPercentEncoding(withAllowedCharacters: .alphanumerics) ?? ref)
    DispatchQueue.main.async { self.open(path) }
    done()
  }

  // Shown while the app is in front too: fyrd already leaves out the thread
  // the owner is looking at (presence).
  func userNotificationCenter(
    _ c: UNUserNotificationCenter, willPresent n: UNNotification,
    withCompletionHandler done: @escaping (UNNotificationPresentationOptions) -> Void
  ) {
    done([.banner, .list, .sound])
  }
}
