// Data for the menu-bar menu: the org map from meshy-sync, refusals and revocations from Arkiv
// (via the dashboard's read-only proxy), agent identities on this Mac, sign-in, favorites, cache.
// See docs/mac/PLAN.md §2–§3.
import AppKit
import Foundation
import UserNotifications

// MARK: - meshy-sync /v1/map (docs/INTERFACES.md §7)

struct MapService: Codable {
    let name: String
    let fullName: String
    let device: String
    let spec: String?
    var specObj: [String: String] { jsonStrings(spec) }
    var proto: String { specObj["proto"] ?? "tcp" }
    var target: String { specObj["target"] ?? "" }
    var desc: String { specObj["desc"] ?? "" }
}

struct MapResource: Codable {
    let name: String
    let fullName: String
    let addr: String?
    let endpoint: String?
    let kind: String?
    let meta: String?
    let roots: String?
    let services: [MapService]?
    var metaObj: [String: String] { jsonStrings(meta) }
    var rootList: [String] {
        guard let r = roots, let d = r.data(using: .utf8), let a = try? JSONDecoder().decode([String].self, from: d) else { return [] }
        return a
    }
    var summary: String {
        [metaObj["model"] ?? kind, metaObj["location"]].compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: " · ")
    }
}

struct MapNetwork: Codable {
    let name: String
    let fullName: String
    let resources: [MapResource]?
}

struct MapAgent: Codable {
    let name: String
    let fullName: String
    let addr: String?
    let owner: String?
    let worldIdValid: Bool?
}

struct MapGrant: Codable {
    let name: String
    let fullName: String
    let owner: String?
    let expiry: Double
    let status: String
    let kind: String?
    let agent: String?
    let target: String?
    let device: String?
    var isLive: Bool { status == "live" && expiry > Date().timeIntervalSince1970 }
    var agentLabel: String { (agent ?? name).components(separatedBy: ".").first ?? name }
    var isService: Bool { kind == "service" }
}

struct OrgMap: Codable {
    let org: String
    let networks: [MapNetwork]?
    let agents: [MapAgent]?
    let grants: [MapGrant]?
    let block: Int?
    let generatedAt: String?
    var allNetworks: [MapNetwork] { networks ?? [] }
    var devices: [MapResource] { allNetworks.flatMap { $0.resources ?? [] } }
    var services: [MapService] { devices.flatMap { $0.services ?? [] } }
    var liveGrants: [MapGrant] { (grants ?? []).filter(\.isLive).sorted { $0.expiry < $1.expiry } }
    var endedGrants: [MapGrant] { (grants ?? []).filter { !$0.isLive } }
}

func jsonStrings(_ s: String?) -> [String: String] {
    guard let s, let d = s.data(using: .utf8), let o = try? JSONSerialization.jsonObject(with: d) as? [String: Any] else { return [:] }
    return o.compactMapValues { "\($0)" }
}

// MARK: - Arkiv events (refusals, revocations)

struct ArkivEvent: Codable, Hashable {
    let key: String
    let kind: String
    let agent: String
    let resource: String
    let reason: String
    let ts: Double
    let block: Int?
    let latencyMs: Int?
    var title: String {
        let res = resource.components(separatedBy: ".").prefix(2).joined(separator: ".")
        switch kind {
        case "session.revoke": return "\(agent)'s session on \(res) ended (revoked)"
        default: return "\(agent) was refused on \(res)"
        }
    }
}

// MARK: - Identities on this Mac (~/.meshy/agent*/agent.json)

struct LocalAgent: Codable {
    let agent: String
    let org: String
    let addr: String
}

// MARK: - Store

final class MeshyStore {
    static let shared = MeshyStore()
    let defaults = appDefaults
    var onChange: (() -> Void)?

