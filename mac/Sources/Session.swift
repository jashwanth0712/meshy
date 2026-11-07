// Sign-In with Ethereum (EIP-4361) for the menu bar, and the session in the Keychain.
//
//   1. beginSignIn() makes a random nonce, remembers it (15 min, single use) and returns
//      <dashboard>/connect?nonce=…&callback=meshy://auth for the default browser.
//   2. The page has MetaMask personal_sign a SIWE message with that nonce, the dashboard's
//      domain, chain 11155111 and a 10-minute expiry, then opens meshy://auth?message=…&sig=….
//   3. completeSignIn() runs the bundled `meshy verify-siwe`, which recovers the signer with
//      secp256k1 and checks the nonce, domain, chain and expiry. Only then is the address trusted.
//   4. The session {address, message, sig} is stored in the login Keychain and survives relaunch
//      until Sign Out.
import Foundation

struct Session: Codable {
    let address: String
    let message: String
    let sig: String
    let signedAt: Double
    let domain: String
}

struct SignInError: Error { let message: String }

enum Keychain {
    // Stored with /usr/bin/security, so the item's access list names `security` rather than this
    // ad-hoc signed app: updating Meshy doesn't trigger "allow access" prompts.
    static let service = isTest ? "live.meshy.mac.session.test" : "live.meshy.mac.session"
    static var account: String { ProcessInfo.processInfo.environment["MESHY_TEST_KEYCHAIN_ACCOUNT"] ?? "default" }

    static func read() -> Session? {
        guard let out = run("/usr/bin/security", ["find-generic-password", "-s", service, "-a", account, "-w"]), out.status == 0,
              let d = out.stdout.trimmingCharacters(in: .whitespacesAndNewlines).data(using: .utf8) else { return nil }
        return try? JSONDecoder().decode(Session.self, from: d)
    }

    @discardableResult
    static func write(_ s: Session) -> Bool {
        guard let d = try? JSONEncoder().encode(s), let json = String(data: d, encoding: .utf8) else { return false }
        return run("/usr/bin/security", ["add-generic-password", "-U", "-s", service, "-a", account, "-l", "Meshy sign-in", "-w", json])?.status == 0
    }

    static func delete() {
        _ = run("/usr/bin/security", ["delete-generic-password", "-s", service, "-a", account])
    }
}

/// Runs a program and waits (short-lived tools only).
@discardableResult
func run(_ path: String, _ args: [String], stdin: String? = nil, env: [String: String] = [:], timeout: TimeInterval = 60) -> (status: Int32, stdout: String, stderr: String)? {
    let p = Process()
    p.executableURL = URL(fileURLWithPath: path)
    p.arguments = args
    var e = ProcessInfo.processInfo.environment
    for (k, v) in env { e[k] = v }
    p.environment = e
    let out = Pipe(), err = Pipe(), inp = Pipe()
    p.standardOutput = out
    p.standardError = err
    p.standardInput = inp
    do { try p.run() } catch { return nil }
    if let stdin { inp.fileHandleForWriting.write(stdin.data(using: .utf8) ?? Data()) }
    try? inp.fileHandleForWriting.close()
    // Read before waiting so a chatty tool can't fill the pipe and block.
    var o = Data(), x = Data()
    let g = DispatchGroup()
    g.enter(); DispatchQueue.global().async { o = out.fileHandleForReading.readDataToEndOfFile(); g.leave() }
    g.enter(); DispatchQueue.global().async { x = err.fileHandleForReading.readDataToEndOfFile(); g.leave() }
    if g.wait(timeout: .now() + timeout) == .timedOut { p.terminate() }
    p.waitUntilExit()
    return (p.terminationStatus, String(data: o, encoding: .utf8) ?? "", String(data: x, encoding: .utf8) ?? "")
}

extension MeshyStore {
    private static let nonceTTL: Double = 15 * 60

    private var nonces: [String: Double] {
        get { defaults.dictionary(forKey: "signin.nonces") as? [String: Double] ?? [:] }
        set { defaults.set(newValue, forKey: "signin.nonces") }
    }

    func beginSignIn() -> URL? {
        var bytes = [UInt8](repeating: 0, count: 16)
        guard SecRandomCopyBytes(kSecRandomDefault, bytes.count, &bytes) == errSecSuccess else { return nil }
        let nonce = bytes.map { String(format: "%02x", $0) }.joined()
        let now = Date().timeIntervalSince1970
        var n = nonces.filter { now - $0.value < Self.nonceTTL } // a few tries may be outstanding
        n[nonce] = now
        nonces = n
        var c = URLComponents(string: Settings.base + "/connect")
        c?.queryItems = [URLQueryItem(name: "nonce", value: nonce), URLQueryItem(name: "callback", value: "meshy://auth")]
        return c?.url
    }

    /// Verifies a meshy://auth callback. Accepted only for a nonce this Mac issued (once, within
    /// 15 minutes) and a signature that recovers to the address in the message.
    func completeSignIn(_ url: URL) -> Result<String, SignInError> {
        let items = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems ?? []
        func v(_ k: String) -> String? { items.first { $0.name == k }?.value }
        guard let message = v("message"), let sig = v("sig") else {
            return .failure(SignInError(message: "The link from the browser was incomplete."))
        }
        guard let nonce = message.components(separatedBy: "\n").first(where: { $0.hasPrefix("Nonce: ") })?.dropFirst(7).description,
              let at = nonces[nonce] else {
            return .failure(SignInError(message: "That sign-in wasn't started from this Mac, or was already used."))
        }
        guard Date().timeIntervalSince1970 - at < Self.nonceTTL else {
            nonces[nonce] = nil
            return .failure(SignInError(message: "That sign-in link expired."))
        }
        guard let bin = Bins.path("meshy") else { return .failure(SignInError(message: "The meshy tool is missing from Meshy.app. Reinstall Meshy.")) }
        let domain = Settings.host
        guard let r = run(bin, ["verify-siwe", "--sig", sig, "--nonce", nonce, "--domain", domain, "--chain", String(Settings.chainID)], stdin: message, timeout: 15) else {
            return .failure(SignInError(message: "Couldn't run the signature check."))
        }
        guard r.status == 0, let d = r.stdout.data(using: .utf8), let j = try? JSONSerialization.jsonObject(with: d) as? [String: Any],
              let addr = j["address"] as? String else {
            let why = r.stdout.replacingOccurrences(of: "MESHY_SIWE_INVALID ", with: "").trimmingCharacters(in: .whitespacesAndNewlines)
            return .failure(SignInError(message: "The signature didn't check out: \(why.isEmpty ? r.stderr : why)."))
        }
        nonces[nonce] = nil // single use
        let s = Session(address: addr, message: message, sig: sig, signedAt: Date().timeIntervalSince1970, domain: domain)
        guard Keychain.write(s) else { return .failure(SignInError(message: "Couldn't save the session in the Keychain.")) }
        setSession(s)
        return .success(addr)
    }
}
