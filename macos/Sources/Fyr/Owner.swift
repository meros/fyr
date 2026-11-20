import Foundation

// Owner asks fyrd's owner socket for a one-time sign-in link, as `fyr open`
// does (V0.130 (1), decision 0028). The socket is the local channel: whoever
// can open it is the owner.
enum Owner {
  struct Failure: Error, CustomStringConvertible { let description: String }

  // The socket's path, as auth.SocketPath has it for a data folder whose
  // path is short enough (the default one is).
  static func socket(data: String) -> String { (data as NSString).appendingPathComponent("owner.sock") }

  static func onceURL(data: String) throws -> URL {
    let reply = try request(socket: socket(data: data), route: "/session/once")
    guard let sep = reply.range(of: Data("\r\n\r\n".utf8)) else { throw Failure(description: "no answer from Fyr") }
    let head = String(decoding: reply[..<sep.lowerBound], as: UTF8.self)
    let body = reply[sep.upperBound...]
    struct Out: Decodable { let url: String?; let error: String? }
    let out = try? JSONDecoder().decode(Out.self, from: body)
    guard head.hasPrefix("HTTP/1.1 200") || head.hasPrefix("HTTP/1.0 200"), let s = out?.url, let u = URL(string: s) else {
      throw Failure(description: "Fyr refused: \(head.split(separator: "\r\n").first ?? "") \(out?.error ?? "")")
    }
    return u
  }

  // request sends one HTTP/1.0 request over the Unix socket and returns the
  // whole reply (HTTP/1.0: the server closes after it).
  static func request(socket path: String, method: String = "POST", route: String, body: Data = Data()) throws -> Data {
    let fd = try connect(path, timeout: 5)
    defer { close(fd) }
    try send(fd, method: method, route: route, body: body)
    var out = Data()
    var buf = [UInt8](repeating: 0, count: 4096)
    while true {
      let n = read(fd, &buf, buf.count)
      if n <= 0 { break }
      out.append(buf, count: n)
    }
    return out
  }

  // stream GETs route and calls line with each line of the body until the
  // server ends it or idle seconds pass without a byte. A status other
  // than 200 throws.
  static func stream(socket path: String, route: String, idle: Int, line: (String) -> Void) throws {
    let fd = try connect(path, timeout: idle)
    defer { close(fd) }
    try send(fd, method: "GET", route: route, body: Data())
    var pending = Data()
    var head = true
    var buf = [UInt8](repeating: 0, count: 4096)
    while true {
      let n = read(fd, &buf, buf.count)
      if n <= 0 { throw Failure(description: n == 0 ? "the stream ended" : "read: \(errno)") }
      pending.append(buf, count: n)
      if head {
        guard let sep = pending.range(of: Data("\r\n\r\n".utf8)) else { continue }
        let status = String(decoding: pending[..<sep.lowerBound], as: UTF8.self).split(separator: "\r\n").first ?? ""
        guard status.hasPrefix("HTTP/1.1 200") || status.hasPrefix("HTTP/1.0 200") else { throw Failure(description: "\(route): \(status)") }
        pending = Data(pending[sep.upperBound...])
        head = false
      }
      while let nl = pending.firstIndex(of: UInt8(ascii: "\n")) {
        line(String(decoding: pending[pending.startIndex..<nl], as: UTF8.self))
        pending = Data(pending[(nl + 1)...])
      }
    }
  }

  private static func send(_ fd: Int32, method: String, route: String, body: Data) throws {
    var req = Data("\(method) \(route) HTTP/1.0\r\nHost: owner\r\nContent-Type: application/json\r\nContent-Length: \(body.count)\r\n\r\n".utf8)
    req.append(body)
    let n = req.withUnsafeBytes { write(fd, $0.baseAddress, req.count) }
    guard n == req.count else { throw Failure(description: "write: \(errno)") }
  }

  private static func connect(_ path: String, timeout: Int) throws -> Int32 {
    let fd = Darwin.socket(AF_UNIX, SOCK_STREAM, 0)
    guard fd >= 0 else { throw Failure(description: "socket: \(errno)") }
    var tv = timeval(tv_sec: timeout, tv_usec: 0)
    setsockopt(fd, SOL_SOCKET, SO_RCVTIMEO, &tv, socklen_t(MemoryLayout<timeval>.size))
    setsockopt(fd, SOL_SOCKET, SO_SNDTIMEO, &tv, socklen_t(MemoryLayout<timeval>.size))
    var addr = sockaddr_un()
    addr.sun_family = sa_family_t(AF_UNIX)
    let bytes = Array(path.utf8)
    guard bytes.count < MemoryLayout.size(ofValue: addr.sun_path) else {
      close(fd)
      throw Failure(description: "socket path too long: \(path)")
    }
    withUnsafeMutableBytes(of: &addr.sun_path) { p in
      for (i, b) in bytes.enumerated() { p[i] = b }
      p[bytes.count] = 0
    }
    let ok = withUnsafePointer(to: &addr) {
      $0.withMemoryRebound(to: sockaddr.self, capacity: 1) { Darwin.connect(fd, $0, socklen_t(MemoryLayout<sockaddr_un>.size)) }
    }
    guard ok == 0 else {
      close(fd)
      throw Failure(description: "Fyr does not answer on \(path)")
    }
    return fd
  }
}
