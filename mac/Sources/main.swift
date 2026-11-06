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

final class AppDelegate: NSObject, NSApplicationDelegate {
    var status: StatusMenu!

    func applicationDidFinishLaunching(_ note: Notification) {
        NSApp.setActivationPolicy(.accessory)
        NSApp.mainMenu = editMenu() // so ⌘C / ⌘V / ⌘A work in dialogs
        status = StatusMenu()

        let args = CommandLine.arguments
        if args.contains("--dump-menu") || args.contains("--callback") || args.contains("--begin-signin") || args.contains("--sign-out") {
            return runTestCommand(args)
        }
        MeshyStore.shared.start()
        DeviceManager.shared.start()
    }

    /// Headless commands for mac/test.sh (always with MESHY_TEST=1):
    ///   --sign-out                      clear the session and orgs
    ///   --begin-signin                  print the browser URL with a fresh nonce
    ///   --callback <meshy://auth?…>     handle a sign-in callback, print accepted/rejected
    ///   --add-org <name>                add an org without the network role check
    ///   --fixture <map.json>            use a map instead of the provider
    ///   --dump-menu                     print the menu tree
    private func runTestCommand(_ args: [String]) {
        let store = MeshyStore.shared
        func arg(_ k: String) -> String? { args.firstIndex(of: k).flatMap { $0 + 1 < args.count ? args[$0 + 1] : nil } }
        if args.contains("--sign-out") { store.signOut(); print("signed out") }
        if args.contains("--begin-signin"), let u = store.beginSignIn() { print("signin-url: \(u.absoluteString)") }
        if let c = arg("--callback"), let u = URL(string: c) {
            switch store.completeSignIn(u) {
            case .success(let a): print("signin: accepted \(a)")
            case .failure(let e): print("signin: rejected (\(e.message))")
            }
        }
        if let o = arg("--add-org") { store.addOrgUnchecked(o) }
        if let f = arg("--fixture") {
            guard store.loadFixture(f) else { print("fixture unreadable"); exit(2) }
            if args.contains("--dump-menu") { print(status.dump()) }
            exit(0)
        }
        guard args.contains("--dump-menu") else { exit(0) }
        guard store.org != nil else { print(status.dump()); exit(0) }
        store.refresh(force: true)
        let started = Date()
        Timer.scheduledTimer(withTimeInterval: 0.5, repeats: true) { [weak self] t in
            guard let self else { return }
            let waited = Date().timeIntervalSince(started) > 6 || !store.events.isEmpty
            if (!store.fetching && store.mapAt != nil && waited) || Date().timeIntervalSince(started) > 40 {
                t.invalidate()
                print(self.status.dump())
                exit(store.map == nil ? 1 : 0)
            }
        }
    }

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
    if let u = URL(string: Settings.base + path) { NSWorkspace.shared.open(u) }
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
