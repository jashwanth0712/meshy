// Meshy for macOS: a menu-bar app (no Dock icon). See docs/mac/PLAN.md.
//
// - Sign in with Ethereum in your browser (MetaMask lives there); the signature comes back
//   through meshy://auth, is verified with the bundled `meshy verify-siwe`, and the session
//   is kept in the Keychain.
// - Nothing is assumed: a fresh install is signed out with no orgs. You add your orgs by
//   ENS name; your role (owner, delegated admin, member) is read from ENSv2.
// - Share this Mac, join as an agent, request access: the bundled meshy / meshyd /
//   meshy-mesh binaries (Contents/Resources/bin) do the work, so no curl install is needed.
// - The dashboard (https://meshy.live) opens in your default browser for anything you sign.
//
// Build: mac/build.sh  →  mac/build/Meshy.app
import AppKit
import UserNotifications

// MARK: - Settings

let isTest = ProcessInfo.processInfo.environment["MESHY_TEST"] != nil
/// MESHY_TEST=1 keeps tests out of your real settings, sign-in and LaunchAgents.
let appDefaults: UserDefaults = isTest ? UserDefaults(suiteName: "live.meshy.mac.tests")! : .standard

enum Settings {
    static let defaults = appDefaults
    static let productionURL = "https://meshy.live"
    static let providerURL = "https://api.meshy.live"
    static let sepoliaWS = "wss://ethereum-sepolia-rpc.publicnode.com"
    static let chainID = 11155111
    /// Old hosts from earlier builds, moved to meshy.live on first launch of 0.5.0.
    static let retired = ["https://meshy-bay.vercel.app", "https://meshy-next.vercel.app"]

    static var baseURL: String {
        get {
            if let e = ProcessInfo.processInfo.environment["MESHY_URL"] { return e }
            let v = defaults.string(forKey: "baseURL") ?? productionURL
            return retired.contains(v) ? productionURL : v
        }
        set { defaults.set(newValue == productionURL ? nil : newValue, forKey: "baseURL") }
    }
    static var base: String { baseURL.trimmingCharacters(in: CharacterSet(charactersIn: "/")) }
    static var host: String { URL(string: baseURL)?.host.map { h in URL(string: baseURL)?.port.map { "\(h):\($0)" } ?? h } ?? "meshy.live" }
}

// MARK: - App delegate

final class AppDelegate: NSObject, NSApplicationDelegate, UNUserNotificationCenterDelegate {
    var status: StatusMenu!

    func applicationDidFinishLaunching(_ note: Notification) {
        NSApp.setActivationPolicy(.accessory)
        NSApp.mainMenu = editMenu() // so ⌘C / ⌘V / ⌘A work in dialogs
        if !isTest, Bundle.main.bundleIdentifier != nil { UNUserNotificationCenter.current().delegate = self }
        // One-time migration: copy settings from the old bundle ID domain (eth.meshydemo.mac)
        // into the standard domain (live.meshy.mac) so existing users keep their orgs and prefs.
        if !isTest && !UserDefaults.standard.bool(forKey: "migratedFromEthMeshydemo") {
            if let old = UserDefaults(suiteName: "eth.meshydemo.mac") {
                for (key, value) in old.dictionaryRepresentation() {
                    if UserDefaults.standard.object(forKey: key) == nil {
                        UserDefaults.standard.set(value, forKey: key)
                    }
                }
            }
            UserDefaults.standard.set(true, forKey: "migratedFromEthMeshydemo")
        }
        status = StatusMenu()

        let args = CommandLine.arguments
        if ["--dump-menu", "--callback", "--begin-signin", "--sign-out", "--add-org", "--discover-orgs", "--list-actions", "--click", "--click-all"].contains(where: args.contains) {
            return runTestCommand(args)
        }
        MeshyStore.shared.start()
        DeviceManager.shared.start()
        // `meshy` in Terminal works right after installing the app: link the bundled tools into
        // ~/.local/bin and put that on PATH, every launch (cheap, and follows app updates).
        DispatchQueue.global(qos: .utility).async { Tools.ensure() }
        // Signed in from before: pick up orgs created or joined since (chain state).
        if MeshyStore.shared.address != nil { MeshyStore.shared.discoverOrgs { _ in } }
    }

