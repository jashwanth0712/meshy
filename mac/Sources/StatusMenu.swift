// The menu-bar menu (NSStatusItem). Layout follows docs/mac/PLAN.md §1.
import AppKit
import ServiceManagement

final class StatusMenu: NSObject, NSMenuDelegate, NSSearchFieldDelegate {
    let store = MeshyStore.shared
    let item = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
    let root = NSMenu()
    /// Opens a dashboard path (e.g. "/org/x/directory?n=…") in the Admin Console or the browser.
    var openDashboard: (String) -> Void = { _ in }
    var showConsole: () -> Void = {}

    private var countdowns: [(NSMenuItem, MapGrant)] = []
    private var tick: Timer?
    private var networksMenu: NSMenu?
    private var query = ""
    private var settingsWindow: NSWindow?

    override init() {
        super.init()
        if let b = item.button {
            let img = NSImage(named: "StatusIconTemplate")
            img?.isTemplate = true
            img?.size = NSSize(width: 18, height: 18)
            b.image = img
            b.imagePosition = .imageLeading
            if img == nil { b.title = "Meshy" }
        }
        root.delegate = self
        root.autoenablesItems = false
        item.menu = root
        store.onChange = { [weak self] in self?.updateButton(); if self?.isOpen == true { self?.rebuild() } }
        updateButton()
        rebuild()
    }

    private var isOpen = false

    // MARK: NSMenuDelegate

    func menuWillOpen(_ menu: NSMenu) {
        guard menu === root else { return }
        isOpen = true
        store.loadLocalAgents()
        store.refresh(force: false)
        rebuild()
        tick = Timer(timeInterval: 1, repeats: true) { [weak self] _ in self?.updateCountdowns() }
        RunLoop.main.add(tick!, forMode: .common)
    }

    func menuDidClose(_ menu: NSMenu) {
        guard menu === root else { return }
        isOpen = false
        tick?.invalidate()
        query = ""
    }

    private func updateButton() {
        let n = store.map?.liveGrants.count ?? 0
        item.button?.title = n > 0 ? " \(n)" : ""
        item.button?.toolTip = "Meshy · \(store.org)"
    }

    private func updateCountdowns() {
        for (mi, g) in countdowns { mi.title = grantTitle(g) }
    }

    // MARK: building

