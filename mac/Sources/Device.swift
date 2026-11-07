// This Mac as a Meshy device or agent, using the binaries bundled in Meshy.app/Contents/Resources/bin.
//
// Share this Mac…   meshyd init (keys + join code) → dashboard Add device, prefilled → once ENS
//                   shows the device, two LaunchAgents: `meshyd run` and `meshyd mesh-join`.
// Join with invite  meshy join <link> (approval in the browser) → meshy mesh-join.
// CLI tools         symlinks in ~/.local/bin.
import AppKit
import Foundation

enum Bins {
    /// Meshy.app/Contents/Resources/bin, or $MESHY_BIN_DIR for development.
    static var dir: String? {
        if let d = ProcessInfo.processInfo.environment["MESHY_BIN_DIR"] { return d }
        return Bundle.main.resourceURL?.appendingPathComponent("bin").path
    }
    static func path(_ tool: String) -> String? {
        guard let d = dir else { return nil }
        let p = (d as NSString).appendingPathComponent(tool)
        return FileManager.default.isExecutableFile(atPath: p) ? p : nil
    }
    static var home: String { FileManager.default.homeDirectoryForCurrentUser.path }
    static var meshyHome: String { ProcessInfo.processInfo.environment["MESHY_HOME"] ?? home + "/.meshy" }
    /// meshyd's keys and mesh state for this Mac.
    static var stateDir: String { ProcessInfo.processInfo.environment["MESHY_STATE"] ?? meshyHome + "/meshyd" }
    static var controlSocket: String { stateDir + "/control.sock" }
    static let tools = ["meshy", "meshyd", "meshy-mesh"]
    /// Where Install Command-Line Tools puts symlinks.
    static var toolsDir: String { ProcessInfo.processInfo.environment["MESHY_TOOLS_DIR"] ?? home + "/.local/bin" }
}

/// The device this Mac shares (one per Mac).
struct SharedDevice: Codable {
    let name: String // db2.prod.acme.eth
    let org: String
    var roots: [String]
    let addr: String
    let joinCode: String
    var running: Bool
    let createdAt: Double
    /// Set by Stop Sharing: the device stays registered but this Mac doesn't serve it until
    /// Start Sharing. (Without it, the next status check would start it again by itself.)
    var paused: Bool?
    /// Where this device's keys live, when not ~/.meshy/meshyd (a device made from the terminal).
    var state: String?
    var stateDir: String { state ?? Bins.stateDir }
    var controlSocket: String { stateDir + "/control.sock" }
}

/// Device keys on this Mac (`meshyd whoami`).
struct LocalKeys: Codable {
    let state: String
    let addr: String
}

/// Live status from meshyd's control socket.
struct DaemonStatus {
    var reachable = false
    var healthy = false
    var head: Int?
    var sessions = 0
    var meshRunning = false
    var meshJoined = false
    /// The provider's last refusal from the mesh log, e.g. MESHY_SEATS_EXCEEDED … (nil once on the mesh).
    var meshError: String?
}

final class DeviceManager {
    static let shared = DeviceManager()
    let defaults = appDefaults
    var onChange: (() -> Void)?

    private(set) var status = DaemonStatus()
    private(set) var joinState: String? // "Waiting for approval of claude-1.agents.acme.eth…"
    private var joinProc: Process?
    private var timer: Timer?
    private var starting = false

    var device: SharedDevice? {
        get { defaults.data(forKey: "device").flatMap { try? JSONDecoder().decode(SharedDevice.self, from: $0) } }
        set { defaults.set(newValue.flatMap { try? JSONEncoder().encode($0) }, forKey: "device") }
    }

    func start() {
        poll()
        timer = Timer.scheduledTimer(withTimeInterval: 10, repeats: true) { [weak self] _ in self?.poll() }
    }

    // MARK: Share this Mac