    // settings
    var syncURL: String {
        get { defaults.string(forKey: "syncURL") ?? "https://sync.16-4-10-3.sslip.io" }
        set { defaults.set(newValue, forKey: "syncURL") }
    }
    var openInBrowser: Bool {
        get { defaults.bool(forKey: "openInBrowser") }
        set { defaults.set(newValue, forKey: "openInBrowser") }
    }
    var refreshSeconds: Double {
        get { let v = defaults.double(forKey: "refreshSeconds"); return v > 0 ? v : 30 }
        set { defaults.set(newValue, forKey: "refreshSeconds") }
    }
    var live: Bool {
        get { defaults.object(forKey: "live") as? Bool ?? true }
        set { defaults.set(newValue, forKey: "live"); if newValue { refresh(force: true) }; onChange?() }
    }
    var notificationsOn: Bool {
        get { defaults.object(forKey: "notifications") as? Bool ?? true }
        set { defaults.set(newValue, forKey: "notifications"); if newValue { requestNotificationAuth() } }
    }

    // orgs
    var orgs: [String] {
        get {
            var a = defaults.stringArray(forKey: "orgs") ?? []
            if !a.contains(Settings.org) { a.insert(Settings.org, at: 0) }
            return a
        }
        set { defaults.set(newValue, forKey: "orgs") }
    }
    var org: String { Settings.org }

    // identity (sign-in)
    var address: String? {
        get { defaults.string(forKey: "identity.address") }
        set { defaults.set(newValue, forKey: "identity.address") }
    }
    private(set) var signature: String? {
        get { defaults.string(forKey: "identity.sig") }
        set { defaults.set(newValue, forKey: "identity.sig") }
    }
    private var pendingState: String? {
        get { defaults.string(forKey: "signin.state") }
        set { defaults.set(newValue, forKey: "signin.state") }
    }
    private var pendingAt: Double {
        get { defaults.double(forKey: "signin.at") }
        set { defaults.set(newValue, forKey: "signin.at") }
    }

    // data
    private(set) var map: OrgMap?
    private(set) var mapAt: Date?
    private(set) var mapError: String?
    private(set) var fetching = false
    private(set) var events: [ArkivEvent] = []
    private(set) var localAgents: [LocalAgent] = []
    private var seenEvents = Set<String>()
    private var primedEvents = false
    private var timer: Timer?
    private var failures = 0

    private init() {
        loadCache()
        loadLocalAgents()
        NSWorkspace.shared.notificationCenter.addObserver(forName: NSWorkspace.didWakeNotification, object: nil, queue: .main) { [weak self] _ in self?.refresh(force: true) }
    }

    func start() {
        refresh(force: true)
        schedule()
    }

    private func schedule() {
        timer?.invalidate()
        let backoff = min(4, pow(2, Double(failures)))
        timer = Timer.scheduledTimer(withTimeInterval: refreshSeconds * backoff, repeats: false) { [weak self] _ in
            guard let self else { return }
            if self.live { self.refresh(force: false) }
            self.schedule()
        }
    }

    // MARK: identity

    enum Role: String { case admin, agent, member }
    var role: Role? {
        guard let a = address?.lowercased(), let m = map else { return nil }
        if (m.agents ?? []).contains(where: { $0.addr?.lowercased() == a }) { return .agent }
        if (m.agents ?? []).contains(where: { $0.owner?.lowercased() == a }) { return .admin }
        return .member
    }

    func beginSignIn(dashboard: String) -> URL? {
        var bytes = [UInt8](repeating: 0, count: 16)
        _ = SecRandomCopyBytes(kSecRandomDefault, bytes.count, &bytes)
        let state = bytes.map { String(format: "%02x", $0) }.joined()
        pendingState = state
        pendingAt = Date().timeIntervalSince1970
        var c = URLComponents(string: dashboard.trimmingCharacters(in: CharacterSet(charactersIn: "/")) + "/connect")
        c?.queryItems = [URLQueryItem(name: "state", value: state), URLQueryItem(name: "org", value: org)]
        return c?.url
    }

