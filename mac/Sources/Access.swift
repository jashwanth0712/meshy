// Using what you were granted, without a terminal:
//
//   Open in Finder   `meshy mount <device>` (bundled): the device's granted folders as a network
//                    volume in /Volumes. It ejects itself when the grant is revoked or expires.
//   Link this Mac    `meshy login` (bundled): your wallet links this Mac's keys to your member
//                    name (<you>.people.<org>), once per Mac. Needed before Open in Finder.
//   CLI on PATH      on every launch, `meshy`, `meshyd` and `meshy-mesh` are linked into
//                    ~/.local/bin and that folder is added to your shell's PATH.
import AppKit
import Foundation

/// A member identity on this Mac, made by `meshy login` (~/.meshy/person-<label>.<org>).
struct PersonHere: Codable {
    let agent: String // label: roshan
    let org: String
    let addr: String
    var mesh: String?
    var name: String { "\(agent).people.\(org)" }
}

enum People {
    static func here() -> [PersonHere] {
        let home = URL(fileURLWithPath: Bins.meshyHome)
        let dirs = (try? FileManager.default.contentsOfDirectory(at: home, includingPropertiesForKeys: nil)) ?? []
        return dirs.filter { $0.lastPathComponent.hasPrefix("person-") }.compactMap {
            (try? Data(contentsOf: $0.appendingPathComponent("agent.json"))).flatMap { try? JSONDecoder().decode(PersonHere.self, from: $0) }
        }
    }
    static func inOrg(_ org: String?) -> PersonHere? { here().first { $0.org == org } }
}

/// One Finder volume per device you have a grant on.
struct Mount {
    enum State { case mounting, mounted, failed }
    var state: State
    var volume: String?
    var message: String?
    var proc: Process?
}

final class AccessManager {
    static let shared = AccessManager()
    var onChange: (() -> Void)?
    private(set) var mounts: [String: Mount] = [:] // device ENS name -> mount
    private(set) var linking: String? // org being linked
    private var loginProc: Process?

    // MARK: Open in Finder

    func mount(_ device: String, done: @escaping (String?) -> Void = { _ in }) {
        if let m = mounts[device], m.state != .failed {
            if let v = m.volume { UI.open(URL(fileURLWithPath: v)) }
            return done(nil)
        }
        guard let meshy = Bins.path("meshy") else { return done("meshy is missing from Meshy.app. Reinstall Meshy.") }
        let p = Process()
        p.executableURL = URL(fileURLWithPath: meshy)
        p.arguments = ["mount", device] + (isTest ? ["--no-mount"] : [])
        var env = ProcessInfo.processInfo.environment
        env["MESHY_PROVIDER"] = MeshyStore.shared.syncURL
        env["MESHY_NO_SPLASH"] = "1"
        p.environment = env
        let out = Pipe()
        p.standardOutput = out
        p.standardError = out
        var last = ""
        var answered = false
        let answer: (String?) -> Void = { e in if !answered { answered = true; done(e) } }
        out.fileHandleForReading.readabilityHandler = { [weak self] h in
            guard let s = String(data: h.availableData, encoding: .utf8), !s.isEmpty else { return }
            DispatchQueue.main.async {
                guard let self else { return }
                for line in s.components(separatedBy: "\n") where line.hasPrefix("MESHY_") {
                    last = line
                    if line.hasPrefix("MESHY_MOUNTED") {
                        let vol = line.components(separatedBy: " ").first { $0.hasPrefix("volume=") }.map { String($0.dropFirst(7)) }
                        self.mounts[device] = Mount(state: .mounted, volume: vol?.isEmpty == false ? vol : nil, message: nil, proc: p)
                        self.onChange?()
                        answer(nil)
                    }
                }
            }
        }
        p.terminationHandler = { [weak self] proc in
            DispatchQueue.main.async {
                guard let self else { return }
                out.fileHandleForReading.readabilityHandler = nil
                let wasMounted = self.mounts[device]?.state == .mounted
                let why = friendly(last, device: device)
                if wasMounted || proc.terminationStatus == 0 {
                    self.mounts[device] = nil
                    if wasMounted, proc.terminationStatus != 0 {
                        MeshyStore.shared.notify(title: "Ejected \(device.components(separatedBy: ".").first ?? device)", body: why)
                    }
                } else {
                    self.mounts[device] = Mount(state: .failed, volume: nil, message: why, proc: nil)
                }
                self.onChange?()
                answer(wasMounted ? nil : why)
            }
        }
        mounts[device] = Mount(state: .mounting, volume: nil, message: nil, proc: p)
        onChange?()
        do { try p.run() } catch {
            mounts[device] = nil
            onChange?()
            answer(error.localizedDescription)
        }
    }

