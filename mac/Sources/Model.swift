// Data for the menu-bar menu: the org map from meshy-sync, refusals and revocations from Arkiv
// (via the dashboard's read-only proxy), agent identities on this Mac, your orgs and roles,
// favorites, access requests, cache. Nothing loads until you sign in and add an org.
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

// MARK: - Role in an org (dashboard /api/role, read from ENSv2)

struct RoleInfo: Codable {
    let role: String // owner | admin | member
    let adminOf: [String]
    let checkedAt: Double
    var canSign: Bool { role == "owner" || role == "admin" }
}

/// An access request this Mac sent to an admin, shown as pending until the grant appears on ENS.
struct AccessRequest: Codable {
    let agent: String
    let target: String
    let path: String?
    let mode: String?
    let dur: Int
    let at: Double
    let link: String
}

// MARK: - Store

final class MeshyStore {
    static let shared = MeshyStore()
    let defaults = appDefaults
    var onChange: (() -> Void)?

    // settings
    var syncURL: String {
        get {
            let v = defaults.string(forKey: "syncURL") ?? Settings.providerURL
            return v.contains("sslip.io") ? Settings.providerURL : v // the old IP-based host moved to api.meshy.live
        }
        set { defaults.set(newValue == Settings.providerURL ? nil : newValue, forKey: "syncURL") }
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

    // session (Session.swift): nothing is shown until you sign in
    private(set) var session: Session?
    var address: String? { session?.address }
    private var who: String { address?.lowercased() ?? "-" }

    // orgs belong to the signed-in address
    var orgs: [String] {
        get { address == nil ? [] : defaults.stringArray(forKey: "orgs.\(who)") ?? [] }
        set { defaults.set(newValue, forKey: "orgs.\(who)") }
    }
    var org: String? {
        guard address != nil else { return nil }
        let o = defaults.string(forKey: "org.\(who)")
        return o.flatMap { orgs.contains($0) ? $0 : nil } ?? orgs.first
    }
    private func setOrg(_ o: String?) { defaults.set(o, forKey: "org.\(who)") }

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
        session = Keychain.read()
        loadCache()
        loadLocalAgents()
        NSWorkspace.shared.notificationCenter.addObserver(forName: NSWorkspace.didWakeNotification, object: nil, queue: .main) { [weak self] _ in self?.refresh(force: true) }
    }