    /// Headless commands for mac/test.sh (always with MESHY_TEST=1):
    ///   --sign-out                      clear the session
    ///   --begin-signin                  print the browser URL with a fresh nonce
    ///   --callback <meshy://auth?…>     handle a sign-in callback, print accepted/rejected
    ///   --add-org <name> [--role r]     add an org without the network check (role: owner|admin|member)
    ///   --fixture <map.json>            use a map instead of the provider
    ///   --dump-menu                     print the menu tree
    ///   --list-actions                  print every clickable item path
    ///   --click "A › B › C"             click one item (repeatable), then wait --wait seconds
    ///   --click-all                     click every item except Quit; dialogs answer Cancel
    private func runTestCommand(_ args: [String]) {
        let store = MeshyStore.shared
        func arg(_ k: String) -> String? { args.firstIndex(of: k).flatMap { $0 + 1 < args.count ? args[$0 + 1] : nil } }
        func all(_ k: String) -> [String] { args.indices.filter { args[$0] == k && $0 + 1 < args.count }.map { args[$0 + 1] } }
        if args.contains("--sign-out") { store.signOut(); print("signed out") }
        if args.contains("--discover-orgs") {
            store.discoverOrgs { found in print("discovered: \(found.joined(separator: ","))"); exit(0) }
            RunLoop.main.run(until: Date().addingTimeInterval(70))
        }
        if args.contains("--begin-signin"), let u = store.beginSignIn() { print("signin-url: \(u.absoluteString)") }
        if let c = arg("--callback"), let u = URL(string: c) {
            switch store.completeSignIn(u) {
            case .success(let a): print("signin: accepted \(a)")
            case .failure(let e): print("signin: rejected (\(e.message))")
            }
        }
        if let o = arg("--add-org") {
            store.addOrgUnchecked(o)
            if let r = arg("--role") { store.saveRole(o, RoleInfo(role: r, adminOf: r == "admin" ? ["prod.\(o)"] : [], checkedAt: Date().timeIntervalSince1970)) }
        }
        let fixture = arg("--fixture")
        if let f = fixture, !store.loadFixture(f) { print("fixture unreadable"); exit(2) }
        let wantsMenu = args.contains("--dump-menu") || args.contains("--list-actions") || args.contains("--click") || args.contains("--click-all")
        guard wantsMenu else { exit(0) }

        let finish = { [weak self] in
            guard let self else { return }
            if args.contains("--list-actions") { for p in self.status.actionPaths() { print("ACTION: \(p.path)") } }
            let clicks = all("--click")
            for c in clicks { print(self.status.click(c) ? "CLICKED: \(c)" : "NOT FOUND: \(c)") }
            if args.contains("--click-all") {
                let skip = ["Quit Meshy"]
                let paths = self.status.actionPaths().map(\.path).filter { p in !skip.contains { p.hasSuffix($0) } }
                var n = 0
                for p in paths where self.status.click(p) { print("CLICKED: \(p)"); n += 1 }
                print("clicked \(n) of \(paths.count)")
            }
            let wait = Double(arg("--wait") ?? "") ?? (clicks.isEmpty && !args.contains("--click-all") ? 0 : 1)
            DispatchQueue.main.asyncAfter(deadline: .now() + wait) {
                if args.contains("--dump-menu") { print(self.status.dump()) }
                exit(0)
            }
        }
        // With a fixture (or no org) the menu is ready now; otherwise wait for the provider.
        if fixture != nil || store.org == nil { return finish() }
        store.refresh(force: true)
        let started = Date()
        Timer.scheduledTimer(withTimeInterval: 0.5, repeats: true) { t in
            let waited = Date().timeIntervalSince(started) > 6 || !store.events.isEmpty
            if (!store.fetching && store.mapAt != nil && waited) || Date().timeIntervalSince(started) > 40 {
                t.invalidate()
                if store.map == nil { print("no map: \(store.mapError ?? "timeout")") }
                finish()
            }
        }
    }

