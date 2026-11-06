// Meshy for macOS: a native window around the Meshy dashboard (WKWebView).
//
// - Loads the live dashboard (https://meshy-bay.vercel.app, production of main) for one org.
//   Every screen reads ENSv2 on Sepolia and Arkiv; nothing is bundled or hardcoded.
// - Same-site links stay in the app; Etherscan, ENS, Arkiv, World ID open in your browser.
// - Wallet signing needs a browser extension (MetaMask), which WebKit apps can't host, so
//   "Connect wallet" offers to open the current page in your default browser instead.
// - ⌘1–⌘6 jump between pages, ⇧⌘O switches org, ⇧⌘B opens the page in your browser.
// - Lives in the menu bar (StatusMenu.swift); this window is the "Admin Console".
//
// Build: mac/build.sh  →  mac/build/Meshy.app
import AppKit
import WebKit

// MARK: - Settings

/// MESHY_TEST=1 keeps tests out of your real settings and sign-in.
let appDefaults: UserDefaults = ProcessInfo.processInfo.environment["MESHY_TEST"] != nil ? UserDefaults(suiteName: "eth.meshydemo.mac.tests")! : .standard

enum Settings {
    static let defaults = appDefaults
    static let productionURL = "https://meshy-bay.vercel.app" // production, deployed from main
    static let classicURL = "https://meshy-next.vercel.app" // the branch build of the redesign
    static let localURL = "http://localhost:3000"

    static var baseURL: String {
        get { ProcessInfo.processInfo.environment["MESHY_URL"] ?? defaults.string(forKey: "baseURL") ?? productionURL }
        set { defaults.set(newValue, forKey: "baseURL") }
    }
    static var org: String {
        get { defaults.string(forKey: "org") ?? "meshydemo.eth" }
        set { defaults.set(newValue, forKey: "org") }
    }
    static var lastPath: String? {
        get { defaults.string(forKey: "lastPath") }
        set { defaults.set(newValue, forKey: "lastPath") }
    }
    static var host: String? { URL(string: baseURL)?.host }
}

let pages: [(title: String, path: String, key: String)] = [
    ("Access", "", "1"),
    ("Directory", "/directory", "2"),
    ("Activity", "/activity", "3"),
    ("Billing", "/billing", "4"),
    ("Provider", "/provider", "5"),
    ("Settings", "/settings", "6"),
]

// Runs in every page before its own scripts. Catches "Connect wallet" when no wallet is injected.
let walletShim = """
(() => {
  document.addEventListener('click', (e) => {
    if (window.ethereum) return;
    const b = e.target && e.target.closest && e.target.closest('button, a');
    if (!b) return;
    const t = (b.textContent || '').trim().toLowerCase();
    if (t.includes('connect wallet') || t.includes('connect a wallet')) {
      e.preventDefault(); e.stopImmediatePropagation();
      window.webkit.messageHandlers.meshy.postMessage({ type: 'wallet', url: location.href });
    }
  }, true);
})();
"""

// MARK: - Window controller

final class BrowserController: NSWindowController, WKNavigationDelegate, WKUIDelegate, WKScriptMessageHandler, NSWindowDelegate {
    let webView: WKWebView
    private let progress = NSProgressIndicator()
    private var progressObs: NSKeyValueObservation?
    private var titleObs: NSKeyValueObservation?