    func eject(_ device: String) {
        guard let m = mounts[device] else { return }
        m.proc?.interrupt() // meshy mount ejects the volume on SIGINT
        if m.proc == nil { mounts[device] = nil; onChange?() }
    }

    func ejectAll() { for d in mounts.keys { eject(d) } }

    // MARK: Link this Mac (meshy login)

    /// Opens the dashboard's sign-in page; your wallet links this Mac to your member name.
    func linkThisMac(org: String, done: @escaping (Result<String, SignInError>) -> Void) {
        guard linking == nil else { return }
        guard let meshy = Bins.path("meshy") else { return done(.failure(SignInError(message: "meshy is missing from Meshy.app."))) }
        let p = Process()
        p.executableURL = URL(fileURLWithPath: meshy)
        p.arguments = ["login", "--dashboard", Settings.base] + (isTest ? ["--no-open", "--timeout", "3s"] : [])
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
                self.linking = nil
                self.loginProc = nil
                self.onChange?()
                let names = log.components(separatedBy: "\n").filter { $0.hasPrefix("MESHY_LOGGED_IN ") }.map { String($0.dropFirst(16)) }
                if proc.terminationStatus == 0, let n = names.first(where: { $0.hasSuffix(".\(org)") }) ?? names.first {
                    done(.success(n))
                } else {
                    let why = log.components(separatedBy: "\n").last { $0.hasPrefix("MESHY_") || $0.contains("✗") } ?? "Linking didn't finish."
                    done(.failure(SignInError(message: why.trimmingCharacters(in: .whitespaces))))
                }
            }
        }
        linking = org
        onChange?()
        do { try p.run(); loginProc = p } catch {
            linking = nil
            done(.failure(SignInError(message: error.localizedDescription)))
        }
    }

    func cancelLink() { loginProc?.terminate() }

    // MARK: Requests to the admins (no links)

    /// `meshy request …` signed with this Mac's identity in the org. Fails (so the caller can fall back to
    /// a link) when this Mac has no identity in the org or the provider doesn't take requests yet.
    func sendRequest(_ args: [String], done: @escaping (Result<String, SignInError>) -> Void) {
        guard let meshy = Bins.path("meshy") else { return done(.failure(SignInError(message: "meshy is missing"))) }
        let provider = MeshyStore.shared.syncURL
        DispatchQueue.global().async {
            let r = run(meshy, ["request"] + args, env: ["MESHY_PROVIDER": provider], timeout: 60)
            let out = (r?.stdout ?? "") + (r?.stderr ?? "")
            DispatchQueue.main.async {
                if r?.status == 0, let line = out.components(separatedBy: "\n").first(where: { $0.hasPrefix("MESHY_REQUESTED") }) {
                    done(.success(line.components(separatedBy: " ").first { $0.hasPrefix("id=") }.map { String($0.dropFirst(3)) } ?? ""))
                } else {
                    done(.failure(SignInError(message: out.trimmingCharacters(in: .whitespacesAndNewlines))))
                }
            }
        }
    }
}