    func start() {
        refresh(force: true)
        if let o = org, role(o) == nil || Date().timeIntervalSince1970 - (role(o)?.checkedAt ?? 0) > 3600 { checkRole(o) { _ in } }
        schedule()
        if notificationsOn && address != nil { requestNotificationAuth() }
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

    // MARK: session

    func setSession(_ s: Session) {
        session = s
        resetData()
        loadCache()
        onChange?()
        refresh(force: true)
        if let o = org { checkRole(o) { _ in } }
        if notificationsOn { requestNotificationAuth() }
    }

    func signOut() {
        Keychain.delete()
        session = nil
        resetData()
        onChange?()
    }

    private func resetData() {
        map = nil
        mapAt = nil
        mapError = nil
        events = []
        seenEvents = []
        primedEvents = false
    }

    // MARK: roles

    func role(_ o: String) -> RoleInfo? {
        guard let d = defaults.data(forKey: "role.\(who).\(o)") else { return nil }
        return try? JSONDecoder().decode(RoleInfo.self, from: d)
    }

    func saveRole(_ o: String, _ info: RoleInfo) {
        if let d = try? JSONEncoder().encode(info) { defaults.set(d, forKey: "role.\(who).\(o)") }
        onChange?()
    }

    /// Reads the address's role in an org from ENSv2 through the dashboard. nil role = not a Meshy org.
    func checkRole(_ o: String, done: @escaping (Result<RoleInfo?, SignInError>) -> Void) {
        guard let a = address else { return done(.failure(SignInError(message: "Sign in first."))) }
        var c = URLComponents(string: Settings.base + "/api/role")
        c?.queryItems = [URLQueryItem(name: "org", value: o), URLQueryItem(name: "address", value: a)]
        guard let url = c?.url else { return done(.failure(SignInError(message: "Bad org name."))) }
        var req = URLRequest(url: url, timeoutInterval: 45)
        req.setValue("MeshyMac/\(appVersion)", forHTTPHeaderField: "User-Agent")
        URLSession.shared.dataTask(with: req) { [weak self] data, _, err in
            DispatchQueue.main.async {
                guard let self else { return }
                guard let data, let j = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else {
                    return done(.failure(SignInError(message: err?.localizedDescription ?? "The dashboard didn't answer.")))
                }
                if let e = j["error"] as? String { return done(.failure(SignInError(message: e))) }
                guard j["exists"] as? Bool == true, let r = j["role"] as? String else { return done(.success(nil)) }
                let info = RoleInfo(role: r, adminOf: j["adminOf"] as? [String] ?? [], checkedAt: Date().timeIntervalSince1970)
                self.saveRole(o, info)
                done(.success(info))
            }
        }.resume()
    }

    /// One line for the header: "owner", "admin of prod", "member", plus "agent claude-1" for identities here.
    var roleLine: String? {
        guard let o = org else { return nil }
        var parts: [String] = []
        if let r = role(o) {
            parts.append(r.role == "admin" ? "admin of " + r.adminOf.map { $0.replacingOccurrences(of: ".\(o)", with: "") }.joined(separator: ", ") : r.role)
        }
        let mine = agentsHere.map(\.agent)
        if !mine.isEmpty { parts.append("agent " + mine.joined(separator: ", ")) }
        return parts.isEmpty ? nil : parts.joined(separator: " · ")
    }

    // MARK: orgs

    func switchOrg(_ o: String) {
        guard o != org, orgs.contains(o) else { return }
        setOrg(o)
        resetData()
        loadCache()
        onChange?()
        refresh(force: true)
        if role(o) == nil { checkRole(o) { _ in } }
    }

    /// Finds every org this wallet belongs to, from chain state via the provider
    /// (GET /v1/orgs?addr=): orgs it created, orgs where it owns or administers a
    /// registry, and names it claimed with an invite. Adds them all; returns them.
    func discoverOrgs(done: @escaping ([String]) -> Void) {
        guard let a = address else { return done([]) }
        var c = URLComponents(string: syncURL + "/v1/orgs")
        c?.queryItems = [URLQueryItem(name: "addr", value: a)]
        guard let url = c?.url else { return done([]) }
        var req = URLRequest(url: url, timeoutInterval: 60)
        req.setValue("MeshyMac/\(appVersion)", forHTTPHeaderField: "User-Agent")
        URLSession.shared.dataTask(with: req) { [weak self] data, _, _ in
            DispatchQueue.main.async {
                guard let self, let data,
                      let j = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
                      let list = j["orgs"] as? [[String: Any]] else { return done([]) }
                var found: [String] = []
                for o in list {
                    guard let name = o["org"] as? String, let role = o["role"] as? String else { continue }
                    // admin scope is filled in by checkRole later; "agent"/"member" are both members here
                    self.saveRole(name, RoleInfo(role: role == "agent" ? "member" : role, adminOf: [], checkedAt: Date().timeIntervalSince1970))
                    found.append(name)
                }
                let new = found.filter { !self.orgs.contains($0) }
                if !new.isEmpty { self.orgs = self.orgs + new }
                if self.org == nil, let first = found.first {
                    self.setOrg(first)
                    self.resetData()
                    self.loadCache()
                    self.refresh(force: true)
                }
                self.onChange?()
                done(found)
            }
        }.resume()
    }

    /// Adds an org after checking ENS: nil = it isn't a Meshy org.
    func addOrg(_ o: String, done: @escaping (Result<RoleInfo?, SignInError>) -> Void) {
        checkRole(o) { [weak self] r in
            if case .success(.some) = r { self?.addOrgUnchecked(o) }
            done(r)
        }
    }

    func addOrgUnchecked(_ o: String) {
        guard address != nil else { return }
        if !orgs.contains(o) { orgs = orgs + [o] }
        setOrg(o)
        resetData()
        loadCache()
        onChange?()
        refresh(force: true)
    }

    func removeOrg(_ o: String) {
        orgs = orgs.filter { $0 != o }
        if o == org || org == nil { setOrg(orgs.first); resetData(); loadCache(); refresh(force: true) }
        onChange?()
    }

    // MARK: favorites

    private var favKey: String { "favorites.\(who).\(org ?? "-")" }
    var favorites: [String] { defaults.stringArray(forKey: favKey) ?? [] }
    func isFavorite(_ name: String) -> Bool { favorites.contains(name) }
    func toggleFavorite(_ name: String) {
        var f = favorites
        if let i = f.firstIndex(of: name) { f.remove(at: i) } else { f.append(name) }
        defaults.set(f, forKey: favKey)
        onChange?()
    }

    // MARK: access requests (pending until the grant shows up)

    private var reqKey: String { "requests.\(who).\(org ?? "-")" }
    var requests: [AccessRequest] {
        get {
            guard let d = defaults.data(forKey: reqKey), let a = try? JSONDecoder().decode([AccessRequest].self, from: d) else { return [] }
            let now = Date().timeIntervalSince1970
            let live = map?.liveGrants ?? []
            return a.filter { r in now - r.at < 86400 && !live.contains { $0.agentLabel == r.agent && $0.target == r.target && $0.expiry > r.at } }
        }
        set { defaults.set(try? JSONEncoder().encode(newValue), forKey: reqKey) }
    }
    func addRequest(_ r: AccessRequest) { requests = requests + [r]; onChange?() }
    func dropRequest(at i: Int) { var a = requests; if a.indices.contains(i) { a.remove(at: i) }; requests = a; onChange?() }

    // MARK: refresh

    func refresh(force: Bool) {
        guard let org else { return }
        if fetching { return }
        if !force, let at = mapAt, Date().timeIntervalSince(at) < 15 { return }
        fetching = true
        onChange?()
        var c = URLComponents(string: syncURL.trimmingCharacters(in: CharacterSet(charactersIn: "/")) + "/v1/map")
        c?.queryItems = [URLQueryItem(name: "org", value: org)]
        guard let url = c?.url else { fetching = false; return }
        var req = URLRequest(url: url, timeoutInterval: 20)
        req.setValue("MeshyMac/\(appVersion)", forHTTPHeaderField: "User-Agent")
        URLSession.shared.dataTask(with: req) { [weak self] data, resp, err in
            DispatchQueue.main.async {
                guard let self else { return }
                self.fetching = false
                guard org == self.org else { return }
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
        guard let m = map, let org else { return }
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
            guard let self, org == self.org else { return }
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
        guard let url = URL(string: Settings.base + "/api/arkiv") else { return done([]) }
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
        guard !isTest, Bundle.main.bundleIdentifier != nil else { return }
        UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound]) { _, _ in }
    }

    func notify(title: String, body: String, id: String = UUID().uuidString) {
        guard !isTest, notificationsOn else { return }
        let c = UNMutableNotificationContent()
        c.title = title
        c.body = body
        UNUserNotificationCenter.current().add(UNNotificationRequest(identifier: id, content: c, trigger: nil))
    }

    private func notify(_ e: ArkivEvent) {
        notify(title: e.kind == "session.revoke" ? "Access revoked" : "Access refused", body: e.title, id: e.key)
    }

    // MARK: cache and local identities

    private var cacheURL: URL? {
        guard let org, let dir = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask).first?.appendingPathComponent(isTest ? "Meshy-tests" : "Meshy", isDirectory: true) else { return nil }
        try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        return dir.appendingPathComponent("map-\(org).json")
    }