    init() {
        let config = WKWebViewConfiguration()
        config.websiteDataStore = .default() // keeps the org key (localStorage) between launches
        config.applicationNameForUserAgent = "MeshyMac/0.1"
        let ucc = WKUserContentController()
        ucc.addUserScript(WKUserScript(source: walletShim, injectionTime: .atDocumentStart, forMainFrameOnly: true))
        config.userContentController = ucc
        webView = WKWebView(frame: .zero, configuration: config)
        webView.allowsBackForwardNavigationGestures = true
        webView.allowsMagnification = true
        if #available(macOS 13.3, *) { webView.isInspectable = true }
        webView.setValue(false, forKey: "drawsBackground") // no white flash before first paint

        let window = NSWindow(
            contentRect: NSRect(x: 0, y: 0, width: 1320, height: 860),
            styleMask: [.titled, .closable, .miniaturizable, .resizable, .fullSizeContentView],
            backing: .buffered, defer: false)
        window.title = "Meshy"
        window.titlebarAppearsTransparent = true
        window.titleVisibility = .hidden
        window.backgroundColor = .windowBackgroundColor // follows light/dark like the pages
        window.minSize = NSSize(width: 860, height: 560)
        window.setFrameAutosaveName("MeshyMain")
        window.tabbingMode = .disallowed
        super.init(window: window)

        ucc.add(WeakHandler(self), name: "meshy")
        webView.navigationDelegate = self
        webView.uiDelegate = self
        window.delegate = self

        // Layout: the web view fills the window; a 2 px progress line sits under the titlebar.
        let root = NSView()
        webView.translatesAutoresizingMaskIntoConstraints = false
        progress.translatesAutoresizingMaskIntoConstraints = false
        progress.style = .bar
        progress.isIndeterminate = false
        progress.minValue = 0
        progress.maxValue = 1
        progress.controlSize = .small
        root.addSubview(webView)
        root.addSubview(progress)
        NSLayoutConstraint.activate([
            webView.leadingAnchor.constraint(equalTo: root.leadingAnchor),
            webView.trailingAnchor.constraint(equalTo: root.trailingAnchor),
            webView.topAnchor.constraint(equalTo: root.safeAreaLayoutGuide.topAnchor), // keep traffic lights off the page
            webView.bottomAnchor.constraint(equalTo: root.bottomAnchor),
            progress.leadingAnchor.constraint(equalTo: root.leadingAnchor),
            progress.trailingAnchor.constraint(equalTo: root.trailingAnchor),
            progress.topAnchor.constraint(equalTo: root.safeAreaLayoutGuide.topAnchor),
            progress.heightAnchor.constraint(equalToConstant: 2),
        ])
        window.contentView = root
        progressObs = webView.observe(\.estimatedProgress, options: [.new]) { [weak self] wv, _ in
            guard let self else { return }
            self.progress.doubleValue = wv.estimatedProgress
            self.progress.isHidden = wv.estimatedProgress >= 1
        }
        titleObs = webView.observe(\.title, options: [.new]) { [weak self] wv, _ in
            let t = (wv.title ?? "").isEmpty ? "Meshy" : (wv.title ?? "Meshy")
            self?.window?.title = t
        }
        if Settings.defaults.object(forKey: "MeshyMainFrame") == nil { window.center() }
    }

    required init?(coder: NSCoder) { fatalError("not used") }

    // MARK: navigation helpers

    func load(path: String) {
        if Settings.lastPath == nil { Settings.lastPath = path }
        let base = Settings.baseURL.trimmingCharacters(in: CharacterSet(charactersIn: "/"))
        guard let url = URL(string: base + path) else { return }
        webView.load(URLRequest(url: url))
    }

    func openOrgPage(_ subpath: String) { load(path: "/org/\(Settings.org)\(subpath)") }

    func restoreOrHome() {
        if let p = Settings.lastPath, p.hasPrefix("/org/\(Settings.org)") { load(path: p) } else { openOrgPage("") }
    }

    func openInBrowser(_ url: URL? = nil) {
        if let u = url ?? webView.url { NSWorkspace.shared.open(u) }
    }

    private func isInternal(_ url: URL) -> Bool {
        guard let h = url.host else { return url.scheme == "about" || url.scheme == "data" || url.scheme == "blob" }
        return h == Settings.host
    }

    // MARK: WKNavigationDelegate

    func webView(_ webView: WKWebView, decidePolicyFor action: WKNavigationAction, decisionHandler: @escaping (WKNavigationActionPolicy) -> Void) {
        guard let url = action.request.url else { return decisionHandler(.allow) }
        if action.targetFrame?.isMainFrame == false { return decisionHandler(.allow) } // iframes (e.g. RPC widgets)
        if isInternal(url) { return decisionHandler(.allow) }
        if ["http", "https", "mailto"].contains(url.scheme ?? "") { NSWorkspace.shared.open(url) }
        decisionHandler(.cancel)
    }

    func windowWillClose(_ notification: Notification) {
        NSApp.setActivationPolicy(.accessory) // back to menu-bar only
    }

