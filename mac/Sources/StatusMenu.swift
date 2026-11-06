// The menu-bar menu (NSStatusItem). Layout follows docs/mac/PLAN.md §1.
//
//   signed out        Sign In with Ethereum… · Join with an Invite Link… · More · Quit
//   no org yet        Add Organization… · Join… · More · Quit
//   org selected      header · Organizations · Networks · Access · This Mac · Recent · Admin Console · More · Quit
import AppKit
import ServiceManagement

final class StatusMenu: NSObject, NSMenuDelegate, NSSearchFieldDelegate {
    let store = MeshyStore.shared
    let dev = DeviceManager.shared
    let item = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
    let root = NSMenu()

    private var countdowns: [(NSMenuItem, MapGrant)] = []
    private var tick: Timer?
    private var networksMenu: NSMenu?
    private var query = ""
    private var settingsWindow: NSWindow?
    private var checking: String? // org being checked on ENS
    private var isOpen = false

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
        let changed = { [weak self] in self?.updateButton(); if self?.isOpen == true { self?.rebuild() } }
        store.onChange = changed
        dev.onChange = changed
        updateButton()
        rebuild()
    }

    // MARK: NSMenuDelegate

    func menuWillOpen(_ menu: NSMenu) {
        guard menu === root else { return }
        isOpen = true
        store.loadLocalAgents()
        store.refresh(force: false)
        dev.poll()
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
        let n = store.org == nil ? 0 : myLive.count
        item.button?.title = n > 0 ? " \(n)" : ""
        item.button?.toolTip = store.org.map { "Meshy · \($0)" } ?? "Meshy"
    }

    private func updateCountdowns() {
        for (mi, g) in countdowns { mi.title = grantTitle(g) }
    }

    // MARK: building

    func rebuild() {
        countdowns = []
        root.removeAllItems()
        root.addItem(headerItem())

        guard store.address != nil else {
            root.addItem(action("Sign In with Ethereum…", #selector(signIn)))
            root.addItem(label("Your wallet signs in through the browser"))
            root.addItem(.separator())
            addJoinItems(to: root)
            finish()
            return
        }
        guard let org = store.org else {
            root.addItem(action(checking.map { "Checking \($0)…" } ?? "Add Organization…", #selector(addOrgPrompt)))
            root.addItem(label("Type your org's ENS name, like acme.eth"))
            root.addItem(.separator())
            addJoinItems(to: root)
            if dev.device != nil { root.addItem(submenu("This Mac", thisMacMenu())) }
            finish()
            return
        }

        let m = store.map
        root.addItem(label(store.statusLine, color: store.mapError != nil ? .systemOrange : .secondaryLabelColor))
        root.addItem(.separator())
        root.addItem(submenu("Organizations", orgsMenu()))
        let net = submenu("Networks", networksMenuBuilt())
        net.badgeText((m?.devices.count ?? 0) + (m?.services.count ?? 0))
        root.addItem(net)
        let acc = submenu("Access", accessMenu())
        acc.badgeText(myLive.count + store.requests.count)
        root.addItem(acc)
        if !store.favorites.isEmpty, let m { root.addItem(submenu("Favorites", favoritesMenu(m))) }
        let mac = submenu("This Mac", thisMacMenu())
        if let s = dev.statusLine { mac.subtitleText(s) }
        root.addItem(mac)
        if !store.events.isEmpty { root.addItem(submenu("Recent", recentMenu())) }
        root.addItem(.separator())
        root.addItem(open("Admin Console", "/org/\(org)", key: "0"))
        finish()
    }

    private func finish() {
        root.addItem(submenu("More", moreMenu()))
        root.addItem(.separator())
        root.addItem(action("Quit Meshy", #selector(quit), key: "q"))
    }

    private func addJoinItems(to m: NSMenu) {
        if let s = dev.joinState {
            m.addItem(label(s))
            m.addItem(action("Cancel Joining", #selector(cancelJoin)))
        } else {
            m.addItem(action("Join with an Invite Link…", #selector(joinPrompt)))
        }
    }

    private func headerItem() -> NSMenuItem {
        let mi = NSMenuItem()
        let v = NSView(frame: NSRect(x: 0, y: 0, width: 300, height: 52))
        let title = store.org ?? "Meshy"
        let tile = NSView(frame: NSRect(x: 14, y: 11, width: 30, height: 30))
        tile.wantsLayer = true
        tile.layer?.backgroundColor = (store.org == nil ? NSColor.tertiaryLabelColor : NSColor(red: 0x1e / 255, green: 0x3f / 255, blue: 0xd8 / 255, alpha: 1)).cgColor
        tile.layer?.cornerRadius = 8
        let glyph = NSTextField(labelWithString: String(title.prefix(1)).uppercased())
        glyph.alignment = .center
        glyph.font = .systemFont(ofSize: 15, weight: .bold)
        glyph.textColor = .white
        glyph.frame = NSRect(x: 0, y: 5, width: 30, height: 20)
        tile.addSubview(glyph)
        v.addSubview(tile)

        let name = NSTextField(labelWithString: title)
        name.font = .systemFont(ofSize: 13, weight: .semibold)
        name.frame = NSRect(x: 54, y: 27, width: 190, height: 17)
        name.lineBreakMode = .byTruncatingTail
        v.addSubview(name)

        let who: String
        if let a = store.address {
            who = shortAddr(a) + (store.roleLine.map { " · \($0)" } ?? "")
        } else {
            who = "Not signed in"
        }
        let sub = NSTextField(labelWithString: who)
        sub.font = .systemFont(ofSize: 11)
        sub.textColor = .secondaryLabelColor
        sub.lineBreakMode = .byTruncatingTail
        sub.frame = NSRect(x: 54, y: 10, width: 190, height: 15)
        v.addSubview(sub)

        if store.org != nil {
            let sw = NSSwitch(frame: NSRect(x: 248, y: 14, width: 40, height: 24))
            sw.controlSize = .small
            sw.state = store.live ? .on : .off
            sw.target = self
            sw.action = #selector(toggleLive(_:))
            sw.toolTip = "Live updates and notifications"
            v.addSubview(sw)
        }
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
            if let r = store.role(o) { it.subtitleText(r.role == "admin" ? "admin" : r.role) }
            m.addItem(it)
        }
        m.addItem(.separator())
        m.addItem(action(checking.map { "Checking \($0)…" } ?? "Add Organization…", #selector(addOrgPrompt)))
        if let o = store.org { m.addItem(action("Remove \(o)", #selector(removeOrg))) }
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
            if hits.count > 40 { m.addItem(open("Show all in Admin Console", dirPath())) }
            return
        }
        let total = map.devices.count + map.services.count
        m.addItem(label("\(plural(total, "resource")) in \(plural(map.allNetworks.count, "network"))"))
        if map.allNetworks.isEmpty { m.addItem(open("Add a Network in Admin Console", dirPath())) }
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
        if map.devices.count > 40 { m.addItem(open("Show all \(map.devices.count) in Admin Console", dirPath())) }
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

    private var canSign: Bool { store.org.flatMap { store.role($0) }?.canSign ?? false }

    private func deviceItem(_ d: MapResource, title: String) -> NSMenuItem {
        let it = NSMenuItem(title: (store.isFavorite(d.fullName) ? "★ " : "") + title, action: nil, keyEquivalent: "")
        if !d.summary.isEmpty { it.subtitleText(d.summary) }
        let sm = NSMenu()
        sm.autoenablesItems = false
        sm.addItem(label(d.fullName))
        if !d.summary.isEmpty { sm.addItem(label(d.summary)) }
        if !d.rootList.isEmpty { sm.addItem(label("Shares " + d.rootList.map(tilde).joined(separator: ", "))) }
        sm.addItem(.separator())
        let req = action("Request Access…", #selector(requestAccess(_:)))
        req.representedObject = d.fullName
        sm.addItem(req)
        if canSign { sm.addItem(open("Give Access…", "/org/\(org)?give=&target=\(q(d.fullName))")) }
        sm.addItem(copyItem("Copy “meshy ssh \(short(d.fullName))”", "meshy ssh \(d.fullName)"))
        if let a = d.addr { sm.addItem(copyItem("Copy Device Address", a)) }
        sm.addItem(.separator())
        sm.addItem(open("Open in Admin Console", "/org/\(org)/directory?n=\(q(d.fullName))"))
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
        let req = action("Request Access…", #selector(requestAccess(_:)))
        req.representedObject = s.fullName
        sm.addItem(req)
        if canSign { sm.addItem(open("Give Access…", "/org/\(org)?give=&target=\(q(s.fullName))")) }
        sm.addItem(copyItem("Copy “meshy connect \(short(s.fullName))”", "meshy connect \(s.fullName)"))
        sm.addItem(.separator())
        sm.addItem(open("Open in Admin Console", "/org/\(org)/directory?n=\(q(s.fullName))"))
        sm.addItem(favItem(s.fullName))
        it.submenu = sm
        return it
    }

    private func grantTitle(_ g: MapGrant) -> String {
        "\(g.agentLabel) → \(short(g.target ?? g.fullName)) · \(left(g.expiry))"
    }

    /// Live grants held by this Mac's agents (or the signed-in address).
    private var myLive: [MapGrant] {
        let mine = Set(store.agentsHere.map { $0.addr.lowercased() } + [store.address?.lowercased() ?? "-"])
        return (store.map?.liveGrants ?? []).filter { mine.contains($0.owner?.lowercased() ?? "") }
    }

    private func grantItem(_ g: MapGrant, mine: Bool) -> NSMenuItem {
        let it = NSMenuItem(title: grantTitle(g), action: nil, keyEquivalent: "")
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
        if canSign {
            sm.addItem(open("Renew…", "/org/\(org)?give=\(q(g.agentLabel))&target=\(q(g.target ?? ""))"))
            sm.addItem(open("Revoke…", "/org/\(org)?revoke=\(q(g.fullName))"))
        } else if mine, let t = g.target {
            let r = action("Ask for More Time…", #selector(requestAccess(_:)))
            r.representedObject = t
            sm.addItem(r)
        }
        it.submenu = sm
        return it
    }

    private func accessMenu() -> NSMenu {
        let m = NSMenu()
        m.autoenablesItems = false
        guard let map = store.map else {
            m.addItem(label(store.fetching ? "Loading…" : "No data"))
            return m
        }
        let mine = myLive
        let reqs = store.requests
        m.addItem(sectionHeader("Yours"))
        if mine.isEmpty && reqs.isEmpty { m.addItem(label(store.agentsHere.isEmpty ? "No agent on this Mac yet. Use This Mac › Join." : "Nothing yet. Request access from Networks.")) }
        for g in mine { m.addItem(grantItem(g, mine: true)) }
        for (i, r) in reqs.enumerated() {
            let it = NSMenuItem(title: "\(r.agent) → \(short(r.target)) · waiting for an admin", action: nil, keyEquivalent: "")
            it.subtitleText("requested \(ago(Date(timeIntervalSince1970: r.at)))")
            let sm = NSMenu()
            sm.autoenablesItems = false
            sm.addItem(copyItem("Copy Request Link", r.link))
            let e = action("Email Request…", #selector(emailRequest(_:)))
            e.representedObject = r.link
            sm.addItem(e)
            let x = action("Withdraw", #selector(withdraw(_:)))
            x.tag = i
            sm.addItem(x)
            it.submenu = sm
            m.addItem(it)
        }
        let others = map.liveGrants.filter { g in !mine.contains { $0.fullName == g.fullName } }
        if !others.isEmpty {
            m.addItem(.separator())
            m.addItem(sectionHeader("Everyone in \(org)"))
            for g in others { m.addItem(grantItem(g, mine: false)) }
        }
        m.addItem(.separator())
        if canSign { m.addItem(open("Give Access…", "/org/\(org)?give=")) }
        m.addItem(open("All Access in Admin Console", "/org/\(org)"))
        return m
    }

    private func favoritesMenu(_ map: OrgMap) -> NSMenu {
        let m = NSMenu()
        m.autoenablesItems = false
        for f in store.favorites {
            if let d = map.devices.first(where: { $0.fullName == f }) { m.addItem(deviceItem(d, title: short(d.fullName))) }
            else if let s = map.services.first(where: { $0.fullName == f }) { m.addItem(serviceItem(s, title: short(s.fullName))) }
        }
        if m.items.isEmpty { m.addItem(label("Nothing here")) }
        return m
    }

    private func thisMacMenu() -> NSMenu {
        let m = NSMenu()
        m.autoenablesItems = false
        m.addItem(sectionHeader("Shared as a device"))
        if let d = dev.device {
            m.addItem(label(d.name))
            if let s = dev.statusLine { m.addItem(label(s.components(separatedBy: " · ").dropFirst().joined(separator: " · "))) }
            m.addItem(label("Folders: " + d.roots.map(tilde).joined(separator: ", ")))
            if !d.running && !dev.registered, let l = dev.addDeviceLink { m.addItem(open("Finish Adding in the Dashboard…", l)) }
            if d.running {
                m.addItem(action("Stop Sharing", #selector(stopSharing)))
            } else if dev.registered {
                m.addItem(action("Start Sharing", #selector(startSharing)))
            }
            m.addItem(open("Open in Admin Console", "/org/\(d.org)/directory?n=\(q(d.name))"))
            m.addItem(action("Forget This Device…", #selector(forgetDevice)))
        } else {
            m.addItem(action("Share this Mac…", #selector(shareThisMac)))
            m.addItem(label("Let agents reach folders you choose"))
        }
        m.addItem(.separator())
        m.addItem(sectionHeader("Agents on this Mac"))
        let here = store.agentsHere
        if here.isEmpty { m.addItem(label("None in this org")) }
        for a in here {
            let it = NSMenuItem(title: a.agent, action: nil, keyEquivalent: "")
            it.subtitleText(shortAddr(a.addr))
            let sm = NSMenu()
            sm.autoenablesItems = false
            sm.addItem(label("\(a.agent).agents.\(a.org)"))
            sm.addItem(copyItem("Copy Address", a.addr))
            sm.addItem(copyItem("Copy “meshy whoami”", "meshy whoami"))
            it.submenu = sm
            m.addItem(it)
        }
        addJoinItems(to: m)
        m.addItem(.separator())
        let tools = action("Install Command-Line Tools", #selector(installTools))
        tools.state = dev.toolsInstalled ? .on : .off
        m.addItem(tools)
        return m
    }

    private func recentMenu() -> NSMenu {
        let m = NSMenu()
        m.autoenablesItems = false
        for e in store.events {
            let it = open(e.title, "/org/\(org)/activity")
            it.subtitleText(ago(Date(timeIntervalSince1970: e.ts)))
            m.addItem(it)
        }
        m.addItem(.separator())
        m.addItem(open("Open Activity", "/org/\(org)/activity"))
        return m
    }

    private func moreMenu() -> NSMenu {
        let m = NSMenu()
        m.autoenablesItems = false
        m.addItem(sectionHeader("Account"))
        if let a = store.address {
            m.addItem(copyItem("Signed in as \(shortAddr(a))", a))
            m.addItem(action("Sign Out", #selector(signOut)))
        } else {
            m.addItem(action("Sign In with Ethereum…", #selector(signIn)))
        }
        m.addItem(sectionHeader("App"))
        m.addItem(action("Settings…", #selector(openSettings), key: ","))
        let login = action("Start at Login", #selector(toggleLogin))
        login.state = SMAppService.mainApp.status == .enabled ? .on : .off
        m.addItem(login)
        let notif = action("Notifications", #selector(toggleNotifications))
        notif.state = store.notificationsOn ? .on : .off
        m.addItem(notif)
        if store.address == nil {
            let tools = action("Install Command-Line Tools", #selector(installTools))
            tools.state = dev.toolsInstalled ? .on : .off
            m.addItem(tools)
        }
        m.addItem(sectionHeader("Help"))
        m.addItem(link("How Meshy Works", "https://github.com/jashwanth0712/meshy#readme"))
        m.addItem(link("Documentation", "https://github.com/jashwanth0712/meshy/tree/main/docs"))
        let tr = NSMenu()
        tr.autoenablesItems = false
        tr.addItem(action("Refresh Now", #selector(refreshNow), key: "r"))
        tr.addItem(action("Run meshyd doctor", #selector(doctor)))
        tr.addItem(action("Show Logs", #selector(showLogs)))
        tr.addItem(action("Copy Diagnostics", #selector(diagnostics)))
        m.addItem(submenu("Troubleshoot", tr))
        m.addItem(.separator())
        m.addItem(label("Meshy \(appVersion) · ENSv2 on Sepolia · Arkiv"))
        return m
    }

    // MARK: item helpers

    private var org: String { store.org ?? "" }
    private func dirPath() -> String { "/org/\(org)/directory" }

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

    private func open(_ title: String, _ path: String, key: String = "") -> NSMenuItem {
        let it = action(title, #selector(openPath(_:)), key: key)
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

    private func short(_ name: String) -> String { org.isEmpty ? name : name.replacingOccurrences(of: ".\(org)", with: "") }
    private func tilde(_ p: String) -> String {
        p.replacingOccurrences(of: "^/Users/[^/]+", with: "~", options: .regularExpression).replacingOccurrences(of: "^/home/[^/]+", with: "~", options: .regularExpression)
    }

    // MARK: actions

    @objc func toggleLive(_ s: NSSwitch) { store.live = s.state == .on }
    @objc func quit() { NSApp.terminate(nil) }
    @objc func refreshNow() { store.refresh(force: true); dev.poll() }
    @objc func copyValue(_ s: NSMenuItem) {
        guard let v = s.representedObject as? String else { return }
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(v, forType: .string)
    }
    @objc func openPath(_ s: NSMenuItem) { if let p = s.representedObject as? String { openDashboard(p) } }
    @objc func openURL(_ s: NSMenuItem) { if let u = (s.representedObject as? String).flatMap(URL.init(string:)) { NSWorkspace.shared.open(u) } }
    @objc func toggleFav(_ s: NSMenuItem) { if let n = s.representedObject as? String { store.toggleFavorite(n) } }
    @objc func pickOrg(_ s: NSMenuItem) { if let o = s.representedObject as? String { store.switchOrg(o) } }
    @objc func removeOrg() { if let o = store.org { store.removeOrg(o) } }
    @objc func toggleNotifications() { store.notificationsOn.toggle() }
    @objc func cancelJoin() { dev.cancelJoin() }

    @objc func signIn() {
        guard let u = store.beginSignIn() else { return }
        NSWorkspace.shared.open(u) // MetaMask lives in the browser; the page comes back via meshy://auth
    }

    @objc func signOut() {
        guard confirm("Sign out of Meshy?", info: "Your orgs stay saved for when you sign in again. A shared device keeps running.", ok: "Sign Out") else { return }
        store.signOut()
    }

    /// After a successful meshy://auth callback.
    func signedIn(_ addr: String) {
        store.notify(title: "Signed in to Meshy", body: "as \(shortAddr(addr))")
        if store.orgs.isEmpty {
            addOrgPrompt()
        } else {
            DispatchQueue.main.async { self.item.button?.performClick(nil) }
        }
    }

    @objc func addOrgPrompt() {
        guard checking == nil, let o = prompt("Add your organization", info: "Type the org's ENS name. Meshy reads your role in it from ENS on Sepolia.", placeholder: "your-org.eth") else { return }
        addOrg(named: o)
    }

    func addOrg(named raw: String) {
        let o = raw.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
        guard o.range(of: "^([a-z0-9-]+\\.)+eth$", options: .regularExpression) != nil else { return alert("“\(raw)” isn't an ENS name. Try something like acme.eth.") }
        guard store.address != nil else { return alert("Sign in first, then add \(o).") }
        checking = o
        rebuild()
        store.addOrg(o) { [weak self] r in
            guard let self else { return }
            self.checking = nil
            self.rebuild()
            switch r {
            case .success(let info?):
                let what = info.role == "owner" ? "You own it, so you can give and revoke access."
                    : info.role == "admin" ? "You're a delegated admin of \(info.adminOf.map { $0.replacingOccurrences(of: ".\(o)", with: "") }.joined(separator: ", "))."
                    : "You can browse it and request access. An owner or admin signs grants."
                self.store.notify(title: "Added \(o)", body: what)
                self.item.button?.performClick(nil)
            case .success(nil):
                self.alert("\(o) isn't a Meshy org on Sepolia ENS. Check the spelling, or create it in the dashboard.")
            case .failure(let e):
                self.alert("Couldn't check \(o): \(e.message)")
            }
        }
    }

    @objc func joinPrompt() {
        guard let link = prompt("Join with an invite link", info: "Paste the link an admin sent you. Meshy creates this Mac's agent keys, opens the approval page in your browser, and joins the mesh once it's approved.", placeholder: "https://meshy.live/approve?org=…&agent=…") else { return }
        let l = link.trimmingCharacters(in: .whitespacesAndNewlines)
        guard let c = URLComponents(string: l), c.scheme?.hasPrefix("http") == true, c.queryItems?.contains(where: { $0.name == "org" }) == true, c.queryItems?.contains(where: { $0.name == "agent" }) == true else {
            return alert("That isn't a Meshy invite link. It should contain org= and agent=.")
        }
        let org = c.queryItems?.first { $0.name == "org" }?.value ?? ""
        dev.join(link: l, provider: store.syncURL) { [weak self] r in
            guard let self else { return }
            switch r {
            case .success(let name):
                if self.store.address != nil, !self.store.orgs.contains(org) { self.store.addOrg(org) { _ in } }
                self.store.notify(title: "Joined \(org)", body: "\(name) is approved and on the mesh.")
            case .failure(let e):
                self.alert("Joining didn't finish: \(e.message)")
            }
        }
    }

    @objc func shareThisMac() {
        guard let org = store.org, let map = store.map else { return alert("Add your org first.") }
        let nets = map.allNetworks
        guard !nets.isEmpty else {
            alert("\(org) has no networks yet. Create one in the dashboard (Directory › Add › Network), then come back.")
            return openDashboard(dirPath())
        }
        NSApp.activate(ignoringOtherApps: true)
        let panel = NSOpenPanel()
        panel.canChooseDirectories = true
        panel.canChooseFiles = false
        panel.allowsMultipleSelection = true
        panel.prompt = "Share"
        panel.message = "Choose the folders agents may be granted. Each agent still needs a grant, folder by folder, with an expiry."
        panel.directoryURL = FileManager.default.homeDirectoryForCurrentUser
        guard panel.runModal() == .OK, !panel.urls.isEmpty else { return }
        let roots = panel.urls.map(\.path)
        guard !roots.contains(where: { $0 == "/" || $0 == Bins.home }) else { return alert("Pick specific folders, not your whole disk or home folder.") }

        let a = NSAlert()
        a.messageText = "Share this Mac in \(org)"
        a.informativeText = "Folders: " + roots.map(tilde).joined(separator: ", ")
        let name = NSTextField(string: defaultDeviceLabel())
        let net = NSPopUpButton()
        net.addItems(withTitles: nets.map(\.name))
        let grid = NSGridView(views: [[NSTextField(labelWithString: "Device name"), name], [NSTextField(labelWithString: "Network"), net]])
        grid.column(at: 0).xPlacement = .trailing
        grid.rowSpacing = 8
        name.widthAnchor.constraint(equalToConstant: 200).isActive = true
        grid.frame = NSRect(x: 0, y: 0, width: 300, height: 56)
        a.accessoryView = grid
        a.addButton(withTitle: "Continue")
        a.addButton(withTitle: "Cancel")
        a.window.initialFirstResponder = name
        guard a.runModal() == .alertFirstButtonReturn else { return }
        let label = name.stringValue.trimmingCharacters(in: .whitespaces).lowercased()
        guard label.range(of: "^[a-z0-9][a-z0-9-]{0,40}$", options: .regularExpression) != nil else { return alert("Use lowercase letters, digits and dashes for the device name.") }
        let network = nets[max(0, net.indexOfSelectedItem)]
        let full = "\(label).\(network.fullName)"
        if map.devices.contains(where: { $0.fullName == full }) { return alert("\(full) already exists. Pick another name.") }
        switch dev.share(name: full, org: org, roots: roots) {
        case .success:
            if let l = dev.addDeviceLink { openDashboard(l) }
            alert("Finish in your browser", info: "The dashboard opened with this Mac's join code filled in. An owner or admin of \(network.name) signs 3 transactions. Meshy starts sharing by itself once \(label) shows up on ENS.\n\nNot an admin? Copy the link from This Mac › Finish Adding and send it to one.")
        case .failure(let e):
            alert("Couldn't prepare this Mac: \(e.message)")
        }
    }

    private func defaultDeviceLabel() -> String {
        let host = (Host.current().localizedName ?? "mac").lowercased()
        let s = host.replacingOccurrences(of: "[^a-z0-9]+", with: "-", options: .regularExpression).trimmingCharacters(in: CharacterSet(charactersIn: "-"))
        return String(s.prefix(30)).isEmpty ? "mac" : String(s.prefix(30))
    }

    @objc func startSharing() { if let e = dev.startDaemons() { alert(e) } }
    @objc func stopSharing() { dev.stopDaemons() }
    @objc func forgetDevice() {
        guard let d = dev.device, confirm("Forget \(d.name)?", info: "Meshy stops the daemon and forgets this device here. Its keys stay in ~/.meshy/meshyd, and the ENS name stays until an admin removes it.", ok: "Forget") else { return }
        dev.forgetDevice()
    }

    @objc func installTools() { alert("Command-line tools", info: dev.installTools()) }

    // Request access: builds a dashboard link with the Give access sheet prefilled for an admin to sign.
    @objc func requestAccess(_ s: NSMenuItem) {
        guard let target = s.representedObject as? String, let map = store.map else { return }
        let d = map.devices.first { $0.fullName == target }
        let svc = map.services.first { $0.fullName == target }
        guard d != nil || svc != nil else { return }
        NSApp.activate(ignoringOtherApps: true)
        let a = NSAlert()
        a.messageText = "Request access to \(short(target))"
        a.informativeText = "Meshy makes a link that opens the Give access sheet, filled in, for an admin of \(org) to sign."
        let who = NSComboBox()
        who.addItems(withObjectValues: store.agentsHere.map(\.agent))
        who.stringValue = store.agentsHere.first?.agent ?? ""
        who.placeholderString = "agent name, e.g. claude-1"
        var rows: [[NSView]] = [[NSTextField(labelWithString: "For"), who]]
        let folder = NSComboBox()
        let mode = NSPopUpButton()
        if let d {
            folder.addItems(withObjectValues: d.rootList)
            folder.stringValue = d.rootList.first ?? ""
            folder.placeholderString = "/path/on/\(d.name)"
            mode.addItems(withTitles: ["Read", "Read and write"])
            rows.append([NSTextField(labelWithString: "Folder"), folder])
            rows.append([NSTextField(labelWithString: "Access"), mode])
        }
        let durs: [(String, Int)] = [("15 minutes", 900), ("1 hour", 3600), ("4 hours", 14400), ("1 day", 86400)]
        let dur = NSPopUpButton()
        dur.addItems(withTitles: durs.map(\.0))
        dur.selectItem(at: 1)
        rows.append([NSTextField(labelWithString: "For how long"), dur])
        let grid = NSGridView(views: rows)
        grid.column(at: 0).xPlacement = .trailing
        grid.rowSpacing = 8
        who.widthAnchor.constraint(equalToConstant: 220).isActive = true
        grid.frame = NSRect(x: 0, y: 0, width: 320, height: CGFloat(rows.count) * 30)
        a.accessoryView = grid
        a.addButton(withTitle: "Create Request")
        a.addButton(withTitle: "Cancel")
        guard a.runModal() == .alertFirstButtonReturn else { return }
        let agent = who.stringValue.trimmingCharacters(in: .whitespaces).lowercased()
        guard !agent.isEmpty else { return alert("Say which agent the access is for.") }
        let seconds = durs[max(0, dur.indexOfSelectedItem)].1
        var path = "/org/\(org)?give=\(q(agent))&target=\(q(target))&dur=\(seconds)"
        var p: String?, m: String?
        if d != nil {
            p = folder.stringValue.trimmingCharacters(in: .whitespaces)
            guard p?.hasPrefix("/") == true, p != "/" else { return alert("Pick a folder the device shares.") }
            m = mode.indexOfSelectedItem == 1 ? "rw" : "ro"
            path += "&path=\(q(p!))&mode=\(m!)"
        }
        let link = Settings.base + path
        store.addRequest(AccessRequest(agent: agent, target: target, path: p, mode: m, dur: seconds, at: Date().timeIntervalSince1970, link: link))

        let r = NSAlert()
        r.messageText = canSign ? "Ready to sign" : "Send this to an admin of \(org)"
        r.informativeText = link
        if canSign {
            r.addButton(withTitle: "Open and Sign")
            r.addButton(withTitle: "Copy Link")
        } else {
            r.addButton(withTitle: "Copy Link")
            r.addButton(withTitle: "Email…")
        }
        r.addButton(withTitle: "Done")
        let res = r.runModal()
        if canSign {
            if res == .alertFirstButtonReturn { openDashboard(path) } else if res == .alertSecondButtonReturn { copy(link) }
        } else {
            if res == .alertFirstButtonReturn { copy(link) } else if res == .alertSecondButtonReturn { email(link) }
        }
    }

    @objc func emailRequest(_ s: NSMenuItem) { if let l = s.representedObject as? String { email(l) } }
    @objc func withdraw(_ s: NSMenuItem) { store.dropRequest(at: s.tag) }

    private func copy(_ s: String) {
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(s, forType: .string)
    }

    private func email(_ link: String) {
        let body = "Could you give me access on Meshy? Open this link and sign:\n\n\(link)\n"
        if let svc = NSSharingService(named: .composeEmail) {
            svc.subject = "Meshy access request"
            svc.perform(withItems: [body])
        } else {
            copy(link)
        }
    }

    @objc func toggleLogin() {
        let svc = SMAppService.mainApp
        do {
            if svc.status == .enabled { try svc.unregister() } else { try svc.register() }
        } catch {
            alert("Couldn't change Start at Login: \(error.localizedDescription). Move Meshy to /Applications and try again.")
        }
    }

    @objc func doctor() {
        guard let bin = Bins.path("meshyd") else { return alert("meshyd is missing from Meshy.app. Reinstall Meshy.") }
        let script = FileManager.default.temporaryDirectory.appendingPathComponent("meshy-doctor.command")
        let name = dev.device.map { " --name '\($0.name)'" } ?? ""
        let body = "#!/bin/zsh\nclear\necho '› meshyd doctor'\n'\(bin)' doctor --control '\(Bins.controlSocket)'\(name)\necho\nread -k1 '?Press any key to close'\n"
        do {
            try body.write(to: script, atomically: true, encoding: .utf8)
            try FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: script.path)
            NSWorkspace.shared.open(script)
        } catch {
            alert(error.localizedDescription)
        }
    }

    @objc func showLogs() {
        let dir = Bins.home + "/Library/Logs/Meshy"
        try? FileManager.default.createDirectory(atPath: dir, withIntermediateDirectories: true)
        NSWorkspace.shared.open(URL(fileURLWithPath: dir))
    }

    @objc func diagnostics() {
        let m = store.map
        let s = dev.status
        let lines = [
            "Meshy for Mac \(appVersion), macOS \(ProcessInfo.processInfo.operatingSystemVersionString)",
            "signed in: \(store.address ?? "no")",
            "org: \(store.org ?? "none") (\(store.org.flatMap { store.role($0)?.role } ?? "role unknown"))",
            "orgs: \(store.orgs.joined(separator: ", "))",
            "provider: \(store.syncURL)",
            "dashboard: \(Settings.baseURL)",
            "status: \(store.statusLine)",
            "error: \(store.mapError ?? "none")",
            "devices: \(m?.devices.count ?? 0), services: \(m?.services.count ?? 0), agents: \(m?.agents?.count ?? 0), live grants: \(m?.liveGrants.count ?? 0)",
            "local agents: \(store.localAgents.map { "\($0.agent)@\($0.org)" }.joined(separator: ", "))",
            "shared device: \(dev.device?.name ?? "none"), running: \(dev.device?.running ?? false), daemon: \(s.reachable ? (s.healthy ? "healthy" : "unhealthy") : "not reachable"), mesh: \(s.meshRunning ? "running" : "not running")",
            "tools: \(Bins.dir ?? "-")",
        ]
        copy(lines.joined(separator: "\n"))
    }

    @objc func openSettings() {
        if let w = settingsWindow { NSApp.activate(ignoringOtherApps: true); w.makeKeyAndOrderFront(nil); return }
        let w = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 460, height: 170), styleMask: [.titled, .closable], backing: .buffered, defer: false)
        w.title = "Meshy Settings"
        w.isReleasedWhenClosed = false
        let sync = NSTextField(string: store.syncURL)
        let dash = NSTextField(string: Settings.baseURL)
        let every = NSPopUpButton()
        every.addItems(withTitles: ["15 seconds", "30 seconds", "1 minute", "5 minutes"])
        let secs: [Double] = [15, 30, 60, 300]
        every.selectItem(at: secs.firstIndex(of: store.refreshSeconds) ?? 1)
        let grid = NSGridView(views: [
            [NSTextField(labelWithString: "Provider API"), sync],
            [NSTextField(labelWithString: "Dashboard"), dash],
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
            let s = sync.stringValue.trimmingCharacters(in: .whitespaces)
            if s.hasPrefix("http") { self.store.syncURL = s }
            let d = dash.stringValue.trimmingCharacters(in: .whitespaces)
            if d.hasPrefix("http") { Settings.baseURL = d }
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
        let f = NSTextField(frame: NSRect(x: 0, y: 0, width: 320, height: 24))
        f.placeholderString = placeholder
        a.accessoryView = f
        a.addButton(withTitle: "OK")
        a.addButton(withTitle: "Cancel")
        a.window.initialFirstResponder = f
        return a.runModal() == .alertFirstButtonReturn && !f.stringValue.isEmpty ? f.stringValue : nil
    }

    private func confirm(_ title: String, info: String, ok: String) -> Bool {
        NSApp.activate(ignoringOtherApps: true)
        let a = NSAlert()
        a.messageText = title
        a.informativeText = info
        a.addButton(withTitle: ok)
        a.addButton(withTitle: "Cancel")
        return a.runModal() == .alertFirstButtonReturn
    }

    private func alert(_ text: String, info: String = "") {
        NSApp.activate(ignoringOtherApps: true)
        let a = NSAlert()
        a.messageText = text
        a.informativeText = info
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