    /// meshy://signin?state=…&address=…&sig=…: accepted only for the state we issued, within 10 min.
    @discardableResult
    func completeSignIn(_ url: URL) -> Bool {
        let q = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems ?? []
        func v(_ k: String) -> String? { q.first { $0.name == k }?.value }
        guard let st = v("state"), let want = pendingState, st == want,
              Date().timeIntervalSince1970 - pendingAt < 600,
              let addr = v("address"), addr.range(of: "^0x[0-9a-fA-F]{40}$", options: .regularExpression) != nil
        else { return false }
        address = addr
        signature = v("sig")
        pendingState = nil
        onChange?()
        return true
    }

    func useAddress(_ a: String) -> Bool {
        guard a.range(of: "^0x[0-9a-fA-F]{40}$", options: .regularExpression) != nil else { return false }
        address = a
        signature = nil
        onChange?()
        return true
    }

    func logOut() {
        address = nil
        signature = nil
        onChange?()
    }

    // MARK: orgs

    func switchOrg(_ o: String) {
        guard o != org else { return }
        Settings.org = o
        Settings.lastPath = nil
        map = nil
        mapAt = nil
        mapError = nil
        events = []
        seenEvents = []
        primedEvents = false
        loadCache()
        onChange?()
        NotificationCenter.default.post(name: .meshyOrgChanged, object: nil)
        refresh(force: true)
    }

    func addOrg(_ o: String) {
        var a = orgs
        if !a.contains(o) { a.append(o) }
        orgs = a
        switchOrg(o)
    }

    func removeOrg(_ o: String) {
        var a = orgs.filter { $0 != o }
        if a.isEmpty { a = ["meshydemo.eth"] }
        orgs = a
        if o == org { switchOrg(a[0]) } else { onChange?() }
    }

    // MARK: favorites

    private var favKey: String { "favorites.\(org)" }
    var favorites: [String] { defaults.stringArray(forKey: favKey) ?? [] }
    func isFavorite(_ name: String) -> Bool { favorites.contains(name) }
    func toggleFavorite(_ name: String) {
        var f = favorites
        if let i = f.firstIndex(of: name) { f.remove(at: i) } else { f.append(name) }
        defaults.set(f, forKey: favKey)
        onChange?()
    }

    // MARK: refresh

    func refresh(force: Bool) {
        if fetching { return }
        if !force, let at = mapAt, Date().timeIntervalSince(at) < 15 { return }
        fetching = true
        onChange?()
        let org = self.org
        var c = URLComponents(string: syncURL.trimmingCharacters(in: CharacterSet(charactersIn: "/")) + "/v1/map")
        c?.queryItems = [URLQueryItem(name: "org", value: org)]
        guard let url = c?.url else { fetching = false; return }
        var req = URLRequest(url: url, timeoutInterval: 20)
        req.setValue("MeshyMac/\(appVersion)", forHTTPHeaderField: "User-Agent")
        URLSession.shared.dataTask(with: req) { [weak self] data, resp, err in
            DispatchQueue.main.async {
                guard let self, org == self.org else { return }
                self.fetching = false
                let code = (resp as? HTTPURLResponse)?.statusCode ?? 0
                if let data, code == 200, let m = try? JSONDecoder().decode(OrgMap.self, from: data) {
                    self.map = m
                    self.mapAt = Date()
                    self.mapError = nil
                    self.failures = 0
                    self.saveCache(data)
                    self.fetchEvents()
                } else {
                    self.failures += 1
                    if let data, let s = String(data: data, encoding: .utf8), s.contains("error") {
                        self.mapError = (try? JSONSerialization.jsonObject(with: data) as? [String: Any])?["error"] as? String ?? "Provider error \(code)"
                    } else {
                        self.mapError = err?.localizedDescription ?? "Provider error \(code)"
                    }
                }
                self.loadLocalAgents()
                self.onChange?()
            }
        }.resume()
    }