    func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) {
        if let u = webView.url, isInternal(u) {
            Settings.lastPath = u.path + (u.query.map { "?" + $0 } ?? "")
        }
        snapshotIfAsked()
    }

    // `Meshy --snapshot out.png [--wait seconds]`: render the loaded page to a PNG and quit (for CI/QA).
    private var snapshotDone = false
    private func snapshotIfAsked() {
        let args = CommandLine.arguments
        guard !snapshotDone, let i = args.firstIndex(of: "--snapshot"), i + 1 < args.count else { return }
        snapshotDone = true
        let out = URL(fileURLWithPath: args[i + 1])
        let wait = args.firstIndex(of: "--wait").flatMap { $0 + 1 < args.count ? Double(args[$0 + 1]) : nil } ?? 8
        DispatchQueue.main.asyncAfter(deadline: .now() + wait) { [weak self] in
            self?.webView.takeSnapshot(with: nil) { image, _ in
                if let tiff = image?.tiffRepresentation, let rep = NSBitmapImageRep(data: tiff), let png = rep.representation(using: .png, properties: [:]) {
                    try? png.write(to: out)
                }
                NSApp.terminate(nil)
            }
        }
    }

    func webView(_ webView: WKWebView, didFailProvisionalNavigation navigation: WKNavigation!, withError error: Error) {
        showOffline(error)
    }

    func webView(_ webView: WKWebView, didFail navigation: WKNavigation!, withError error: Error) {
        if (error as NSError).code != NSURLErrorCancelled { showOffline(error) }
    }

    private func showOffline(_ error: Error) {
        let e = (error as NSError)
        if e.code == NSURLErrorCancelled { return }
        let msg = e.localizedDescription.replacingOccurrences(of: "<", with: "&lt;")
        let html = """
        <!doctype html><meta charset="utf-8"><title>Meshy · offline</title>
        <style>html,body{margin:0;height:100%;background:#050b24;color:#e8edff;font:15px -apple-system,system-ui}
        .c{height:100%;display:grid;place-items:center;text-align:center}
        h1{font-weight:600;letter-spacing:-.02em;margin:0 0 8px}p{color:#93a0c8;margin:0 0 20px}
        button{background:#fff;color:#1e3fd8;border:0;border-radius:999px;padding:10px 18px;font-weight:600;font-size:14px;cursor:pointer}
        code{color:#a5d8ff}</style>
        <div class="c"><div><h1>Can't reach Meshy</h1><p>\(msg)<br><code>\(Settings.baseURL)</code></p>
        <button onclick="window.webkit.messageHandlers.meshy.postMessage({type:'retry'})">Try again</button></div></div>
        """
        webView.loadHTMLString(html, baseURL: nil)
    }

    // MARK: WKUIDelegate

    // target=_blank and window.open: internal → same window, external → default browser.
    func webView(_ webView: WKWebView, createWebViewWith configuration: WKWebViewConfiguration, for action: WKNavigationAction, windowFeatures: WKWindowFeatures) -> WKWebView? {
        if let url = action.request.url {
            if isInternal(url) { webView.load(action.request) } else { NSWorkspace.shared.open(url) }
        }
        return nil
    }

    func webView(_ webView: WKWebView, runJavaScriptAlertPanelWithMessage message: String, initiatedByFrame frame: WKFrameInfo, completionHandler: @escaping () -> Void) {
        let a = NSAlert(); a.messageText = message; a.runModal(); completionHandler()
    }

    func webView(_ webView: WKWebView, runJavaScriptConfirmPanelWithMessage message: String, initiatedByFrame frame: WKFrameInfo, completionHandler: @escaping (Bool) -> Void) {
        let a = NSAlert(); a.messageText = message
        a.addButton(withTitle: "OK"); a.addButton(withTitle: "Cancel")
        completionHandler(a.runModal() == .alertFirstButtonReturn)
    }

    // MARK: WKScriptMessageHandler

    func userContentController(_ ucc: WKUserContentController, didReceive message: WKScriptMessage) {
        guard let body = message.body as? [String: Any], let type = body["type"] as? String else { return }
        switch type {
        case "retry": restoreOrHome()
        case "wallet":
            let a = NSAlert()
            a.messageText = "Connect your wallet in the browser"
            a.informativeText = "Signing grants and revokes needs a wallet extension such as MetaMask, which only runs in a browser. Meshy for Mac stays read-only. Open this page in your default browser?"
            a.addButton(withTitle: "Open in Browser")
            a.addButton(withTitle: "Stay Read-only")
            if a.runModal() == .alertFirstButtonReturn {
                openInBrowser(URL(string: body["url"] as? String ?? "") ?? webView.url)
            }
        default: break
        }
    }

    // MARK: actions (menu)

    @objc func goPage(_ sender: NSMenuItem) { openOrgPage(pages[sender.tag].path) }
    @objc func reload(_ sender: Any?) { webView.reload() }
    @objc func hardReload(_ sender: Any?) { webView.reloadFromOrigin() }
    @objc func back(_ sender: Any?) { webView.goBack() }
    @objc func forward(_ sender: Any?) { webView.goForward() }
    @objc func zoomIn(_ sender: Any?) { webView.pageZoom = min(2, webView.pageZoom + 0.1) }
    @objc func zoomOut(_ sender: Any?) { webView.pageZoom = max(0.5, webView.pageZoom - 0.1) }
    @objc func zoomReset(_ sender: Any?) { webView.pageZoom = 1 }
    @objc func openCurrentInBrowser(_ sender: Any?) { openInBrowser() }
    @objc func copyLink(_ sender: Any?) {
        guard let u = webView.url else { return }
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(u.absoluteString, forType: .string)
    }

    @objc func switchOrg(_ sender: Any?) {
        let a = NSAlert()
        a.messageText = "Open an org"
        a.informativeText = "Type the org's ENS name."
        let field = NSTextField(frame: NSRect(x: 0, y: 0, width: 280, height: 24))
        field.stringValue = Settings.org
        field.placeholderString = "your-org.eth"
        a.accessoryView = field
        a.addButton(withTitle: "Open"); a.addButton(withTitle: "Cancel")
        a.window.initialFirstResponder = field
        guard a.runModal() == .alertFirstButtonReturn else { return }
        let org = field.stringValue.trimmingCharacters(in: .whitespaces).lowercased()
        guard org.contains("."), !org.contains("/") else { return }
        MeshyStore.shared.addOrg(org) // the menu bar follows; the console reloads via .meshyOrgChanged
    }

    @objc func useProduction(_ sender: Any?) { Settings.baseURL = Settings.productionURL; restoreOrHome() }
    @objc func useClassic(_ sender: Any?) { Settings.baseURL = Settings.classicURL; restoreOrHome() }
    @objc func useLocal(_ sender: Any?) { Settings.baseURL = Settings.localURL; restoreOrHome() }
    @objc func useCustom(_ sender: Any?) {
        let a = NSAlert()
        a.messageText = "Dashboard URL"
        let field = NSTextField(frame: NSRect(x: 0, y: 0, width: 320, height: 24))
        field.stringValue = Settings.baseURL
        a.accessoryView = field
        a.addButton(withTitle: "Use"); a.addButton(withTitle: "Cancel")
        guard a.runModal() == .alertFirstButtonReturn, let u = URL(string: field.stringValue), u.scheme?.hasPrefix("http") == true else { return }
        Settings.baseURL = field.stringValue
        restoreOrHome()
    }

    func validate(_ item: NSMenuItem) -> Bool {
        switch item.action {
        case #selector(back(_:)): return webView.canGoBack
        case #selector(forward(_:)): return webView.canGoForward
        case #selector(useProduction(_:)): item.state = Settings.baseURL == Settings.productionURL ? .on : .off
        case #selector(useClassic(_:)): item.state = Settings.baseURL == Settings.classicURL ? .on : .off
        case #selector(useLocal(_:)): item.state = Settings.baseURL == Settings.localURL ? .on : .off
        default: break
        }
        return true
    }
}