    func rebuild() {
        countdowns = []
        root.removeAllItems()
        let m = store.map

        root.addItem(headerItem())
        root.addItem(label(store.statusLine, color: store.mapError != nil ? .systemOrange : .secondaryLabelColor))
        if store.address == nil {
            root.addItem(action("Sign In…", #selector(signIn)))
        }
        root.addItem(.separator())

        root.addItem(submenu("Organizations", orgsMenu()))
        let nRes = (m?.devices.count ?? 0) + (m?.services.count ?? 0)
        let net = submenu("Networks", networksMenuBuilt())
        net.badgeText(nRes)
        root.addItem(net)
        let acc = submenu("Access", accessMenu())
        acc.badgeText(m?.liveGrants.count ?? 0)
        root.addItem(acc)
        if !store.favorites.isEmpty, let m { root.addItem(submenu("Favorites", favoritesMenu(m))) }
        if !store.localAgents.isEmpty { root.addItem(submenu("This Mac", thisMacMenu())) }
        if !store.events.isEmpty { root.addItem(submenu("Recent", recentMenu())) }

        root.addItem(.separator())
        root.addItem(action("Admin Console", #selector(console), key: "0"))
        root.addItem(submenu("More", moreMenu()))
        root.addItem(.separator())
        root.addItem(action("Quit Meshy", #selector(quit), key: "q"))
    }

    private func headerItem() -> NSMenuItem {
        let mi = NSMenuItem()
        let v = NSView(frame: NSRect(x: 0, y: 0, width: 300, height: 52))
        let avatar = NSTextField(labelWithString: String(store.org.prefix(1)).uppercased())
        avatar.alignment = .center
        avatar.font = .systemFont(ofSize: 15, weight: .bold)
        avatar.textColor = .white
        avatar.wantsLayer = true
        avatar.layer?.backgroundColor = NSColor(red: 0x1e / 255, green: 0x3f / 255, blue: 0xd8 / 255, alpha: 1).cgColor
        avatar.layer?.cornerRadius = 8
        avatar.frame = NSRect(x: 14, y: 11, width: 30, height: 30)
        // vertically centre the glyph inside the tile
        let wrap = NSView(frame: avatar.frame)
        wrap.wantsLayer = true
        wrap.layer?.backgroundColor = avatar.layer?.backgroundColor
        wrap.layer?.cornerRadius = 8
        avatar.layer?.backgroundColor = nil
        avatar.frame = NSRect(x: 0, y: 5, width: 30, height: 20)
        wrap.addSubview(avatar)
        v.addSubview(wrap)

        let name = NSTextField(labelWithString: store.org)
        name.font = .systemFont(ofSize: 13, weight: .semibold)
        name.frame = NSRect(x: 54, y: 27, width: 190, height: 17)
        name.lineBreakMode = .byTruncatingTail
        v.addSubview(name)

        let who: String
        if let a = store.address {
            who = "\(shortAddr(a))" + (store.role.map { " · \($0.rawValue)" } ?? "")
        } else {
            who = "Not signed in · read-only"
        }
        let sub = NSTextField(labelWithString: who)
        sub.font = .systemFont(ofSize: 11)
        sub.textColor = .secondaryLabelColor
        sub.frame = NSRect(x: 54, y: 10, width: 190, height: 15)
        v.addSubview(sub)

        let sw = NSSwitch(frame: NSRect(x: 248, y: 14, width: 40, height: 24))
        sw.controlSize = .small
        sw.state = store.live ? .on : .off
        sw.target = self
        sw.action = #selector(toggleLive(_:))
        sw.toolTip = "Live updates and notifications"
        v.addSubview(sw)
        mi.view = v
        return mi
    }

    private func orgsMenu() -> NSMenu {
        let m = NSMenu()
        m.autoenablesItems = false
        for o in store.orgs {
            let it = action(o, #selector(pickOrg(_:)))
            it.representedObject = o
            it.state = o == store.org ? .on : .off
            if o == store.org, let map = store.map {
                it.subtitleText("\(plural(map.devices.count, "device")) · \(plural((map.agents ?? []).count, "agent"))")
            }
            m.addItem(it)
        }
        m.addItem(.separator())
        m.addItem(action("Add Organization…", #selector(addOrg)))
        if store.orgs.count > 1 {
            let r = action("Remove \(store.org)", #selector(removeOrg))
            m.addItem(r)
        }
        return m
    }

    // Networks › with a search field; typing flattens everything into matches.
    private func networksMenuBuilt() -> NSMenu {
        let m = NSMenu()
        m.autoenablesItems = false
        networksMenu = m
        fillNetworks(m)
        return m
    }

    private func fillNetworks(_ m: NSMenu) {
        m.removeAllItems()
        let search = NSMenuItem()
        let field = NSSearchField(frame: NSRect(x: 14, y: 4, width: 252, height: 24))
        field.placeholderString = "Search names"
        field.stringValue = query
        field.delegate = self
        field.sendsSearchStringImmediately = true
        let box = NSView(frame: NSRect(x: 0, y: 0, width: 280, height: 32))
        box.addSubview(field)
        search.view = box
        m.addItem(search)

        guard let map = store.map else {
            m.addItem(label(store.fetching ? "Loading…" : "No data. Check More › Troubleshoot."))
            return
        }
        let q = query.lowercased()
        if !q.isEmpty {
            var hits: [NSMenuItem] = []
            for d in map.devices where d.fullName.lowercased().contains(q) || d.summary.lowercased().contains(q) { hits.append(deviceItem(d, title: short(d.fullName))) }
            for s in map.services where s.fullName.lowercased().contains(q) || s.desc.lowercased().contains(q) { hits.append(serviceItem(s, title: short(s.fullName))) }
            m.addItem(label(hits.isEmpty ? "No matches" : "\(hits.count) match\(hits.count == 1 ? "" : "es")"))
            for h in hits.prefix(40) { m.addItem(h) }
            if hits.count > 40 { m.addItem(action("Show all in Admin Console", #selector(openDirectory))) }
            return
        }
        let total = map.devices.count + map.services.count
        m.addItem(label("\(total) resource\(total == 1 ? "" : "s") in \(map.allNetworks.count) network\(map.allNetworks.count == 1 ? "" : "s")"))
        if map.allNetworks.isEmpty { m.addItem(action("Add a network in Admin Console", #selector(openDirectory))) }
        var shown = 0
        for n in map.allNetworks {
            m.addItem(.separator())
            m.addItem(sectionHeader(n.name))
            for d in n.resources ?? [] {
                if shown >= 40 { break }
                m.addItem(deviceItem(d, title: d.name))
                shown += 1
                for s in d.services ?? [] {
                    let it = serviceItem(s, title: s.name)
                    it.indentationLevel = 1
                    m.addItem(it)
                }
            }
            if (n.resources ?? []).isEmpty { m.addItem(label("No devices")) }
        }
        if map.devices.count > 40 { m.addItem(action("Show all \(map.devices.count) in Admin Console", #selector(openDirectory))) }
    }

    func controlTextDidChange(_ obj: Notification) {
        guard let f = obj.object as? NSSearchField, let m = networksMenu else { return }
        query = f.stringValue
        // Rebuild the rows under the field, keeping the field (and its focus) in place.
        while m.items.count > 1 { m.removeItem(at: 1) }
        let tmp = NSMenu()
        fillNetworks(tmp)
        for it in tmp.items.dropFirst() {
            tmp.removeItem(it)
            m.addItem(it)
        }
    }

    private func deviceItem(_ d: MapResource, title: String) -> NSMenuItem {
        let it = NSMenuItem(title: title, action: nil, keyEquivalent: "")
        if !d.summary.isEmpty { it.subtitleText(d.summary) }
        if store.isFavorite(d.fullName) { it.title = "★ " + title }
        let sm = NSMenu()
        sm.autoenablesItems = false
        sm.addItem(label(d.fullName))
        if !d.summary.isEmpty { sm.addItem(label(d.summary)) }
        if let r = d.rootList.first { sm.addItem(label("Shares " + tilde(r))) }
        sm.addItem(.separator())
        sm.addItem(copyItem("Copy “meshy ssh \(short(d.fullName))”", "meshy ssh \(d.fullName)"))
        if let a = d.addr { sm.addItem(copyItem("Copy Device Address", a)) }
        if let e = d.endpoint, !e.isEmpty { sm.addItem(copyItem("Copy Endpoint (\(e))", e)) }
        sm.addItem(.separator())
        sm.addItem(open("Open in Admin Console", "/org/\(store.org)/directory?n=\(d.fullName)"))
        sm.addItem(open("Give Access…", "/org/\(store.org)?give=&target=\(d.fullName)"))
        sm.addItem(favItem(d.fullName))
        if let svcs = d.services, !svcs.isEmpty {
            sm.addItem(.separator())
            sm.addItem(sectionHeader("Services"))
            for s in svcs { sm.addItem(serviceItem(s, title: s.name)) }
        }
        it.submenu = sm
        return it
    }

    private func serviceItem(_ s: MapService, title: String) -> NSMenuItem {
        let it = NSMenuItem(title: (store.isFavorite(s.fullName) ? "★ " : "") + title, action: nil, keyEquivalent: "")
        it.subtitleText([s.proto, s.desc].filter { !$0.isEmpty }.joined(separator: " · "))
        let sm = NSMenu()
        sm.autoenablesItems = false
        sm.addItem(label(s.fullName))
        sm.addItem(label("\(s.proto) → \(s.target)"))
        if !s.desc.isEmpty { sm.addItem(label(s.desc)) }
        sm.addItem(.separator())
        sm.addItem(copyItem("Copy “meshy connect \(short(s.fullName))”", "meshy connect \(s.fullName)"))
        if !s.target.isEmpty { sm.addItem(copyItem("Copy Target (\(s.target))", s.target)) }
        sm.addItem(.separator())
        sm.addItem(open("Open in Admin Console", "/org/\(store.org)/directory?n=\(s.fullName)"))
        sm.addItem(open("Give Access…", "/org/\(store.org)?give=&target=\(s.fullName)"))
        sm.addItem(favItem(s.fullName))
        it.submenu = sm
        return it
    }

    private func grantTitle(_ g: MapGrant) -> String {
        "\(g.agentLabel) → \(short(g.target ?? g.fullName)) · \(left(g.expiry))"
    }

    private func accessMenu() -> NSMenu {
        let m = NSMenu()
        m.autoenablesItems = false
        guard let map = store.map else {
            m.addItem(label(store.fetching ? "Loading…" : "No data"))
            return m
        }
        let live = map.liveGrants
        if live.isEmpty { m.addItem(label("No agent can reach anything right now")) }
        let me = store.address?.lowercased()
        for g in live {
            let it = NSMenuItem(title: grantTitle(g), action: nil, keyEquivalent: "")
            if let me, g.owner?.lowercased() == me { it.subtitleText("yours") }
            countdowns.append((it, g))
            let sm = NSMenu()
            sm.autoenablesItems = false
            sm.addItem(label(g.fullName))
            let until = DateFormatter.localizedString(from: Date(timeIntervalSince1970: g.expiry), dateStyle: .none, timeStyle: .short)
            sm.addItem(label("\(g.isService ? "One port" : "Folders") · expires \(until)"))
            sm.addItem(.separator())
            if let t = g.target {
                sm.addItem(g.isService ? copyItem("Copy “meshy connect \(short(t))”", "meshy connect \(t)") : copyItem("Copy “meshy ssh \(short(t))”", "meshy ssh \(t)"))
            }
            sm.addItem(open("Open in Admin Console", "/org/\(store.org)"))
            sm.addItem(.separator())
            sm.addItem(open("Renew +1h…", "/org/\(store.org)?give=\(g.agentLabel)&target=\(g.target ?? "")"))
            let rv = open("Revoke…", "/org/\(store.org)?revoke=\(g.fullName)")
            sm.addItem(rv)
            it.submenu = sm
            m.addItem(it)
        }
        m.addItem(.separator())
        let ended = map.endedGrants.count
        if ended > 0 { m.addItem(open("\(ended) ended", "/org/\(store.org)")) }
        m.addItem(open("Give Access…", "/org/\(store.org)?give="))
        return m
    }

    private func favoritesMenu(_ map: OrgMap) -> NSMenu {
        let m = NSMenu()
        m.autoenablesItems = false
        for f in store.favorites {
            if let d = map.devices.first(where: { $0.fullName == f }) { m.addItem(deviceItem(d, title: short(d.fullName))) }
            else if let s = map.services.first(where: { $0.fullName == f }) { m.addItem(serviceItem(s, title: short(s.fullName))) }
        }
        if m.items.isEmpty { m.addItem(label("Favorites from another org")) }
        return m
    }

    private func thisMacMenu() -> NSMenu {
        let m = NSMenu()
        m.autoenablesItems = false
        m.addItem(sectionHeader("Agents on this Mac"))
        for a in store.localAgents {
            let it = NSMenuItem(title: "\(a.agent) · \(a.org)", action: nil, keyEquivalent: "")
            it.subtitleText(shortAddr(a.addr) + (a.org == store.org ? "" : " · other org"))
            let sm = NSMenu()
            sm.autoenablesItems = false
            sm.addItem(label("\(a.agent).agents.\(a.org)"))
            let mine = (store.map?.liveGrants ?? []).filter { $0.agentLabel == a.agent && a.org == store.org }
            sm.addItem(label(mine.isEmpty ? "No live access" : "\(mine.count) live grant\(mine.count == 1 ? "" : "s")"))
            for g in mine {
                if let t = g.target { sm.addItem(g.isService ? copyItem("Copy “meshy connect \(short(t))”", "meshy connect \(t)") : copyItem("Copy “meshy ssh \(short(t))”", "meshy ssh \(t)")) }
            }
            sm.addItem(.separator())
            sm.addItem(copyItem("Copy Address", a.addr))
            sm.addItem(copyItem("Copy “meshy whoami”", "meshy whoami"))
            it.submenu = sm
            m.addItem(it)
        }
        return m
    }

    private func recentMenu() -> NSMenu {
        let m = NSMenu()
        m.autoenablesItems = false
        for e in store.events {
            let it = open(e.title, "/org/\(store.org)/activity")
            it.subtitleText(ago(Date(timeIntervalSince1970: e.ts)))
            m.addItem(it)
        }
        m.addItem(.separator())
        m.addItem(open("Open Activity", "/org/\(store.org)/activity"))
        return m
    }

    private func moreMenu() -> NSMenu {
        let m = NSMenu()
        m.autoenablesItems = false
        m.addItem(sectionHeader("Account"))
        if let a = store.address {
            m.addItem(copyItem("Signed in as \(shortAddr(a))" + (store.role.map { " · \($0.rawValue)" } ?? ""), a))
            m.addItem(action("Log Out", #selector(logOut)))
        } else {
            m.addItem(action("Sign In…", #selector(signIn)))
            m.addItem(action("Use an Address…", #selector(useAddress)))
        }
        m.addItem(sectionHeader("App"))
        m.addItem(action("Settings…", #selector(openSettings), key: ","))
        let login = action("Start at Login", #selector(toggleLogin))
        login.state = SMAppService.mainApp.status == .enabled ? .on : .off
        m.addItem(login)
        let notif = action("Notifications", #selector(toggleNotifications))
        notif.state = store.notificationsOn ? .on : .off
        m.addItem(notif)
        m.addItem(sectionHeader("Help"))
        m.addItem(link("How Meshy Works", "https://github.com/jashwanth0712/meshy#readme"))
        m.addItem(link("Documentation", "https://github.com/jashwanth0712/meshy/tree/main/docs"))
        m.addItem(open("Download the CLI", "/download"))
        let tr = NSMenu()
        tr.autoenablesItems = false
        tr.addItem(action("Refresh Now", #selector(refreshNow), key: "r"))
        tr.addItem(action("Run meshyd doctor", #selector(doctor)))
        tr.addItem(action("Show meshyd Logs", #selector(logs)))
        tr.addItem(action("Copy Diagnostics", #selector(diagnostics)))
        m.addItem(submenu("Troubleshoot", tr))
        m.addItem(.separator())
        m.addItem(label("Meshy \(appVersion) · ENSv2 on Sepolia · Arkiv"))
        return m
    }

    // MARK: item helpers

    private func action(_ title: String, _ sel: Selector, key: String = "") -> NSMenuItem {
        let it = NSMenuItem(title: title, action: sel, keyEquivalent: key)
        it.target = self
        return it
    }

    private func label(_ t: String, color: NSColor = .secondaryLabelColor) -> NSMenuItem {
        let it = NSMenuItem(title: t, action: nil, keyEquivalent: "")
        it.isEnabled = false
        it.attributedTitle = NSAttributedString(string: t, attributes: [.foregroundColor: color, .font: NSFont.menuFont(ofSize: 12)])
        return it
    }

    private func sectionHeader(_ t: String) -> NSMenuItem {
        if #available(macOS 14.0, *) { return NSMenuItem.sectionHeader(title: t) }
        return label(t.uppercased())
    }

    private func submenu(_ title: String, _ m: NSMenu) -> NSMenuItem {
        let it = NSMenuItem(title: title, action: nil, keyEquivalent: "")
        it.submenu = m
        return it
    }

    private func copyItem(_ title: String, _ value: String) -> NSMenuItem {
        let it = action(title, #selector(copyValue(_:)))
        it.representedObject = value
        return it
    }

    private func open(_ title: String, _ path: String) -> NSMenuItem {
        let it = action(title, #selector(openPath(_:)))
        it.representedObject = path
        return it
    }

    private func link(_ title: String, _ url: String) -> NSMenuItem {
        let it = action(title, #selector(openURL(_:)))
        it.representedObject = url
        return it
    }

    private func favItem(_ name: String) -> NSMenuItem {
        let it = action(store.isFavorite(name) ? "Remove from Favorites" : "Add to Favorites", #selector(toggleFav(_:)))
        it.representedObject = name
        return it
    }

    private func short(_ name: String) -> String { name.replacingOccurrences(of: ".\(store.org)", with: "") }
    private func tilde(_ p: String) -> String {
        p.replacingOccurrences(of: "^/Users/[^/]+", with: "~", options: .regularExpression).replacingOccurrences(of: "^/home/[^/]+", with: "~", options: .regularExpression)
    }

    // MARK: actions

    @objc func toggleLive(_ s: NSSwitch) { store.live = s.state == .on }
    @objc func console() { showConsole() }
    @objc func quit() { NSApp.terminate(nil) }
    @objc func refreshNow() { store.refresh(force: true) }
    @objc func copyValue(_ s: NSMenuItem) {
        guard let v = s.representedObject as? String else { return }
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(v, forType: .string)
    }
    @objc func openPath(_ s: NSMenuItem) { if let p = s.representedObject as? String { openDashboard(p) } }
    @objc func openURL(_ s: NSMenuItem) { if let u = (s.representedObject as? String).flatMap(URL.init(string:)) { NSWorkspace.shared.open(u) } }
    @objc func openDirectory() { openDashboard("/org/\(store.org)/directory") }
    @objc func toggleFav(_ s: NSMenuItem) { if let n = s.representedObject as? String { store.toggleFavorite(n) } }
    @objc func pickOrg(_ s: NSMenuItem) { if let o = s.representedObject as? String { store.switchOrg(o) } }
    @objc func removeOrg() { store.removeOrg(store.org) }
    @objc func toggleNotifications() { store.notificationsOn.toggle() }
    @objc func logOut() { store.logOut() }

    @objc func signIn() {
        guard let u = store.beginSignIn(dashboard: Settings.baseURL) else { return }
        NSWorkspace.shared.open(u) // the wallet lives in the browser
    }

    @objc func addOrg() {
        guard let o = prompt("Add an organization", info: "Type the org's ENS name.", placeholder: "your-org.eth") else { return }
        let org = o.trimmingCharacters(in: .whitespaces).lowercased()
        guard org.contains("."), !org.contains("/"), !org.contains(" ") else { return }
        store.addOrg(org)
    }

    @objc func useAddress() {
        guard let a = prompt("Use an address", info: "ENS is public, so the menu can show your role and access from just your address. Writes still need your wallet.", placeholder: "0x…") else { return }
        if !store.useAddress(a.trimmingCharacters(in: .whitespaces)) { alert("That isn't an Ethereum address.") }
    }

    @objc func toggleLogin() {
        let svc = SMAppService.mainApp
        do {
            if svc.status == .enabled { try svc.unregister() } else { try svc.register() }
        } catch {
            alert("Couldn't change Start at Login: \(error.localizedDescription). Move Meshy to /Applications and try again.")
        }
    }

    @objc func doctor() { runInTerminal("doctor") }
    @objc func logs() { runInTerminal("logs -f") }

    private func meshydPath() -> String? {
        let home = FileManager.default.homeDirectoryForCurrentUser.path
        return ["\(home)/.local/bin/meshyd", "/opt/homebrew/bin/meshyd", "/usr/local/bin/meshyd", "\(home)/go/bin/meshyd"].first { FileManager.default.isExecutableFile(atPath: $0) }
    }

    private func runInTerminal(_ args: String) {
        guard let bin = meshydPath() else {
            alert("meshyd isn't installed on this Mac. Opening the download page.")
            openDashboard("/download")
            return
        }
        let script = FileManager.default.temporaryDirectory.appendingPathComponent("meshy-\(args.components(separatedBy: " ")[0]).command")
        let body = "#!/bin/zsh\nclear\necho '› meshyd \(args)'\n'\(bin)' \(args)\necho\nread -k1 '?Press any key to close'\n"
        do {
            try body.write(to: script, atomically: true, encoding: .utf8)
            try FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: script.path)
            NSWorkspace.shared.open(script)
        } catch {
            alert(error.localizedDescription)
        }
    }

    @objc func diagnostics() {
        let m = store.map
        let lines = [
            "Meshy for Mac \(appVersion), macOS \(ProcessInfo.processInfo.operatingSystemVersionString)",
            "org: \(store.org)",
            "provider: \(store.syncURL)",
            "dashboard: \(Settings.baseURL)",
            "status: \(store.statusLine)",
            "error: \(store.mapError ?? "none")",
            "block: \(m?.block.map(String.init) ?? "-")",
            "devices: \(m?.devices.count ?? 0), services: \(m?.services.count ?? 0), agents: \(m?.agents?.count ?? 0), live grants: \(m?.liveGrants.count ?? 0)",
            "signed in: \(store.address ?? "no")",
            "local agents: \(store.localAgents.map { "\($0.agent)@\($0.org)" }.joined(separator: ", "))",
            "meshyd: \(meshydPath() ?? "not installed")",
        ]
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(lines.joined(separator: "\n"), forType: .string)
    }

    @objc func openSettings() {
        if let w = settingsWindow { NSApp.activate(ignoringOtherApps: true); w.makeKeyAndOrderFront(nil); return }
        let w = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 460, height: 210), styleMask: [.titled, .closable], backing: .buffered, defer: false)
        w.title = "Meshy Settings"
        w.isReleasedWhenClosed = false
        let sync = NSTextField(string: store.syncURL)
        let dash = NSTextField(string: Settings.baseURL)
        let openIn = NSPopUpButton()
        openIn.addItems(withTitles: ["Admin Console window", "Default browser"])
        openIn.selectItem(at: store.openInBrowser ? 1 : 0)
        let every = NSPopUpButton()
        every.addItems(withTitles: ["15 seconds", "30 seconds", "1 minute", "5 minutes"])
        let secs: [Double] = [15, 30, 60, 300]
        every.selectItem(at: secs.firstIndex(of: store.refreshSeconds) ?? 1)
        let grid = NSGridView(views: [
            [NSTextField(labelWithString: "Provider API"), sync],
            [NSTextField(labelWithString: "Dashboard"), dash],
            [NSTextField(labelWithString: "Open dashboard links in"), openIn],
            [NSTextField(labelWithString: "Refresh every"), every],
        ])
        grid.column(at: 0).xPlacement = .trailing
        grid.rowSpacing = 10
        grid.columnSpacing = 10
        sync.widthAnchor.constraint(equalToConstant: 260).isActive = true
        let save = NSButton(title: "Save", target: nil, action: nil)
        save.keyEquivalent = "\r"
        let stack = NSStackView(views: [grid, save])
        stack.orientation = .vertical
        stack.alignment = .trailing
        stack.edgeInsets = NSEdgeInsets(top: 20, left: 20, bottom: 20, right: 20)
        w.contentView = stack
        let handler = ClosureTarget { [weak self, weak w] in
            guard let self else { return }
            self.store.syncURL = sync.stringValue.trimmingCharacters(in: .whitespaces)
            if dash.stringValue.hasPrefix("http") { Settings.baseURL = dash.stringValue.trimmingCharacters(in: .whitespaces) }
            self.store.openInBrowser = openIn.indexOfSelectedItem == 1
            self.store.refreshSeconds = secs[max(0, every.indexOfSelectedItem)]
            self.store.refresh(force: true)
            w?.close()
        }
        save.target = handler
        save.action = #selector(ClosureTarget.fire)
        objc_setAssociatedObject(save, "handler", handler, .OBJC_ASSOCIATION_RETAIN)
        w.center()
        settingsWindow = w
        NSApp.activate(ignoringOtherApps: true)
        w.makeKeyAndOrderFront(nil)
    }

    private func prompt(_ title: String, info: String, placeholder: String) -> String? {
        NSApp.activate(ignoringOtherApps: true)
        let a = NSAlert()
        a.messageText = title
        a.informativeText = info
        let f = NSTextField(frame: NSRect(x: 0, y: 0, width: 280, height: 24))
        f.placeholderString = placeholder
        a.accessoryView = f
        a.addButton(withTitle: "OK")
        a.addButton(withTitle: "Cancel")
        a.window.initialFirstResponder = f
        return a.runModal() == .alertFirstButtonReturn && !f.stringValue.isEmpty ? f.stringValue : nil
    }

    private func alert(_ text: String) {
        NSApp.activate(ignoringOtherApps: true)
        let a = NSAlert()
        a.messageText = text
        a.runModal()
    }

    // MARK: headless test: print the menu tree

    func dump() -> String {
        rebuild()
        var out: [String] = []
        func walk(_ m: NSMenu, _ depth: Int) {
            for it in m.items {
                let pad = String(repeating: "  ", count: depth)
                if it.isSeparatorItem { out.append(pad + "────"); continue }
                var t = it.title
                if let v = it.view {
                    t = v.subviews.flatMap { [$0] + $0.subviews }.compactMap { ($0 as? NSTextField)?.stringValue }.filter { !$0.isEmpty }.joined(separator: " | ")
                    if let sw = v.subviews.compactMap({ $0 as? NSSwitch }).first { t += sw.state == .on ? " [Live on]" : " [Live off]" }
                    if v.subviews.contains(where: { $0 is NSSearchField }) { t = "[search field]" }
                }
                let mark = it.state == .on ? "✓ " : ""
                let dis = it.isEnabled || it.submenu != nil || it.view != nil ? "" : " (label)"
                let sub = it.subtitleValue.isEmpty ? "" : "  — \(it.subtitleValue)"
                let val = (it.representedObject as? String).map { "  ⟶ \($0)" } ?? ""
                out.append(pad + mark + t + sub + dis + val + (it.submenu != nil ? "  ›" : ""))
                if let s = it.submenu { walk(s, depth + 1) }
            }
        }
        walk(root, 0)
        return out.joined(separator: "\n")
    }
}

final class ClosureTarget: NSObject {
    let f: () -> Void
    init(_ f: @escaping () -> Void) { self.f = f }
    @objc func fire() { f() }
}

extension NSMenuItem {
    func subtitleText(_ s: String) {
        guard !s.isEmpty else { return }
        if #available(macOS 14.4, *) { subtitle = s } else { toolTip = s }
    }
    var subtitleValue: String {
        if #available(macOS 14.4, *) { return subtitle ?? "" }
        return toolTip ?? ""
    }
    func badgeText(_ n: Int) {
        guard n > 0 else { return }
        if #available(macOS 14.0, *) { badge = NSMenuItemBadge(count: n) }
    }
}
