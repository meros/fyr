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
    let reply = try post(socket: socket(data: data), route: "/session/once")
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

  // post sends one HTTP/1.0 request over the Unix socket and returns the
  // whole reply (HTTP/1.0: the server closes after it).
  static func post(socket path: String, route: String) throws -> Data {
    let fd = Darwin.socket(AF_UNIX, SOCK_STREAM, 0)
    guard fd >= 0 else { throw Failure(description: "socket: \(errno)") }
    defer { close(fd) }
    var tv = timeval(tv_sec: 5, tv_usec: 0)
    setsockopt(fd, SOL_SOCKET, SO_RCVTIMEO, &tv, socklen_t(MemoryLayout<timeval>.size))
    setsockopt(fd, SOL_SOCKET, SO_SNDTIMEO, &tv, socklen_t(MemoryLayout<timeval>.size))
    var addr = sockaddr_un()
    addr.sun_family = sa_family_t(AF_UNIX)
    let bytes = Array(path.utf8)
    guard bytes.count < MemoryLayout.size(ofValue: addr.sun_path) else { throw Failure(description: "socket path too long: \(path)") }
    withUnsafeMutableBytes(of: &addr.sun_path) { p in
      for (i, b) in bytes.enumerated() { p[i] = b }
      p[bytes.count] = 0
    }
    let ok = withUnsafePointer(to: &addr) {
      $0.withMemoryRebound(to: sockaddr.self, capacity: 1) { connect(fd, $0, socklen_t(MemoryLayout<sockaddr_un>.size)) }
    }
    guard ok == 0 else { throw Failure(description: "Fyr does not answer on \(path)") }
    let req = Array("POST \(route) HTTP/1.0\r\nHost: owner\r\nContent-Type: application/json\r\nContent-Length: 0\r\n\r\n".utf8)
    guard write(fd, req, req.count) == req.count else { throw Failure(description: "write: \(errno)") }
    var out = Data()
    var buf = [UInt8](repeating: 0, count: 4096)
    while true {
      let n = read(fd, &buf, buf.count)
      if n <= 0 { break }
      out.append(buf, count: n)
    }
    return out
  }
}