extension BrowserController: NSMenuItemValidation {
    func validateMenuItem(_ item: NSMenuItem) -> Bool { validate(item) }
}

/// Avoids a retain cycle between WKUserContentController and the controller.
final class WeakHandler: NSObject, WKScriptMessageHandler {
    weak var target: WKScriptMessageHandler?
    init(_ t: WKScriptMessageHandler) { target = t }
    func userContentController(_ c: WKUserContentController, didReceive m: WKScriptMessage) { target?.userContentController(c, didReceive: m) }
}

// MARK: - App delegate and menus

final class AppDelegate: NSObject, NSApplicationDelegate {
    var controller: BrowserController!
    var status: StatusMenu!
    private var consoleLoaded = false

    func applicationDidFinishLaunching(_ note: Notification) {
        controller = BrowserController()
        NSApp.mainMenu = buildMenu()
        status = StatusMenu()
        status.showConsole = { [weak self] in self?.showConsole() }
        status.openDashboard = { [weak self] p in self?.openDashboard(p) }
        NotificationCenter.default.addObserver(forName: .meshyOrgChanged, object: nil, queue: .main) { [weak self] _ in
            guard let self, self.controller.window?.isVisible == true else { return }
            self.controller.openOrgPage("")
        }

        let args = CommandLine.arguments
        if args.contains("--dump-menu") { return dumpMenuAndExit(args) }

        MeshyStore.shared.start()
        if MeshyStore.shared.notificationsOn { MeshyStore.shared.requestNotificationAuth() }
        let first = !UserDefaults.standard.bool(forKey: "launchedBefore")
        UserDefaults.standard.set(true, forKey: "launchedBefore")
        if first || args.contains("--snapshot") || args.contains("--console") { showConsole() }
    }

