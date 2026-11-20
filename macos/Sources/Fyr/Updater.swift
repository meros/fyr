import AppKit

// Updater keeps Fyr.app itself current (decision 0037, V0.mac-selfupdate),
// as fyrd keeps itself current (decision 0027). Every 6 hours it reads the
// repo's latest release, which CI's mac.yml makes `app-<sha12>`, and when
// that is not this build it downloads Fyr.app.zip, accepts it only when
// macOS checks it as signed with the certificate this build pins
// (FyrSigner, written by build.sh), swaps it in place of this bundle and
// starts it. fyrd and its agents go on running: a running program keeps its
// old files, and launchd starts the new fyr-host at the agent's next start.
// An ad hoc build pins no certificate and never updates.
final class Updater {
  static let info = Bundle.main.infoDictionary ?? [:]
  let repo = info["FyrRepo"] as? String ?? "meros/fyr"
  let signer = info["FyrSigner"] as? String ?? ""
  let version = info["CFBundleVersion"] as? String ?? ""
  let bundleID = Bundle.main.bundleIdentifier ?? ""

  // relaunch hands over to the new build: the app quits and opens it.
  var relaunch: (URL) -> Void = { _ in }
  private var timer: Timer?
  private var busy = false

  var enabled: Bool { signer.count == 40 && !version.isEmpty }

  func start() {
    guard enabled else { return NSLog("Fyr: self-update off (this build pins no certificate)") }
    DispatchQueue.main.asyncAfter(deadline: .now() + 60) { self.check() }
    timer = Timer.scheduledTimer(withTimeInterval: 6 * 3600, repeats: true) { _ in self.check() }
  }

  // check runs one update round in the background; a failure waits for the
  // next round.
  func check(_ done: ((String) -> Void)? = nil) {
    guard enabled, !busy else { done?(enabled ? "An update is already running." : "This build does not update itself."); return }
    busy = true
    DispatchQueue.global().async {
      var said = "Fyr.app is up to date."
      do {
        if let new = try self.update() {
          said = "Updated to \(new)."
        }
      } catch {
        said = "Update failed: \(error)"
        NSLog("Fyr: self-update: \(error)")
      }
      DispatchQueue.main.async {
        self.busy = false
        done?(said)
      }
    }
  }

  struct Failure: Error, CustomStringConvertible { let description: String }

  // update returns the new version when it swapped one in.
  private func update() throws -> String? {
    let latest = try json(URL(string: "https://api.github.com/repos/\(repo)/releases/latest")!)
    guard let tag = latest["tag_name"] as? String, tag.hasPrefix("app-") else { return nil }
    let v = String(tag.dropFirst(4))
    if v == version { return nil }
    guard let assets = latest["assets"] as? [[String: Any]],
      let zip = assets.first(where: { $0["name"] as? String == "Fyr.app.zip" })?["browser_download_url"] as? String,
      let zipURL = URL(string: zip)
    else { throw Failure(description: "\(tag) has no Fyr.app.zip") }

    // Unpack beside the bundle, on its volume, so the swap is a rename.
    let fm = FileManager.default
    let here = Bundle.main.bundleURL
    let work = try fm.url(for: .itemReplacementDirectory, in: .userDomainMask, appropriateFor: here, create: true)
    defer { try? fm.removeItem(at: work) }
    let file = work.appendingPathComponent("Fyr.app.zip")
    try Data(contentsOf: zipURL).write(to: file)
    try run("/usr/bin/ditto", ["-x", "-k", file.path, work.path])
    let new = work.appendingPathComponent("Fyr.app")

    // The one check that matters: macOS verifies the whole bundle against
    // this build's certificate and bundle id.
    let req = "=identifier \"\(bundleID)\" and certificate root = H\"\(signer)\""
    try run("/usr/bin/codesign", ["--verify", "--strict", "--deep", "-R", req, new.path])
    guard Bundle(url: new)?.infoDictionary?["CFBundleVersion"] as? String == v else {
      throw Failure(description: "\(tag)'s app says another version")
    }
    _ = try fm.replaceItemAt(here, withItemAt: new)
    NSLog("Fyr: self-update: \(version) → \(v)")
    DispatchQueue.main.async { self.relaunch(here) }
    return v
  }

  private func json(_ u: URL) throws -> [String: Any] {
    var r = URLRequest(url: u, timeoutInterval: 30)
    r.setValue("application/vnd.github+json", forHTTPHeaderField: "Accept")
    var out: Result<Data, Error> = .failure(Failure(description: "no answer from \(u.host ?? "")"))
    let sem = DispatchSemaphore(value: 0)
    URLSession.shared.dataTask(with: r) { d, resp, e in
      if let e { out = .failure(e) } else if (resp as? HTTPURLResponse)?.statusCode != 200 {
        out = .failure(Failure(description: "\(u.path): HTTP \((resp as? HTTPURLResponse)?.statusCode ?? 0)"))
      } else if let d { out = .success(d) }
      sem.signal()
    }.resume()
    sem.wait()
    guard let j = try JSONSerialization.jsonObject(with: out.get()) as? [String: Any] else { throw Failure(description: "bad JSON") }
    return j
  }

  private func run(_ tool: String, _ args: [String]) throws {
    let p = Process()
    p.executableURL = URL(fileURLWithPath: tool)
    p.arguments = args
    let err = Pipe()
    p.standardError = err
    p.standardOutput = FileHandle.nullDevice
    try p.run()
    p.waitUntilExit()
    guard p.terminationStatus == 0 else {
      let why = String(decoding: err.fileHandleForReading.readDataToEndOfFile(), as: UTF8.self).trimmingCharacters(in: .whitespacesAndNewlines)
      throw Failure(description: "\((tool as NSString).lastPathComponent): \(why)")
    }
  }
}