    private func fetchEvents() {
        guard let m = map else { return }
        let writers = m.devices.compactMap(\.addr).filter { $0.range(of: "^0x[0-9a-fA-F]{40}$", options: .regularExpression) != nil }
        guard !writers.isEmpty else { return }
        let owner = " AND (" + writers.map { "$owner = addr(\($0))" }.joined(separator: " OR ") + ")"
        let o = org.replacingOccurrences(of: "'", with: "")
        let queries = [
            "arkiv.schema = str('meshy.blocked') AND org = str('\(o)')\(owner) ORDER BY ts:u64 DESC",
            "arkiv.schema = str('meshy.session') AND kind = str('session.revoke') AND org = str('\(o)')\(owner) ORDER BY ts:u64 DESC",
        ]
        let group = DispatchGroup()
        var found: [ArkivEvent] = []
        let lock = NSLock()
        for q in queries {
            group.enter()
            arkivQuery(q, limit: 12) { rows in
                lock.lock(); found += rows; lock.unlock()
                group.leave()
            }
        }
        group.notify(queue: .main) { [weak self] in
            guard let self else { return }
            // newest first; one row per (text, minute) so batch writes don't show twice
            var seenText = Set<String>()
            let fresh = found.sorted { $0.ts > $1.ts }.filter { seenText.insert("\($0.title)|\(Int($0.ts / 60))").inserted }
            if self.primedEvents && self.notificationsOn && self.live {
                for e in fresh.reversed() where !self.seenEvents.contains(e.key) { self.notify(e) }
            }
            for e in fresh { self.seenEvents.insert(e.key) }
            self.primedEvents = true
            self.events = Array(fresh.prefix(8))
            self.onChange?()
        }
    }

    private func arkivQuery(_ q: String, limit: Int, done: @escaping ([ArkivEvent]) -> Void) {
        guard let url = URL(string: Settings.baseURL.trimmingCharacters(in: CharacterSet(charactersIn: "/")) + "/api/arkiv") else { return done([]) }
        var req = URLRequest(url: url, timeoutInterval: 20)
        req.httpMethod = "POST"
        req.setValue("application/json", forHTTPHeaderField: "content-type")
        let body: [String: Any] = ["jsonrpc": "2.0", "id": 1, "method": "arkiv_query", "params": [q, ["select": ["key": true, "attributes": true], "limit": "0x" + String(limit, radix: 16)]]]
        req.httpBody = try? JSONSerialization.data(withJSONObject: body)
        URLSession.shared.dataTask(with: req) { data, _, _ in
            guard let data, let j = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else { return done([]) }
            let result = j["result"]
            let items = (result as? [[String: Any]]) ?? ((result as? [String: Any])?["data"] as? [[String: Any]]) ?? []
            let rows: [ArkivEvent] = items.compactMap { it in
                guard let key = it["key"] as? String else { return nil }
                var a: [String: Any] = [:]
                for at in it["attributes"] as? [[String: Any]] ?? [] { if let n = at["name"] as? String { a[n] = at["value"] } }
                func num(_ x: Any?) -> Double? {
                    if let d = x as? Double { return d }
                    if let i = x as? Int { return Double(i) }
                    if let s = x as? String { return s.hasPrefix("0x") ? Double(UInt64(s.dropFirst(2), radix: 16) ?? 0) : Double(s) }
                    return nil
                }
                var ts = num(a["ts"]) ?? 0
                if ts > 1e12 { ts /= 1000 } // ms → s
                return ArkivEvent(key: key, kind: a["kind"] as? String ?? "", agent: a["agent"] as? String ?? "someone", resource: a["resource"] as? String ?? "", reason: a["reason"] as? String ?? "",
                                  ts: ts, block: num(a["block"]).map { Int($0) }, latencyMs: num(a["latencyMs"]).map { Int($0) })
            }
            done(rows)
        }.resume()
    }

    // MARK: notifications