    /// The Admin Console window. Showing it adds a Dock icon; closing it returns to the menu bar.
    func showConsole() {
        NSApp.setActivationPolicy(.regular)
        controller.showWindow(nil)
        controller.window?.makeKeyAndOrderFront(nil)
        if !consoleLoaded {
            consoleLoaded = true
            controller.restoreOrHome()
        }
        NSApp.activate(ignoringOtherApps: true)
    }

    func openDashboard(_ path: String) {
        if MeshyStore.shared.openInBrowser {
            let base = Settings.baseURL.trimmingCharacters(in: CharacterSet(charactersIn: "/"))
            if let u = URL(string: base + path) { NSWorkspace.shared.open(u) }
            return
        }
        consoleLoaded = true
        showConsole()
        controller.load(path: path)
    }

    /// `Meshy --dump-menu [--fixture map.json]`: build the menu from live data (or a fixture), print it, quit.
    private func dumpMenuAndExit(_ args: [String]) {
        let store = MeshyStore.shared
        // --begin-signin prints the URL the browser would open; --callback feeds a meshy://signin link.
        if args.contains("--begin-signin"), let u = store.beginSignIn(dashboard: Settings.baseURL) { print("signin-url: \(u.absoluteString)") }
        if let i = args.firstIndex(of: "--callback"), i + 1 < args.count, let u = URL(string: args[i + 1]) {
            print("signin: \(store.completeSignIn(u) ? "accepted" : "rejected")")
        }
        if let i = args.firstIndex(of: "--fixture"), i + 1 < args.count {
            guard store.loadFixture(args[i + 1]) else { print("fixture unreadable"); exit(2) }
            print(status.dump())
            exit(0)
        }
        store.refresh(force: true)
        let started = Date()
        Timer.scheduledTimer(withTimeInterval: 0.5, repeats: true) { [weak self] t in
            guard let self else { return }
            let waitedEvents = Date().timeIntervalSince(started) > 6 || !store.events.isEmpty
            if (!store.fetching && store.mapAt != nil && waitedEvents) || Date().timeIntervalSince(started) > 40 {
                t.invalidate()
                print(self.status.dump())
                exit(store.map == nil ? 1 : 0)
            }
        }
    }

    func applicationShouldTerminateAfterLastWindowClosed(_ app: NSApplication) -> Bool { false }

    func applicationShouldHandleReopen(_ app: NSApplication, hasVisibleWindows: Bool) -> Bool {
        if !hasVisibleWindows { showConsole() }
        return true
    }

    // meshy://signin?… (browser sign-in callback) and meshy://org/<name>[/page] deep links
    func application(_ app: NSApplication, open urls: [URL]) {
        guard let u = urls.first, u.scheme == "meshy" else { return }
        if u.host == "signin" {
            if MeshyStore.shared.completeSignIn(u) {
                status.item.button?.performClick(nil) // show who you are now
            } else {
                NSApp.activate(ignoringOtherApps: true)
                let a = NSAlert()
                a.messageText = "That sign-in link has expired or wasn't started from this Mac."
                a.informativeText = "Choose Sign In… in the Meshy menu and try again."
                a.runModal()
            }
            return
        }
        let parts = ([u.host ?? ""] + u.pathComponents.filter { $0 != "/" }).filter { !$0.isEmpty }
        if parts.first == "org", parts.count >= 2 {
            MeshyStore.shared.addOrg(parts[1])
            showConsole()
            controller.openOrgPage(parts.count > 2 ? "/" + parts[2...].joined(separator: "/") : "")
        } else {
            showConsole()
        }
    }

    private func item(_ title: String, _ action: Selector?, _ key: String = "", _ mods: NSEvent.ModifierFlags = .command, target: AnyObject? = nil) -> NSMenuItem {
        let i = NSMenuItem(title: title, action: action, keyEquivalent: key)
        i.keyEquivalentModifierMask = mods
        i.target = target
        return i
    }