    /// Step 1: keys + join code, then the dashboard's Add device sheet, prefilled.
    func share(name: String, org: String, roots: [String]) -> Result<SharedDevice, SignInError> {
        guard let meshyd = Bins.path("meshyd") else { return .failure(SignInError(message: "meshyd is missing from Meshy.app. Reinstall Meshy.")) }
        guard let r = run(meshyd, ["init", "--name", name, "--roots", roots.joined(separator: ","), "--state", Bins.stateDir], timeout: 30) else {
            return .failure(SignInError(message: "Couldn't run meshyd."))
        }
        guard r.status == 0, let line = r.stdout.components(separatedBy: "\n").first(where: { $0.contains("meshy-join:") }) else {
            return .failure(SignInError(message: (r.stderr + r.stdout).trimmingCharacters(in: .whitespacesAndNewlines)))
        }
        let code = line.trimmingCharacters(in: .whitespaces)
        var b64 = String(code.dropFirst("meshy-join:".count)).replacingOccurrences(of: "-", with: "+").replacingOccurrences(of: "_", with: "/")
        while b64.count % 4 != 0 { b64 += "=" }
        let j = Data(base64Encoded: b64).flatMap { try? JSONSerialization.jsonObject(with: $0) as? [String: Any] }
        let d = SharedDevice(name: name, org: org, roots: roots, addr: j?["addr"] as? String ?? "", joinCode: code, running: false, createdAt: Date().timeIntervalSince1970)
        device = d
        keysCache = nil
        onChange?()
        return .success(d)
    }

    var addDeviceLink: String? {
        guard let d = device else { return nil }
        return "/org/\(d.org)/directory?add=\(q(d.joinCode))"
    }

    /// Is the shared device on ENS (in the selected org's map) with our key?
    var registered: Bool {
        guard let d = device, let m = MeshyStore.shared.map, m.org == d.org else { return false }
        return m.devices.contains { $0.fullName == d.name && ($0.addr ?? "").lowercased() == d.addr.lowercased() }
    }

    /// Step 2 (automatic once registered): run the daemon and join the mesh as LaunchAgents.
    func startDaemons() -> String? {
        guard var d = device else { return "Share this Mac first." }
        guard let meshyd = Bins.path("meshyd") else { return "meshyd is missing from Meshy.app." }
        let logs = Bins.home + "/Library/Logs/Meshy"
        try? FileManager.default.createDirectory(atPath: logs, withIntermediateDirectories: true)
        let common = ["--name", d.name, "--state", d.stateDir]
        let jobs: [(String, [String], String)] = [
            ("live.meshy.meshyd", [meshyd, "run"] + common + ["--roots", d.roots.joined(separator: ","), "--sepolia", Settings.sepoliaWS, "--listen", "127.0.0.1:2222", "--control", d.controlSocket], "meshyd"),
            ("live.meshy.mesh", [meshyd, "mesh-join"] + common + ["--provider", MeshyStore.shared.syncURL], "mesh"),
        ]
        for (label, args, log) in jobs {
            if let e = installAgent(label: label, args: args, log: "\(logs)/\(log).log") { return e }
        }
        d.running = true
        d.paused = false
        device = d
        MeshyStore.shared.notify(title: "This Mac is shared", body: "\(d.name) is running and joining the mesh.")
        poll()
        return nil
    }

    /// startDaemons off the main thread (launchctl can take a few seconds).
    func startDaemonsAsync(_ done: @escaping (String?) -> Void = { _ in }) {
        guard !starting else { return }
        starting = true
        DispatchQueue.global().async {
            let e = self.startDaemons()
            DispatchQueue.main.async {
                self.starting = false
                self.onChange?()
                done(e)
            }
        }
    }

    func stopDaemons() {
        for label in ["live.meshy.meshyd", "live.meshy.mesh"] { removeAgent(label) }
        if var d = device { d.running = false; d.paused = true; device = d }
        MeshyStore.shared.notify(title: "Stopped sharing", body: "Nobody can reach this Mac's folders until you Start Sharing again. Live sessions were ended.")
        onChange?()
        poll()
    }