    private func saveCache(_ data: Data) {
        if let u = cacheURL { try? data.write(to: u) }
    }

    private func loadCache() {
        guard let u = cacheURL, let d = try? Data(contentsOf: u), let m = try? JSONDecoder().decode(OrgMap.self, from: d), m.org == org else { return }
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

    /// Agent identities created on this Mac by `meshy join` (~/.meshy, or $MESHY_HOME).
    func loadLocalAgents() {
        let home = URL(fileURLWithPath: Bins.meshyHome)
        let dirs = (try? FileManager.default.contentsOfDirectory(at: home, includingPropertiesForKeys: nil)) ?? []
        var out: [LocalAgent] = []
        for d in dirs where d.lastPathComponent.hasPrefix("agent") {
            if let data = try? Data(contentsOf: d.appendingPathComponent("agent.json")), let a = try? JSONDecoder().decode(LocalAgent.self, from: data) { out.append(a) }
        }
        if let data = try? Data(contentsOf: home.appendingPathComponent("agent.json")), let a = try? JSONDecoder().decode(LocalAgent.self, from: data) { out.append(a) }
        localAgents = out
    }

    /// This Mac's agent identities in the selected org.
    var agentsHere: [LocalAgent] { localAgents.filter { $0.org == org } }

    var statusLine: String {
        if fetching && map == nil { return "Connecting to the provider…" }
        guard let m = map, let at = mapAt else { return mapError.map { "Offline · \($0)" } ?? "No data yet" }
        let age = ago(at)
        if mapError != nil { return "Offline · showing data from \(age)" }
        let block = m.block.map { "#" + groupedNumber($0) } ?? ""
        return "Sepolia \(block) · updated \(age)\(fetching ? " · updating…" : "")"
    }
}

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