    private func buildMenu() -> NSMenu {
        let c = controller!
        let main = NSMenu()

        let appMenu = NSMenu()
        appMenu.addItem(item("About Meshy", #selector(NSApplication.orderFrontStandardAboutPanel(_:)), ""))
        appMenu.addItem(.separator())
        appMenu.addItem(item("Hide Meshy", #selector(NSApplication.hide(_:)), "h"))
        appMenu.addItem(item("Hide Others", #selector(NSApplication.hideOtherApplications(_:)), "h", [.command, .option]))
        appMenu.addItem(item("Show All", #selector(NSApplication.unhideAllApplications(_:))))
        appMenu.addItem(.separator())
        appMenu.addItem(item("Quit Meshy", #selector(NSApplication.terminate(_:)), "q"))
        main.addItem(withTitle: "Meshy", action: nil, keyEquivalent: "").submenu = appMenu

        let file = NSMenu(title: "File")
        file.addItem(item("Open Org…", #selector(BrowserController.switchOrg(_:)), "o", [.command, .shift], target: c))
        file.addItem(item("Open in Browser", #selector(BrowserController.openCurrentInBrowser(_:)), "b", [.command, .shift], target: c))
        file.addItem(item("Copy Link", #selector(BrowserController.copyLink(_:)), "c", [.command, .shift], target: c))
        file.addItem(.separator())
        file.addItem(item("Close Window", #selector(NSWindow.performClose(_:)), "w"))
        main.addItem(withTitle: "File", action: nil, keyEquivalent: "").submenu = file

        let edit = NSMenu(title: "Edit")
        edit.addItem(item("Undo", Selector(("undo:")), "z"))
        edit.addItem(item("Redo", Selector(("redo:")), "z", [.command, .shift]))
        edit.addItem(.separator())
        edit.addItem(item("Cut", #selector(NSText.cut(_:)), "x"))
        edit.addItem(item("Copy", #selector(NSText.copy(_:)), "c"))
        edit.addItem(item("Paste", #selector(NSText.paste(_:)), "v"))
        edit.addItem(item("Select All", #selector(NSText.selectAll(_:)), "a"))
        main.addItem(withTitle: "Edit", action: nil, keyEquivalent: "").submenu = edit

        let view = NSMenu(title: "View")
        view.addItem(item("Reload", #selector(BrowserController.reload(_:)), "r", target: c))
        view.addItem(item("Reload from Network", #selector(BrowserController.hardReload(_:)), "r", [.command, .shift], target: c))
        view.addItem(.separator())
        view.addItem(item("Zoom In", #selector(BrowserController.zoomIn(_:)), "+", target: c))
        view.addItem(item("Zoom Out", #selector(BrowserController.zoomOut(_:)), "-", target: c))
        view.addItem(item("Actual Size", #selector(BrowserController.zoomReset(_:)), "0", target: c))
        view.addItem(.separator())
        view.addItem(item("Enter Full Screen", #selector(NSWindow.toggleFullScreen(_:)), "f", [.command, .control]))
        main.addItem(withTitle: "View", action: nil, keyEquivalent: "").submenu = view

        let go = NSMenu(title: "Go")
        go.addItem(item("Back", #selector(BrowserController.back(_:)), "[", target: c))
        go.addItem(item("Forward", #selector(BrowserController.forward(_:)), "]", target: c))
        go.addItem(.separator())
        for (i, p) in pages.enumerated() {
            let it = item(p.title, #selector(BrowserController.goPage(_:)), p.key, target: c)
            it.tag = i
            go.addItem(it)
        }
        main.addItem(withTitle: "Go", action: nil, keyEquivalent: "").submenu = go

        let dev = NSMenu(title: "Develop")
        dev.addItem(item("Production (meshy-bay.vercel.app)", #selector(BrowserController.useProduction(_:)), target: c))
        dev.addItem(item("Redesign branch build (meshy-next.vercel.app)", #selector(BrowserController.useClassic(_:)), target: c))
        dev.addItem(item("Local Dev Server (localhost:3000)", #selector(BrowserController.useLocal(_:)), target: c))
        dev.addItem(item("Custom URL…", #selector(BrowserController.useCustom(_:)), target: c))
        main.addItem(withTitle: "Develop", action: nil, keyEquivalent: "").submenu = dev

        let win = NSMenu(title: "Window")
        win.addItem(item("Minimize", #selector(NSWindow.performMiniaturize(_:)), "m"))
        win.addItem(item("Zoom", #selector(NSWindow.performZoom(_:))))
        main.addItem(withTitle: "Window", action: nil, keyEquivalent: "").submenu = win
        NSApp.windowsMenu = win

        return main
    }
}

let app = NSApplication.shared
let delegate = AppDelegate()
app.delegate = delegate
app.run()