    func forgetDevice() {
        stopDaemons()
        // Don't adopt it again by itself (its keys stay on this Mac); Share this Mac can.
        if let n = device?.name { defaults.set(forgotten + [n], forKey: "forgottenDevices") }
        device = nil
        onChange?()
    }

    private var agentsDir: String { Bins.home + "/Library/LaunchAgents" }
    private var uid: String { String(getuid()) }

    private func installAgent(label: String, args: [String], log: String) -> String? {
        let plist: [String: Any] = [
            "Label": label, "ProgramArguments": args, "RunAtLoad": true, "KeepAlive": true,
            "StandardOutPath": log, "StandardErrorPath": log, "ProcessType": "Background", "ThrottleInterval": 20,
        ]
        if isTest { return nil }
        let path = "\(agentsDir)/\(label).plist"
        try? FileManager.default.createDirectory(atPath: agentsDir, withIntermediateDirectories: true)
        guard let data = try? PropertyListSerialization.data(fromPropertyList: plist, format: .xml, options: 0), (try? data.write(to: URL(fileURLWithPath: path))) != nil else {
            return "Couldn't write \(path)."
        }
        _ = run("/bin/launchctl", ["bootout", "gui/\(uid)/\(label)"], timeout: 15)
        // bootout returns before the old job is fully torn down; bootstrapping
        // straight away then fails ("5: Input/output error") and the service is
        // left unloaded. Wait for it to go, and retry the bootstrap a few times.
        for _ in 0..<20 {
            guard let p = run("/bin/launchctl", ["print", "gui/\(uid)/\(label)"], timeout: 5), p.status == 0 else { break }
            Thread.sleep(forTimeInterval: 0.25)
        }
        var last = ""
        for attempt in 0..<5 {
            if attempt > 0 { Thread.sleep(forTimeInterval: Double(attempt)) }
            guard let r = run("/bin/launchctl", ["bootstrap", "gui/\(uid)", path], timeout: 15) else { continue }
            if r.status == 0 { return nil }
            // Already loaded counts as started.
            if let p = run("/bin/launchctl", ["print", "gui/\(uid)/\(label)"], timeout: 5), p.status == 0 { return nil }
            last = r.stderr.trimmingCharacters(in: .whitespacesAndNewlines)
        }
        return "launchctl couldn't start \(label)" + (last.isEmpty ? "." : ": \(last)")
    }

    private func removeAgent(_ label: String) {
        if isTest { return }
        _ = run("/bin/launchctl", ["bootout", "gui/\(uid)/\(label)"], timeout: 15)
        try? FileManager.default.removeItem(atPath: "\(agentsDir)/\(label).plist")
    }

    // MARK: status

    func poll() {
        let dev = device
        DispatchQueue.global().async { [weak self] in
            guard let self else { return }
            var s = DaemonStatus()
            if let dev, FileManager.default.fileExists(atPath: dev.controlSocket),
               let r = run("/usr/bin/curl", ["-s", "--max-time", "3", "--unix-socket", dev.controlSocket, "http://meshyd/status"], timeout: 5), r.status == 0,
               let d = r.stdout.data(using: .utf8), let j = try? JSONSerialization.jsonObject(with: d) as? [String: Any] {
                s.reachable = true
                s.healthy = j["healthy"] as? Bool ?? false
                s.head = (j["headNumber"] as? NSNumber)?.intValue
                s.sessions = (j["sessions"] as? [Any])?.count ?? 0
            }
            if dev != nil, !isTest, let r = run("/bin/launchctl", ["print", "gui/\(getuid())/live.meshy.mesh"], timeout: 5) {
                s.meshRunning = r.status == 0 && r.stdout.contains("state = running")
            }
            s.meshJoined = FileManager.default.fileExists(atPath: (dev?.stateDir ?? Bins.stateDir) + "/mesh/management")
            if dev == nil { _ = self.localKeys() } // warm the cache off the main thread
            if dev?.running == true, let tail = lastLine(of: Bins.home + "/Library/Logs/Meshy/mesh.log"), tail.contains("refused") || tail.contains("MESHY_") {
                s.meshError = tail.range(of: "MESHY_[A-Z_]+.*", options: .regularExpression).map { String(tail[$0]) } ?? tail
            }
            DispatchQueue.main.async {
                self.status = s
                // A device added from the dashboard (or the terminal) whose keys are on this Mac: adopt it.
                if self.device == nil { self.adoptFromENS() }
                // finish setup by itself once the dashboard transaction landed
                if let d = self.device, !d.running, d.paused != true, self.registered { self.startDaemonsAsync() }
                self.onChange?()
            }
        }
    }