    func requestNotificationAuth() {
        UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound]) { _, _ in }
    }

    private func notify(_ e: ArkivEvent) {
        let c = UNMutableNotificationContent()
        c.title = e.kind == "session.revoke" ? "Access revoked" : "Access refused"
        c.body = e.title
        UNUserNotificationCenter.current().add(UNNotificationRequest(identifier: e.key, content: c, trigger: nil))
    }

    // MARK: cache and local identities

    private var cacheURL: URL? {
        guard let dir = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask).first?.appendingPathComponent("Meshy", isDirectory: true) else { return nil }
        try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        return dir.appendingPathComponent("map-\(org).json")
    }

    private func saveCache(_ data: Data) {
        if let u = cacheURL { try? data.write(to: u) }
    }

    private func loadCache() {
        guard let u = cacheURL, let d = try? Data(contentsOf: u), let m = try? JSONDecoder().decode(OrgMap.self, from: d) else { return }
        map = m
        mapAt = (try? u.resourceValues(forKeys: [.contentModificationDateKey]))?.contentModificationDate
    }

    /// For tests: load a fixture instead of the network.
    func loadFixture(_ path: String) -> Bool {
        guard let d = FileManager.default.contents(atPath: path), let m = try? JSONDecoder().decode(OrgMap.self, from: d) else { return false }
        map = m
        mapAt = Date()
        return true
    }

    func loadLocalAgents() {
        let home = FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent(".meshy")
        let dirs = (try? FileManager.default.contentsOfDirectory(at: home, includingPropertiesForKeys: nil)) ?? []
        var out: [LocalAgent] = []
        for d in dirs where d.lastPathComponent.hasPrefix("agent") {
            if let data = try? Data(contentsOf: d.appendingPathComponent("agent.json")), let a = try? JSONDecoder().decode(LocalAgent.self, from: data) { out.append(a) }
        }
        if let data = try? Data(contentsOf: home.appendingPathComponent("agent.json")), let a = try? JSONDecoder().decode(LocalAgent.self, from: data) { out.append(a) }
        localAgents = out
    }

    var statusLine: String {
        if fetching && map == nil { return "Connecting to the provider…" }
        guard let m = map, let at = mapAt else { return mapError.map { "Offline · \($0)" } ?? "No data yet" }
        let age = ago(at)
        if mapError != nil { return "Offline · showing data from \(age)" }
        let block = m.block.map { "#" + groupedNumber($0) } ?? ""
        return "Sepolia \(block) · updated \(age)\(fetching ? " · updating…" : "")"
    }
}

extension Notification.Name { static let meshyOrgChanged = Notification.Name("meshyOrgChanged") }

let appVersion = Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "dev"

func ago(_ d: Date) -> String {
    let s = max(0, Int(Date().timeIntervalSince(d)))
    if s < 10 { return "just now" }
    if s < 60 { return "\(s)s ago" }
    if s < 3600 { return "\(s / 60)m ago" }
    if s < 86400 { return "\(s / 3600)h ago" }
    return "\(s / 86400)d ago"
}

func left(_ expiry: Double) -> String {
    let s = Int(expiry - Date().timeIntervalSince1970)
    if s <= 0 { return "expired" }
    let h = s / 3600, m = (s % 3600) / 60, x = s % 60
    return h > 0 ? "\(h)h \(m)m left" : m > 0 ? "\(m)m \(String(format: "%02d", x))s left" : "\(x)s left"
}

func groupedNumber(_ n: Int) -> String {
    let f = NumberFormatter()
    f.locale = Locale(identifier: "en_US_POSIX")
    f.numberStyle = .decimal
    f.usesGroupingSeparator = true
    f.groupingSeparator = ","
    f.groupingSize = 3
    return f.string(from: NSNumber(value: n)) ?? String(n)
}

func plural(_ n: Int, _ w: String) -> String { "\(n) \(w)\(n == 1 ? "" : "s")" }

func shortAddr(_ a: String) -> String { a.count > 12 ? "\(a.prefix(6))…\(a.suffix(4))" : a }