/// Turns a MESHY_* line into a sentence for an alert.
func friendly(_ line: String, device: String) -> String {
    let short = device.components(separatedBy: ".").first ?? device
    switch true {
    case line.contains("MESHY_SEATS_EXCEEDED"):
        return "Your org is using more seats than it paid for, so the provider won't let this Mac on the mesh. An owner can add seats in the dashboard (Billing). (\(line))"
    case line.contains("MESHY_NO_SUBSCRIPTION"):
        return "Your org has no active subscription with the provider. An owner can subscribe in the dashboard (Billing)."
    case line.contains("MESHY_NO_GRANT"), line.contains("MESHY_EXPIRED"):
        return "You don't have live access to \(short) any more. Request access again."
    case line.contains("MESHY_REVOKED"):
        return "An admin revoked your access to \(short)."
    case line.contains("MESHY_NO_ACCESS"):
        return "Your grant on \(short) doesn't include any of the folders it shares."
    case line.contains("MESHY_UNREACHABLE"):
        return "\(short) isn't reachable. It may be asleep, offline, or not sharing right now."
    case line.contains("MESHY_NOT_ADMITTED"), line.contains("MESHY_UNKNOWN_AGENT"):
        return "\(short) doesn't recognise this Mac yet. Use Link This Mac, then try again."
    case line.contains("no Meshy identity"):
        return "This Mac isn't linked to your name in the org yet. Use Link This Mac first."
    case line.isEmpty:
        return "Couldn't open \(short)."
    default:
        return line
    }
}

// MARK: - Command-line tools, installed by themselves

enum Tools {
    static let pathLine = "export PATH=\"$HOME/.local/bin:$PATH\" # added by Meshy"

    /// Links the bundled tools into ~/.local/bin (replacing older copies) and makes sure that folder
    /// is on the PATH of new Terminal windows. Quiet; returns what it did.
    @discardableResult
    static func ensure() -> String {
        guard let dir = Bins.dir, Bins.path("meshy") != nil else { return "The tools are missing from Meshy.app. Reinstall Meshy." }
        let fm = FileManager.default
        let bin = Bins.toolsDir
        try? fm.createDirectory(atPath: bin, withIntermediateDirectories: true)
        var linked: [String] = []
        for t in Bins.tools {
            let link = "\(bin)/\(t)", want = "\(dir)/\(t)"
            if (try? fm.destinationOfSymbolicLink(atPath: link)) == want { linked.append(t); continue }
            try? fm.removeItem(atPath: link) // an older symlink, or a copy from the curl installer
            if (try? fm.createSymbolicLink(atPath: link, withDestinationPath: want)) != nil { linked.append(t) }
        }
        let path = addToPath(bin)
        return "Installed \(linked.joined(separator: ", ")) in \(bin.replacingOccurrences(of: Bins.home, with: "~"))." + (path.map { "\n\n\($0)" } ?? "")
    }

    /// Adds ~/.local/bin to ~/.zprofile (zsh, the macOS default) and ~/.bash_profile when no shell
    /// profile mentions it yet. Returns a note when it changed something.
    static func addToPath(_ bin: String) -> String? {
        guard bin == Bins.home + "/.local/bin", !isTest else { return nil }
        let names = [".zprofile", ".zshrc", ".zshenv", ".bash_profile", ".bashrc", ".profile"]
        let profiles = names.compactMap { try? String(contentsOfFile: Bins.home + "/" + $0, encoding: .utf8) }
        if profiles.contains(where: { $0.contains(".local/bin") }) { return nil }
        var changed: [String] = []
        for f in [".zprofile"] + (FileManager.default.fileExists(atPath: Bins.home + "/.bash_profile") ? [".bash_profile"] : []) {
            let p = Bins.home + "/" + f
            let old = (try? String(contentsOfFile: p, encoding: .utf8)) ?? ""
            let new = old + (old.isEmpty || old.hasSuffix("\n") ? "" : "\n") + pathLine + "\n"
            if (try? new.write(toFile: p, atomically: true, encoding: .utf8)) != nil { changed.append("~/" + f) }
        }
        return changed.isEmpty ? nil : "Added ~/.local/bin to your PATH in \(changed.joined(separator: " and ")). New Terminal windows have `meshy`."
    }
}