    var statusLine: String? {
        guard let d = device else { return nil }
        let short = d.name.replacingOccurrences(of: ".\(d.org)", with: "")
        if !d.running {
            if d.paused == true { return "\(short) · not sharing" }
            return registered ? "\(short) · starting…" : "\(short) · waiting for an admin to add it"
        }
        if let e = status.meshError, !status.meshRunning || e.contains("SEATS") { return "\(short) · mesh refused: \(e.contains("SEATS") ? "org is out of seats" : e)" }
        if !status.reachable { return "\(short) · daemon starting…" }
        let mesh = status.meshRunning ? "on the mesh" : status.meshJoined ? "mesh reconnecting" : "joining the mesh"
        let health = status.healthy ? "healthy" : "chain not live yet"
        return "\(short) · \(health) · \(mesh)" + (status.sessions > 0 ? " · \(plural(status.sessions, "session"))" : "")
    }

    // MARK: Shared folders

    /// Changes which folders this Mac shares: right away here (meshyd restarts with them), and on ENS
    /// (meshy.roots) once an admin signs, so others see the change. Returns the new list.
    @discardableResult
    func setRoots(_ roots: [String]) -> [String] {
        guard var d = device else { return [] }
        var seen = Set<String>()
        d.roots = roots.map { ($0 as NSString).standardizingPath }.filter { seen.insert($0).inserted }
        device = d
        if d.running { startDaemonsAsync() }
        onChange?()
        return d.roots
    }

    /// The device's folders as published on ENS (what others see), if it differs from this Mac.
    var publishedRoots: [String]? {
        guard let d = device, let m = MeshyStore.shared.map?.devices.first(where: { $0.fullName == d.name }) else { return nil }
        return m.rootList
    }

    // MARK: Adopt a device that is already on ENS

    private var keysCache: [LocalKeys]?

    /// Device keys on this Mac, from the bundled `meshyd whoami` (cached; refreshed after Share).
    func localKeys() -> [LocalKeys] {
        if let k = keysCache { return k }
        guard let meshyd = Bins.path("meshyd"), let r = run(meshyd, ["whoami"], timeout: 10) else { return [] }
        let k = r.stdout.components(separatedBy: "\n").compactMap { $0.data(using: .utf8).flatMap { try? JSONDecoder().decode(LocalKeys.self, from: $0) } }
        keysCache = k
        return k
    }

    /// The device in the selected org's map whose address is one of this Mac's keys, if any.
    func deviceOnENSForThisMac() -> (MapResource, LocalKeys)? {
        guard let m = MeshyStore.shared.map else { return nil }
        for k in localKeys() {
            if let d = m.devices.first(where: { ($0.addr ?? "").lowercased() == k.addr.lowercased() }) { return (d, k) }
        }
        return nil
    }

    private var forgotten: [String] { defaults.stringArray(forKey: "forgottenDevices") ?? [] }

    /// Remembers that device as this Mac's and lets poll() start sharing it. True if adopted.
    /// By itself (poll) it skips devices you forgot; Share this Mac passes explicit: true.
    @discardableResult
    func adoptFromENS(explicit: Bool = false) -> Bool {
        guard device == nil, let org = MeshyStore.shared.map?.org, let (d, k) = deviceOnENSForThisMac() else { return false }
        if forgotten.contains(d.fullName) {
            guard explicit else { return false }
            defaults.set(forgotten.filter { $0 != d.fullName }, forKey: "forgottenDevices")
        }
        device = SharedDevice(name: d.fullName, org: org, roots: d.rootList, addr: k.addr, joinCode: "", running: false,
                              createdAt: Date().timeIntervalSince1970, paused: nil, state: k.state == Bins.stateDir ? nil : k.state)
        UI.log("ADOPTED: \(d.fullName) keys=\(k.state)")
        MeshyStore.shared.notify(title: "This Mac is \(d.name)", body: "It's on ENS with this Mac's keys. Meshy is starting to share it.")
        onChange?()
        return true
    }