    // Clicking a request notification opens it in the dashboard, ready to sign.
    func userNotificationCenter(_ center: UNUserNotificationCenter, didReceive response: UNNotificationResponse, withCompletionHandler done: @escaping () -> Void) {
        if let p = response.notification.request.content.userInfo["open"] as? String { openDashboard(p) }
        done()
    }

    func userNotificationCenter(_ center: UNUserNotificationCenter, willPresent n: UNNotification, withCompletionHandler done: @escaping (UNNotificationPresentationOptions) -> Void) {
        done([.banner, .sound])
    }

    func applicationWillTerminate(_ note: Notification) { AccessManager.shared.ejectAll() }

    func applicationShouldTerminateAfterLastWindowClosed(_ app: NSApplication) -> Bool { false }

    // meshy://auth?message=…&sig=… (Sign-In with Ethereum callback), meshy://org/<name>
    func application(_ app: NSApplication, open urls: [URL]) {
        guard let u = urls.first, u.scheme == "meshy" else { return }
        switch u.host {
        case "auth", "signin":
            switch MeshyStore.shared.completeSignIn(u) {
            case .success(let addr):
                status.signedIn(addr)
            case .failure(let e):
                NSApp.activate(ignoringOtherApps: true)
                let a = NSAlert()
                a.messageText = "Sign-in didn't complete"
                a.informativeText = "\(e.message)\n\nChoose Sign In with Ethereum… in the Meshy menu to try again."
                a.runModal()
            }
        case "join":
            // The approval page, opened without keys: make them here, then reopen it with them.
            if let link = URLComponents(url: u, resolvingAgainstBaseURL: false)?.queryItems?.first(where: { $0.name == "link" })?.value { status.join(link: link) }
        case "org":
            if let o = u.pathComponents.dropFirst().first { status.addOrg(named: o) }
        default:
            break
        }
    }

    private func editMenu() -> NSMenu {
        let main = NSMenu()
        let app = NSMenu()
        app.addItem(withTitle: "Quit Meshy", action: #selector(NSApplication.terminate(_:)), keyEquivalent: "q")
        main.addItem(withTitle: "Meshy", action: nil, keyEquivalent: "").submenu = app
        let edit = NSMenu(title: "Edit")
        edit.addItem(withTitle: "Undo", action: Selector(("undo:")), keyEquivalent: "z")
        edit.addItem(withTitle: "Cut", action: #selector(NSText.cut(_:)), keyEquivalent: "x")
        edit.addItem(withTitle: "Copy", action: #selector(NSText.copy(_:)), keyEquivalent: "c")
        edit.addItem(withTitle: "Paste", action: #selector(NSText.paste(_:)), keyEquivalent: "v")
        edit.addItem(withTitle: "Select All", action: #selector(NSText.selectAll(_:)), keyEquivalent: "a")
        main.addItem(withTitle: "Edit", action: nil, keyEquivalent: "").submenu = edit
        return main
    }
}

/// Opens a dashboard path (e.g. "/org/acme.eth/directory?n=…") in the default browser.
func openDashboard(_ path: String) {
    if let u = URL(string: Settings.base + path) { UI.open(u) }
}

/// Percent-encodes a query value (ENS names, paths, join codes).
func q(_ s: String) -> String {
    var allowed = CharacterSet.urlQueryAllowed
    allowed.remove(charactersIn: "&=+?#/")
    return s.addingPercentEncoding(withAllowedCharacters: allowed) ?? s
}

let app = NSApplication.shared
let delegate = AppDelegate()
app.delegate = delegate
app.run()