    // MARK: Join with an invite link (agent or person)

    var joining: Bool { joinProc != nil }

    func join(link: String, provider: String, done: @escaping (Result<String, SignInError>) -> Void) {
        guard let meshy = Bins.path("meshy") else { return done(.failure(SignInError(message: "meshy is missing from Meshy.app."))) }
        let comps = URLComponents(string: link)
        let org = comps?.queryItems?.first { $0.name == "org" }?.value ?? ""
        let agent = comps?.queryItems?.first { $0.name == "agent" }?.value ?? ""
        let name = "\(agent).agents.\(org)"
        joinState = "Approve \(name) in your browser…"
        onChange?()
        let p = Process()
        p.executableURL = URL(fileURLWithPath: meshy)
        // opens the approval page in the default browser, then waits for ENS (tests: no browser, short wait)
        p.arguments = isTest ? ["join", "--no-open", "--timeout", "3s", link] : ["join", link]
        let out = Pipe()
        p.standardOutput = out
        p.standardError = out
        var log = ""
        out.fileHandleForReading.readabilityHandler = { h in
            if let s = String(data: h.availableData, encoding: .utf8) { DispatchQueue.main.async { log += s } }
        }
        p.terminationHandler = { [weak self] proc in
            DispatchQueue.main.async {
                guard let self else { return }
                out.fileHandleForReading.readabilityHandler = nil
                self.joinProc = nil
                guard proc.terminationStatus == 0 else {
                    self.joinState = nil
                    self.onChange?()
                    let why = log.components(separatedBy: "\n").filter { !$0.isEmpty && !$0.allSatisfy { $0 == "." } }.last ?? "meshy join failed"
                    return done(.failure(SignInError(message: why)))
                }
                self.joinState = "Joining the mesh as \(name)…"
                self.onChange?()
                DispatchQueue.global().async {
                    let r = run(meshy, ["mesh-join", "--provider", provider], timeout: 120)
                    DispatchQueue.main.async {
                        self.joinState = nil
                        MeshyStore.shared.loadLocalAgents()
                        self.onChange?()
                        if r?.status == 0 { done(.success(name)) } else { done(.failure(SignInError(message: "Approved, but joining the mesh failed: \((r?.stderr ?? "").trimmingCharacters(in: .whitespacesAndNewlines))"))) }
                    }
                }
            }
        }
        do { try p.run(); joinProc = p } catch {
            joinState = nil
            done(.failure(SignInError(message: error.localizedDescription)))
        }
    }

    func cancelJoin() {
        joinProc?.terminate()
        joinProc = nil
        joinState = nil
        onChange?()
    }

    // MARK: Command-line tools

    /// Links the bundled tools into ~/.local/bin and puts it on PATH (also done at every launch).
    func installTools() -> String { Tools.ensure() }

    var toolsInstalled: Bool {
        guard let dir = Bins.dir else { return false }
        return (try? FileManager.default.destinationOfSymbolicLink(atPath: Bins.toolsDir + "/meshy")) == "\(dir)/meshy"
    }
}

/// The last non-empty line of a (small) log file.
func lastLine(of path: String) -> String? {
    guard let h = FileHandle(forReadingAtPath: path) else { return nil }
    defer { try? h.close() }
    let size = (try? h.seekToEnd()) ?? 0
    try? h.seek(toOffset: size > 4096 ? size - 4096 : 0)
    let s = String(data: h.readDataToEndOfFile(), encoding: .utf8) ?? ""
    return s.components(separatedBy: "\n").last { !$0.trimmingCharacters(in: .whitespaces).isEmpty }
}
