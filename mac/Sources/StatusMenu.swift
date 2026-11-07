// The menu-bar menu (NSStatusItem). Layout follows docs/mac/PLAN.md §1.
//
//   signed out          Sign In with Ethereum… · Join with an Invite Link… · More · Quit
//   no org yet          Add Organization… · Join… · More · Quit
//   owner / admin       Organizations · Networks · Access · This Mac · Recent · Admin Console · More · Quit
//   member / node       Organizations · Request Access · My Access · This Mac · More · Quit
//
// The menu is never rebuilt while it is open (that can crash AppKit or freeze the menu); new data
// waits for the next open. Only countdown titles update live. Dialogs that answer async work wait
// until the menu has closed.
import AppKit
import ServiceManagement

final class StatusMenu: NSObject, NSMenuDelegate, NSSearchFieldDelegate {
    let store = MeshyStore.shared
    let dev = DeviceManager.shared
    let access = AccessManager.shared
    let item = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
    let root = NSMenu()

    private var countdowns: [(NSMenuItem, MapGrant)] = []
    private var tick: Timer?
    private var networksMenu: NSMenu?
    private var query = ""
    private var settingsWindow: NSWindow?
    private var checking: String? // org being checked on ENS
    private(set) var isOpen = false
    private var afterClose: [() -> Void] = []

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
        let changed = { [weak self] in
            guard let self else { return }
            self.updateButton()
            if !self.isOpen { self.rebuild() }
        }
        store.onChange = changed
        dev.onChange = changed
        access.onChange = changed
        updateButton()
        rebuild()
    }

    // MARK: NSMenuDelegate

    func menuWillOpen(_ menu: NSMenu) {
        guard menu === root else { return }
        isOpen = true
        tick = Timer(timeInterval: 1, repeats: true) { [weak self] _ in self?.updateCountdowns() }
        RunLoop.main.add(tick!, forMode: .common)
        store.refresh(force: false) // results show next time the menu opens
        dev.poll()
    }

    func menuNeedsUpdate(_ menu: NSMenu) {
        guard menu === root, !isOpen else { return }
        store.loadLocalAgents()
        rebuild()
    }

    func menuDidClose(_ menu: NSMenu) {
        guard menu === root else { return }
        isOpen = false
        tick?.invalidate()
        query = ""
        let work = afterClose
        afterClose = []
        DispatchQueue.main.async {
            self.rebuild()
            work.forEach { $0() }
        }
    }

    /// Runs now, or once the menu has closed (never put up a modal over an open menu).
    private func later(_ f: @escaping () -> Void) {
        if isOpen { afterClose.append(f) } else { f() }
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

    /// Owner or delegated admin of the selected org: gets the management menu.
    var manages: Bool { store.org.flatMap { store.role($0) }?.canSign ?? false }

    func rebuild() {
        countdowns = []
        networksMenu = nil
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
        if manages {
            let net = submenu("Networks", networksMenuBuilt())
            net.badgeText((m?.devices.count ?? 0) + (m?.services.count ?? 0))
            root.addItem(net)
            let acc = submenu("Access", accessMenu())
            acc.badgeText(m?.liveGrants.count ?? 0)
            root.addItem(acc)
            if !store.orgRequests.isEmpty {
                let r = submenu("Requests", requestsMenu())
                r.badgeText(store.orgRequests.count)
                root.addItem(r)
            }
        } else {
            root.addItem(submenu("Request Access", requestMenu()))
            let mine = submenu("My Access", myAccessMenu())
            mine.badgeText(myLive.count + store.requests.count)
            root.addItem(mine)
        }
        let devGrants = myLive.filter { !$0.isService && $0.target != nil }
        if !devGrants.isEmpty || !access.mounts.isEmpty {
            let shared = submenu("Shared With Me", sharedWithMeMenu())
            shared.badgeText(devGrants.count)
            root.addItem(shared)
        }
        let mac = submenu("This Mac", thisMacMenu())
        if let s = dev.statusLine { mac.subtitleText(s) }
        root.addItem(mac)
        if manages, !store.events.isEmpty { root.addItem(submenu("Recent", recentMenu())) }
        root.addItem(.separator())
        if manages { root.addItem(open("Admin Console", "/org/\(org)", key: "0")) }
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

        var who = "Not signed in"
        if let a = store.address {
            let role = store.org.map { store.role($0) == nil ? "checking role…" : (store.roleLine ?? "") } ?? ""
            who = shortAddr(a) + (role.isEmpty ? "" : " · \(role)")
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
            if let r = store.role(o) { it.subtitleText(r.role) }
            m.addItem(it)
        }
        m.addItem(.separator())
        m.addItem(action(checking.map { "Checking \($0)…" } ?? "Add Organization…", #selector(addOrgPrompt)))
        if let o = store.org {
            m.addItem(action("Check My Role Again", #selector(recheckRole)))
            m.addItem(action("Remove \(o)", #selector(removeOrg)))
        }
        return m
    }

    // MARK: owner / admin: Networks › (searchable) and Access ›

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
            if hits.count > 40 { m.addItem(open("Show All in Admin Console", dirPath())) }
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
        if map.devices.count > 40 { m.addItem(open("Show All \(map.devices.count) in Admin Console", dirPath())) }
        m.addItem(.separator())
        m.addItem(open("Add a Device…", dirPath()))
    }

    func controlTextDidChange(_ obj: Notification) {
        guard let f = obj.object as? NSSearchField, let m = networksMenu else { return }
        query = f.stringValue
        // Replace the rows under the field, keeping the field (and its focus) in place.
        let tmp = NSMenu()
        fillNetworks(tmp)
        while m.items.count > 1 { m.removeItem(at: 1) }
        for it in tmp.items.dropFirst() {
            tmp.removeItem(it)
            m.addItem(it)
        }
    }

    private func deviceItem(_ d: MapResource, title: String) -> NSMenuItem {
        let it = NSMenuItem(title: title, action: nil, keyEquivalent: "")
        if !d.summary.isEmpty { it.subtitleText(d.summary) }
        let sm = NSMenu()
        sm.autoenablesItems = false
        sm.addItem(label(d.fullName))
        if !d.rootList.isEmpty { sm.addItem(label("Shares " + d.rootList.map(tilde).joined(separator: ", "))) }
        let live = (store.map?.liveGrants ?? []).filter { $0.target == d.fullName }
        sm.addItem(label(live.isEmpty ? "No live access" : "\(plural(live.count, "live grant"))"))
        sm.addItem(.separator())
        sm.addItem(open("Give Access…", "/org/\(org)?give=&target=\(q(d.fullName))"))
        sm.addItem(copyItem("Copy “meshy ssh \(short(d.fullName))”", "meshy ssh \(d.fullName)"))
        if let a = d.addr { sm.addItem(copyItem("Copy Device Address", a)) }
        sm.addItem(open("Open in Admin Console", "/org/\(org)/directory?n=\(q(d.fullName))"))
        if let svcs = d.services, !svcs.isEmpty {
            sm.addItem(.separator())
            sm.addItem(sectionHeader("Services"))
            for s in svcs { sm.addItem(serviceItem(s, title: s.name)) }
        }
        it.submenu = sm
        return it
    }

    private func serviceItem(_ s: MapService, title: String) -> NSMenuItem {
        let it = NSMenuItem(title: title, action: nil, keyEquivalent: "")
        it.subtitleText([s.proto, s.desc].filter { !$0.isEmpty }.joined(separator: " · "))
        let sm = NSMenu()
        sm.autoenablesItems = false
        sm.addItem(label(s.fullName))
        sm.addItem(label("\(s.proto) → \(s.target)"))
        sm.addItem(.separator())
        sm.addItem(open("Give Access…", "/org/\(org)?give=&target=\(q(s.fullName))"))
        sm.addItem(copyItem("Copy “meshy connect \(short(s.fullName))”", "meshy connect \(s.fullName)"))
        sm.addItem(open("Open in Admin Console", "/org/\(org)/directory?n=\(q(s.fullName))"))
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
        let mine = Set(myLive.map(\.fullName))
        for g in live {
            let it = NSMenuItem(title: grantTitle(g), action: nil, keyEquivalent: "")
            if mine.contains(g.fullName) { it.subtitleText("yours") }
            countdowns.append((it, g))
            let sm = NSMenu()
            sm.autoenablesItems = false
            sm.addItem(label(g.fullName))
            let until = DateFormatter.localizedString(from: Date(timeIntervalSince1970: g.expiry), dateStyle: .none, timeStyle: .short)
            sm.addItem(label("\(g.isService ? "One port" : "Folders") · expires \(until)"))
            sm.addItem(.separator())
            sm.addItem(open("Renew…", "/org/\(org)?give=\(q(g.agentLabel))&target=\(q(g.target ?? ""))"))
            sm.addItem(open("Revoke…", "/org/\(org)?revoke=\(q(g.fullName))"))
            it.submenu = sm
            m.addItem(it)
        }
        m.addItem(.separator())
        let ended = map.endedGrants.count
        if ended > 0 { m.addItem(open("\(ended) Ended", "/org/\(org)")) }
        m.addItem(open("Give Access…", "/org/\(org)?give="))
        return m
    }

    /// Owner / admin: what members asked for, straight from the provider (no links to forward).
    private func requestsMenu() -> NSMenu {
        let m = NSMenu()
        m.autoenablesItems = false
        m.addItem(label("Approve opens your wallet to sign the grant."))
        for r in store.orgRequests {
            let title = r.kind == "device" ? "\(r.fromLabel): add device \(short(r.target))" : "\(r.who) → \(short(r.target))"
            let it = NSMenuItem(title: title, action: nil, keyEquivalent: "")
            var sub = [ago(Date(timeIntervalSince1970: r.at))]
            if let p = r.path { sub.insert(tilde(p) + (r.mode == "rw" ? " · read-write" : " · read"), at: 0) }
            if r.kind == "access", r.who != r.fromLabel { sub.append("asked by \(r.fromLabel)") }
            it.subtitleText(sub.joined(separator: " · "))
            let sm = NSMenu()
            sm.autoenablesItems = false
            if r.kind == "access", let d = r.dur { sm.addItem(label("For \(d >= 3600 ? plural(d / 3600, "hour") : plural(d / 60, "minute"))")) }
            sm.addItem(open(r.kind == "device" ? "Add Device…" : "Approve…", r.reviewPath))
            let x = action("Dismiss", #selector(dismissRequest(_:)))
            x.representedObject = r.id
            sm.addItem(x)
            it.submenu = sm
            m.addItem(it)
        }
        return m
    }

    @objc func dismissRequest(_ s: NSMenuItem) { if let id = s.representedObject as? String { store.dismissRequest(id) } }

    // MARK: member / node: Request Access › and My Access ›

    private func requestMenu() -> NSMenu {
        let m = NSMenu()
        m.autoenablesItems = false
        guard let map = store.map else {
            m.addItem(label(store.fetching ? "Loading…" : "No data yet. Try More › Troubleshoot › Refresh Now."))
            return m
        }
        m.addItem(label("Pick what you need. An admin signs it."))
        for n in map.allNetworks {
            m.addItem(.separator())
            m.addItem(sectionHeader(n.name))
            if (n.resources ?? []).isEmpty { m.addItem(label("No devices")) }
            for d in n.resources ?? [] {
                let it = action(d.name + "…", #selector(requestAccess(_:)))
                it.representedObject = d.fullName
                it.subtitleText(d.rootList.isEmpty ? "folders" : d.rootList.map(tilde).joined(separator: ", "))
                m.addItem(it)
                for s in d.services ?? [] {
                    let si = action("\(s.name)…", #selector(requestAccess(_:)))
                    si.representedObject = s.fullName
                    si.indentationLevel = 1
                    si.subtitleText("\(s.proto) port")
                    m.addItem(si)
                }
            }
        }
        if map.allNetworks.isEmpty { m.addItem(label("This org has no devices yet")) }
        return m
    }

    /// Live grants held by this Mac's agents (or the signed-in address).
    private var myLive: [MapGrant] {
        let mine = Set(store.agentsHere.map { $0.addr.lowercased() } + People.here().filter { $0.org == store.org }.map { $0.addr.lowercased() } + [store.address?.lowercased() ?? "-"])
        return (store.map?.liveGrants ?? []).filter { mine.contains($0.owner?.lowercased() ?? "") }
    }

    private func myAccessMenu() -> NSMenu {
        let m = NSMenu()
        m.autoenablesItems = false
        let mine = myLive
        let reqs = store.requests
        if store.agentsHere.isEmpty { m.addItem(label("No agent on this Mac yet. This Mac › Join with an Invite Link.")) }
        if mine.isEmpty && reqs.isEmpty { m.addItem(label("Nothing yet. Use Request Access.")) }
        for g in mine {
            let it = NSMenuItem(title: grantTitle(g), action: nil, keyEquivalent: "")
            countdowns.append((it, g))
            let sm = NSMenu()
            sm.autoenablesItems = false
            sm.addItem(label(g.fullName))
            if let t = g.target {
                if g.isService {
                    sm.addItem(copyItem("Copy “meshy connect \(short(t))”", "meshy connect \(t)"))
                } else {
                    addFinderItems(sm, device: t)
                }
                let r = action("Ask for More Time…", #selector(requestAccess(_:)))
                r.representedObject = t
                sm.addItem(r)
            }
            it.submenu = sm
            m.addItem(it)
        }
        if !reqs.isEmpty {
            m.addItem(.separator())
            m.addItem(sectionHeader("Waiting for an admin"))
        }
        for (i, r) in reqs.enumerated() {
            let it = NSMenuItem(title: "\(r.agent) → \(short(r.target))", action: nil, keyEquivalent: "")
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
        return m
    }

    // MARK: Shared With Me: granted folders as Finder volumes

    private func sharedWithMeMenu() -> NSMenu {
        let m = NSMenu()
        m.autoenablesItems = false
        let grants = myLive.filter { !$0.isService && $0.target != nil }
        if grants.isEmpty { m.addItem(label("No folders shared with you right now")) }
        if !grants.isEmpty, People.inOrg(store.org) == nil, store.agentsHere.isEmpty {
            m.addItem(linkItem())
            m.addItem(label("Once, so devices know this Mac is you"))
            m.addItem(.separator())
        }
        for g in grants {
            guard let t = g.target else { continue }
            let d = store.map?.devices.first { $0.fullName == t }
            let it = NSMenuItem(title: "\(short(t)) · \(left(g.expiry))", action: nil, keyEquivalent: "")
            countdowns.append((it, g))
            let st = access.mounts[t]
            it.subtitleText(st?.state == .mounted ? "in Finder" : st?.state == .mounting ? "opening…" : (d?.rootList.map { ($0 as NSString).lastPathComponent }.joined(separator: ", ") ?? "folders"))
            let sm = NSMenu()
            sm.autoenablesItems = false
            sm.addItem(label(t))
            addFinderItems(sm, device: t)
            it.submenu = sm
            m.addItem(it)
        }
        for (d, mt) in access.mounts where mt.state == .mounted && !grants.contains(where: { $0.target == d }) {
            let e = action("Eject \(short(d))", #selector(ejectDevice(_:)))
            e.representedObject = d
            m.addItem(e)
        }
        return m
    }

    /// Open in Finder / Show in Finder + Eject, and the terminal command for agents.
    private func addFinderItems(_ sm: NSMenu, device t: String) {
        let st = access.mounts[t]
        switch st?.state {
        case .mounted:
            let o = action("Show in Finder", #selector(openInFinder(_:)))
            o.representedObject = t
            sm.addItem(o)
            let e = action("Eject", #selector(ejectDevice(_:)))
            e.representedObject = t
            sm.addItem(e)
        case .mounting:
            sm.addItem(label("Opening in Finder…"))
        default:
            let o = action("Open in Finder", #selector(openInFinder(_:)))
            o.representedObject = t
            sm.addItem(o)
            if let msg = st?.message { sm.addItem(label(msg.count > 80 ? String(msg.prefix(80)) + "…" : msg, color: .systemOrange)) }
        }
        sm.addItem(.separator())
        sm.addItem(copyItem("Copy Terminal Command", "meshy ssh \(t) -- ls"))
    }

    /// Your member label in the org (from the chain), when this Mac isn't linked yet.
    private var myMemberLabel: String? { store.org.flatMap { store.role($0)?.name }?.components(separatedBy: ".").first }

    private func linkItem() -> NSMenuItem {
        if let o = access.linking { return label("Linking this Mac to \(o)… finish in your browser") }
        return action("Link This Mac to \(org)…", #selector(linkThisMacClicked))
    }

    @objc func openInFinder(_ s: NSMenuItem) {
        guard let t = s.representedObject as? String else { return }
        if People.inOrg(store.org) == nil, store.agentsHere.isEmpty, access.mounts[t] == nil {
            // First time on this Mac: link it to your name, then open.
            return linkThisMac(then: t)
        }
        access.mount(t) { [weak self] err in
            guard let self, let err else { return }
            self.later {
                if err.contains("Link This Mac") {
                    if UI.confirm("Link this Mac first", info: err, ok: "Link This Mac…") { self.linkThisMac(then: t) }
                } else {
                    UI.alert("Couldn't open \(self.short(t)) in Finder", info: err)
                }
            }
        }
    }

    @objc func ejectDevice(_ s: NSMenuItem) { if let t = s.representedObject as? String { access.eject(t) } }

    @objc func linkThisMacClicked() { linkThisMac(then: nil) }

    private func linkThisMac(then device: String?) {
        guard let o = store.org else { return }
        access.linkThisMac(org: o) { [weak self] r in
            guard let self else { return }
            switch r {
            case .success(let name):
                self.store.notify(title: "This Mac is linked", body: "Devices now know it as \(name).")
                if let d = device { self.access.mount(d) { e in if let e { self.later { UI.alert("Couldn't open \(self.short(d)) in Finder", info: e) } } } }
            case .failure(let e):
                self.later { UI.alert("Linking this Mac didn't finish", info: e.message) }
            }
        }
        store.notify(title: "Finish in your browser", body: "Connect your wallet and pick \(o). One transaction links this Mac to your name.")
    }

    // MARK: This Mac, Recent, More

    private func thisMacMenu() -> NSMenu {
        let m = NSMenu()
        m.autoenablesItems = false
        m.addItem(sectionHeader("Shared as a device"))
        if let d = dev.device {
            m.addItem(label(d.name))
            if let s = dev.statusLine { m.addItem(label(s.components(separatedBy: " · ").dropFirst().joined(separator: " · "))) }
            m.addItem(.separator())
            m.addItem(sectionHeader("Shared Folders"))
            for (i, r) in d.roots.enumerated() {
                let it = NSMenuItem(title: (r as NSString).lastPathComponent, action: nil, keyEquivalent: "")
                it.subtitleText(tilde(r))
                it.image = NSImage(systemSymbolName: "folder", accessibilityDescription: nil)
                let sm = NSMenu()
                sm.autoenablesItems = false
                let show = action("Show in Finder", #selector(openURL(_:)))
                show.representedObject = URL(fileURLWithPath: r).absoluteString
                sm.addItem(show)
                if d.roots.count > 1 {
                    let rm = action("Stop Sharing This Folder", #selector(removeRoot(_:)))
                    rm.tag = i
                    sm.addItem(rm)
                }
                it.submenu = sm
                m.addItem(it)
            }
            m.addItem(action("Add Folders…", #selector(addRoots)))
            if let pub = dev.publishedRoots, pub != d.roots {
                m.addItem(label(manages ? "Changes not on ENS yet" : "Waiting for an admin to publish the change", color: .systemOrange))
                if manages { m.addItem(open("Publish Folder Changes…", "/org/\(org)/directory?n=\(q(d.name))&roots=\(q(rootsJSON(d.roots)))")) }
            }
            // Every file op, command and block is written to Arkiv by this device's key (needs Rabdi gas).
            let ak = copyItem("Copy Arkiv Writer Address", d.addr)
            ak.subtitleText("Fund it on the Arkiv faucet so activity shows on the dashboard")
            m.addItem(ak)
            if !d.running && !dev.registered, let l = dev.addDeviceLink {
                m.addItem(open("Finish Adding in the Dashboard…", l))
                m.addItem(copyItem("Copy Link for an Admin", Settings.base + l))
            }
            if d.running {
                m.addItem(action("Stop Sharing", #selector(stopSharing)))
            } else if dev.registered {
                m.addItem(action("Start Sharing", #selector(startSharing)))
            }
            m.addItem(action("Forget This Device…", #selector(forgetDevice)))
        } else if store.org != nil {
            m.addItem(action("Share this Mac…", #selector(shareThisMac)))
            m.addItem(label("Let agents reach folders you choose"))
        } else {
            m.addItem(label("Add an org to share this Mac"))
        }
        if let e = dev.status.meshError, e.contains("SEATS") || e.contains("SUBSCRIPTION") {
            m.addItem(label("The provider refused the mesh: \(e.contains("SEATS") ? "the org needs more seats" : "no subscription")", color: .systemOrange))
            if manages { m.addItem(open("Add Seats in Billing…", "/org/\(org)/billing")) } else { m.addItem(label("Ask an owner to add seats (Billing)")) }
        }
        m.addItem(.separator())
        m.addItem(sectionHeader("You on this Mac"))
        if let p = People.inOrg(store.org) {
            m.addItem(label("Linked as \(short(p.name))"))
        } else if store.org != nil {
            m.addItem(linkItem())
            m.addItem(label("Lets you open shared folders in Finder"))
        }
        m.addItem(.separator())
        m.addItem(sectionHeader("Agents on this Mac"))
        let here = store.org == nil ? store.localAgents : store.agentsHere
        if here.isEmpty { m.addItem(label("None yet")) }
        for a in here {
            let it = NSMenuItem(title: a.agent, action: nil, keyEquivalent: "")
            it.subtitleText(shortAddr(a.addr))
            let sm = NSMenu()
            sm.autoenablesItems = false
            sm.addItem(label("\(a.agent).agents.\(a.org)"))
            sm.addItem(copyItem("Copy Address", a.addr))
            it.submenu = sm
            m.addItem(it)
        }
        addJoinItems(to: m)
        m.addItem(.separator())
        let tools = action(dev.toolsInstalled ? "Command-Line Tools Installed" : "Install Command-Line Tools", #selector(installTools))
        tools.state = dev.toolsInstalled ? .on : .off
        if dev.toolsInstalled { tools.subtitleText("meshy is on your PATH (~/.local/bin)") }
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
            m.addItem(copyItem("Signed In as \(shortAddr(a))", a))
            m.addItem(action("Sign Out…", #selector(signOut)))
        } else {
            m.addItem(action("Sign In with Ethereum…", #selector(signIn)))
        }
        m.addItem(sectionHeader("App"))
        m.addItem(action("Settings…", #selector(openSettings), key: ","))
        let login = action("Start at Login", #selector(toggleLogin))
        login.state = !isTest && SMAppService.mainApp.status == .enabled ? .on : .off
        m.addItem(login)
        let notif = action("Notifications", #selector(toggleNotifications))
        notif.state = store.notificationsOn ? .on : .off
        m.addItem(notif)
        m.addItem(sectionHeader("Help"))
        m.addItem(link("How Meshy Works", "https://github.com/jashwanth0712/meshy#readme"))
        let tr = NSMenu()
        tr.autoenablesItems = false
        tr.addItem(action("Refresh Now", #selector(refreshNow), key: "r"))
        tr.addItem(action("Run meshyd doctor", #selector(doctor)))
        tr.addItem(action("Show Logs", #selector(showLogs)))
        tr.addItem(action("Copy Diagnostics", #selector(diagnostics)))
        m.addItem(submenu("Troubleshoot", tr))
        m.addItem(.separator())
        m.addItem(label("Meshy \(appVersion) · \(Settings.host)"))
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

    private func short(_ name: String) -> String { org.isEmpty ? name : name.replacingOccurrences(of: ".\(org)", with: "") }
    private func tilde(_ p: String) -> String {
        p.replacingOccurrences(of: "^/Users/[^/]+", with: "~", options: .regularExpression).replacingOccurrences(of: "^/home/[^/]+", with: "~", options: .regularExpression)
    }

    // MARK: actions

    @objc func toggleLive(_ s: NSSwitch) { store.live = s.state == .on }
    @objc func quit() { NSApp.terminate(nil) }
    @objc func refreshNow() { store.refresh(force: true); dev.poll() }
    @objc func copyValue(_ s: NSMenuItem) { if let v = s.representedObject as? String { UI.copy(v) } }
    @objc func openPath(_ s: NSMenuItem) { if let p = s.representedObject as? String { openDashboard(p) } }
    @objc func openURL(_ s: NSMenuItem) { if let u = (s.representedObject as? String).flatMap(URL.init(string:)) { UI.open(u) } }
    @objc func pickOrg(_ s: NSMenuItem) { if let o = s.representedObject as? String { store.switchOrg(o) } }
    @objc func toggleNotifications() { store.notificationsOn.toggle() }
    @objc func cancelJoin() { dev.cancelJoin() }
    @objc func emailRequest(_ s: NSMenuItem) { if let l = s.representedObject as? String { emailLink(l) } }
    @objc func withdraw(_ s: NSMenuItem) { store.dropRequest(at: s.tag) }

    @objc func removeOrg() {
        guard let o = store.org, UI.confirm("Remove \(o) from this Mac?", info: "Nothing changes on-chain. You can add it again later.", ok: "Remove") else { return }
        store.removeOrg(o)
    }

    @objc func recheckRole() {
        guard let o = store.org else { return }
        store.checkRole(o) { [weak self] r in
            if case .failure(let e) = r { self?.later { UI.alert("Couldn't check your role in \(o)", info: e.message) } }
        }
    }

    @objc func signIn() {
        guard let u = store.beginSignIn() else { return UI.alert("Couldn't start sign-in.") }
        UI.open(u) // MetaMask lives in the browser; the page comes back once through meshy://auth
    }

    @objc func signOut() {
        guard UI.confirm("Sign out of Meshy?", info: "Your orgs stay saved for when you sign in again. A shared device keeps running.", ok: "Sign Out") else { return }
        store.signOut()
    }

    /// After a successful meshy://auth callback (the menu is closed: the browser had focus).
    func signedIn(_ addr: String) {
        store.notify(title: "Signed in to Meshy", body: "as \(shortAddr(addr))")
        // Your orgs come from the chain: created, administered, or joined by invite.
        store.discoverOrgs { [weak self] found in
            guard let self else { return }
            if found.isEmpty {
                if self.store.orgs.isEmpty { self.later { self.addOrgPrompt() } }
            } else {
                self.store.notify(title: found.count == 1 ? "Found your org" : "Found \(found.count) orgs", body: found.joined(separator: ", "))
                if !self.isOpen { self.rebuild() }
            }
        }
    }

    @objc func addOrgPrompt() {
        guard checking == nil else { return }
        guard let o = UI.prompt("Add your organization", info: "Type the org's ENS name. Meshy reads your role in it from ENS on Sepolia.", placeholder: "your-org.eth") else { return }
        addOrg(named: o)
    }

    func addOrg(named raw: String) {
        let o = raw.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
        guard o.range(of: "^([a-z0-9-]+\\.)+eth$", options: .regularExpression) != nil else { return UI.alert("“\(raw)” isn't an ENS name.", info: "Try something like acme.eth.") }
        guard store.address != nil else { return UI.alert("Sign in first, then add \(o).") }
        checking = o
        if !isOpen { rebuild() }
        store.addOrg(o) { [weak self] r in
            guard let self else { return }
            self.checking = nil
            if !self.isOpen { self.rebuild() }
            switch r {
            case .success(let info?):
                let what = info.role == "owner" ? "You own it: the menu shows its devices and access, and you can give and revoke."
                    : info.role == "admin" ? "You're a delegated admin of \(info.adminOf.map { $0.replacingOccurrences(of: ".\(o)", with: "") }.joined(separator: ", "))."
                    : "You're a member: request access to its devices from the menu. An owner or admin signs."
                self.store.notify(title: "Added \(o)", body: what)
                UI.log("ADDED: \(o) role=\(info.role)")
            case .success(nil):
                self.later { UI.alert("\(o) isn't a Meshy org", info: "Nothing called \(o) is registered with Meshy on Sepolia ENS. Check the spelling, or create the org in the dashboard.") }
            case .failure(let e):
                self.later { UI.alert("Couldn't check \(o)", info: e.message) }
            }
        }
    }

    @objc func joinPrompt() {
        guard let link = UI.prompt("Join with an invite link", info: "Paste the link an admin sent you. Meshy makes this Mac's agent keys, opens the approval page in your browser, and joins the mesh once you're approved.", placeholder: "\(Settings.base)/approve?org=…&agent=…") else { return }
        join(link: link)
    }

    /// Join with an invite link (also from meshy://join?link=… on the approval page).
    func join(link: String) {
        guard dev.joinState == nil else { return UI.alert("Already joining", info: dev.joinState ?? "") }
        guard let c = URLComponents(string: link), c.scheme?.hasPrefix("http") == true,
              let org = c.queryItems?.first(where: { $0.name == "org" })?.value, !org.isEmpty,
              let agent = c.queryItems?.first(where: { $0.name == "agent" })?.value, !agent.isEmpty else {
            return UI.alert("That isn't a Meshy invite link.", info: "It should look like \(Settings.base)/approve?org=acme.eth&agent=claude-1.")
        }
        dev.join(link: link, provider: store.syncURL) { [weak self] r in
            guard let self else { return }
            switch r {
            case .success(let name):
                if self.store.address != nil, !self.store.orgs.contains(org) { self.store.addOrgUnchecked(org) }
                self.store.notify(title: "Joined \(org)", body: "\(name) is approved and on the mesh.")
                UI.log("JOINED: \(name)")
            case .failure(let e):
                self.later { UI.alert("Joining didn't finish", info: e.message) }
            }
        }
    }

    @objc func shareThisMac() {
        guard let org = store.org else { return UI.alert("Add your org first.") }
        guard let map = store.map else { return UI.alert("Still loading \(org).", info: "Try again in a moment.") }
        if dev.adoptFromENS(explicit: true), let d = dev.device {
            return UI.alert("This Mac is already \(short(d.name))", info: "It's on ENS with this Mac's keys, sharing \(d.roots.map(tilde).joined(separator: ", ")). Meshy is starting it now: no new device, no transactions.")
        }
        let nets = map.allNetworks
        guard !nets.isEmpty else {
            UI.alert("\(org) has no networks yet.", info: "An owner creates one in the dashboard (Directory › Add › Network).")
            return
        }
        guard let roots = UI.chooseFolders(message: "Choose the folders agents may be granted. Each agent still needs a grant, folder by folder, with an expiry.") else { return }
        guard !roots.isEmpty, !roots.contains(where: { $0 == "/" || $0 == Bins.home }) else { return UI.alert("Pick specific folders, not your whole disk or home folder.") }
        guard let v = UI.form("Share this Mac in \(org)", info: "Folders: " + roots.map(tilde).joined(separator: ", "),
                              fields: [.text("Device name", value: defaultDeviceLabel(), placeholder: "my-mac", options: []), .popup("Network", options: nets.map(\.name), selected: 0)], ok: "Continue"),
              v.count == 2 else { return }
        let label = v[0].lowercased()
        guard label.range(of: "^[a-z0-9][a-z0-9-]{0,40}$", options: .regularExpression) != nil else { return UI.alert("Use lowercase letters, digits and dashes for the device name.") }
        let network = nets[min(max(0, Int(v[1]) ?? 0), nets.count - 1)]
        let full = "\(label).\(network.fullName)"
        if map.devices.contains(where: { $0.fullName == full }) { return UI.alert("\(full) already exists.", info: "Pick another name.") }
        switch dev.share(name: full, org: org, roots: roots) {
        case .success(let d):
            if manages {
                if let l = dev.addDeviceLink { openDashboard(l) }
                UI.alert("Finish in your browser", info: "The dashboard opened with this Mac's join code filled in. Sign the 3 transactions. Meshy starts sharing by itself once \(label) is on ENS.")
                return
            }
            access.sendRequest(["device", full, "--join-code", d.joinCode]) { [weak self] r in
                guard let self else { return }
                switch r {
                case .success:
                    self.store.notify(title: "Asked the admins to add \(label)", body: "They see it in Meshy. This Mac starts sharing by itself once they add it.")
                case .failure:
                    if let l = self.dev.addDeviceLink { UI.copy(Settings.base + l) }
                    self.later { UI.alert("Send the link to an admin", info: "The link to add \(label) is on your clipboard. An owner or admin of \(network.name) opens it and signs. Meshy starts sharing by itself once \(label) is on ENS.\n\nTip: This Mac › Link This Mac lets Meshy send requests to admins directly next time.") }
                }
            }
        case .failure(let e):
            UI.alert("Couldn't prepare this Mac", info: e.message)
        }
    }

    private func defaultDeviceLabel() -> String {
        let host = (Host.current().localizedName ?? "mac").lowercased()
        let s = host.replacingOccurrences(of: "[^a-z0-9]+", with: "-", options: .regularExpression).trimmingCharacters(in: CharacterSet(charactersIn: "-"))
        return s.isEmpty ? "mac" : String(s.prefix(30))
    }

    @objc func startSharing() { dev.startDaemonsAsync { [weak self] e in if let e { self?.later { UI.alert("Couldn't start sharing", info: e) } } } }
    @objc func stopSharing() { dev.stopDaemons() }
    @objc func forgetDevice() {
        guard let d = dev.device, UI.confirm("Forget \(d.name)?", info: "Meshy stops the daemon and forgets this device here. Its keys stay in ~/.meshy/meshyd, and the ENS name stays until an admin removes it.", ok: "Forget") else { return }
        dev.forgetDevice()
    }

    private func rootsJSON(_ r: [String]) -> String { (try? String(data: JSONSerialization.data(withJSONObject: r), encoding: .utf8)) ?? "[]" }

    @objc func addRoots() {
        guard let d = dev.device else { return }
        guard let picked = UI.chooseFolders(message: "Choose more folders to share from this Mac. Admins then decide, folder by folder, who gets each one.") else { return }
        guard !picked.contains(where: { $0 == "/" || $0 == Bins.home }) else { return UI.alert("Pick specific folders, not your whole disk or home folder.") }
        publishRoots(dev.setRoots(d.roots + picked))
    }

    @objc func removeRoot(_ s: NSMenuItem) {
        guard let d = dev.device, d.roots.indices.contains(s.tag) else { return }
        let r = d.roots[s.tag]
        guard UI.confirm("Stop sharing \((r as NSString).lastPathComponent)?", info: "Nobody can open \(tilde(r)) from this Mac any more, even with a grant. Live sessions there end.", ok: "Stop Sharing") else { return }
        publishRoots(dev.setRoots(d.roots.filter { $0 != r }))
    }

    /// The folder list is live on this Mac already; put it on ENS so others see it.
    private func publishRoots(_ roots: [String]) {
        guard let d = dev.device, !roots.isEmpty else { return }
        if manages { return openDashboard("/org/\(org)/directory?n=\(q(d.name))&roots=\(q(rootsJSON(roots)))") }
        access.sendRequest(["roots", d.name, "--roots", roots.joined(separator: ",")]) { [weak self] r in
            guard let self else { return }
            if case .success = r {
                self.store.notify(title: "Folders updated", body: "Shared here now. Admins see the change in Meshy and publish it with one signature.")
            } else {
                self.later { UI.alert("Folders updated on this Mac", info: "Others see the new list once an admin publishes it. Link This Mac so Meshy can ask them directly.") }
            }
        }
    }

    @objc func installTools() { UI.alert("Command-line tools", info: dev.installTools()) }

    /// Request Access: a link that opens the Give access sheet, prefilled, for an admin to sign.
    @objc func requestAccess(_ s: NSMenuItem) {
        guard let target = s.representedObject as? String, let map = store.map else { return }
        let d = map.devices.first { $0.fullName == target }
        let svc = map.services.first { $0.fullName == target }
        guard d != nil || svc != nil else { return UI.alert("\(short(target)) isn't in \(org) any more.") }
        // For you (your member name in the org) or one of this Mac's agents.
        let me = People.inOrg(store.org)?.agent ?? myMemberLabel
        let agents = ([me].compactMap { $0 } + store.agentsHere.map(\.agent)).reduce(into: [String]()) { if !$0.contains($1) { $0.append($1) } }
        var fields: [Field] = [.text("For", value: agents.first ?? "", placeholder: "you or an agent, e.g. claude-1", options: agents)]
        if let d {
            // Folder names, not raw paths; or leave the choice to the admin.
            let names = d.rootList.map { "\(($0 as NSString).lastPathComponent)  (\(tilde($0)))" }
            fields.append(.popup("Folder", options: (d.rootList.count > 1 ? ["Any of its folders (an admin picks)"] : []) + names, selected: 0))
            fields.append(.popup("Access", options: ["Read", "Read and write"], selected: 0))
        }
        let durs: [(String, Int)] = [("15 minutes", 900), ("1 hour", 3600), ("4 hours", 14400), ("1 day", 86400)]
        fields.append(.popup("For how long", options: durs.map(\.0), selected: 1))
        let info = agents.isEmpty ? "Access goes to you (your member name) or an agent. Type the name." : "Admins of \(org) see it in their Meshy menu and sign it."
        guard let v = UI.form("Request access to \(short(target))", info: info, fields: fields, ok: "Create Request"), v.count == fields.count else { return }
        let agent = v[0].lowercased()
        guard agent.range(of: "^[a-z0-9][a-z0-9-]*$", options: .regularExpression) != nil else { return UI.alert("Type a name in \(org), like roshan or claude-1.") }
        if !map.grantees.isEmpty, !map.grantees.contains(agent), agent != me {
            guard UI.confirm("\(agent) isn't in \(org)", info: "Nobody called \(agent) is a member or agent of \(org), so a grant for it can't be used. Send the request anyway?", ok: "Send Anyway") else { return }
        }
        let seconds = durs[min(max(0, Int(v.last!) ?? 1), durs.count - 1)].1
        var path = "/org/\(org)?give=\(q(agent))&target=\(q(target))&dur=\(seconds)"
        var p: String?, m: String?
        if let d {
            guard !d.rootList.isEmpty else { return UI.alert("\(short(target)) doesn't share any folders yet.") }
            let i = Int(v[1]) ?? 0, offset = d.rootList.count > 1 ? 1 : 0
            m = v[2] == "1" ? "rw" : "ro"
            if i >= offset, d.rootList.indices.contains(i - offset) {
                p = d.rootList[i - offset]
                path += "&path=\(q(p!))&mode=\(m!)"
            } else {
                path += "&mode=\(m!)" // any folder: the admin picks in the sheet
            }
        }
        let link = Settings.base + path
        let req = AccessRequest(agent: agent, target: target, path: p, mode: m, dur: seconds, at: Date().timeIntervalSince1970, link: link)
        if manages { return openDashboard(path) } // you can sign it yourself
        var args = ["access", target, "--for", agent, "--dur", String(seconds)]
        if let p { args += ["--path", p] }
        if let m { args += ["--mode", m] }
        access.sendRequest(args) { [weak self] r in
            guard let self else { return }
            self.store.addRequest(req)
            switch r {
            case .success:
                self.store.notify(title: "Request sent", body: "Admins of \(self.org) see it in Meshy. It shows under My Access until they grant it.")
            case .failure(let e):
                // This Mac isn't linked yet, or the provider is older: fall back to a link.
                UI.log("REQUEST-FALLBACK: \(e.message)")
                self.later {
                    switch UI.choose("Send this to an admin of \(self.org)", info: "Meshy couldn't deliver it directly (\(e.message.components(separatedBy: "\n").last ?? "")). Send the link instead:\n\n\(link)", buttons: ["Copy Link", "Email…", "Done"]) {
                    case 0: UI.copy(link)
                    case 1: self.emailLink(link)
                    default: break
                    }
                }
            }
        }
    }

    private func emailLink(_ link: String) {
        UI.email(subject: "Meshy access request", body: "Could you give me access on Meshy? Open this link and sign:\n\n\(link)\n")
    }

    @objc func toggleLogin() {
        if isTest { return UI.log("LOGIN-ITEM: toggled") }
        let svc = SMAppService.mainApp
        do {
            if svc.status == .enabled { try svc.unregister() } else { try svc.register() }
        } catch {
            UI.alert("Couldn't change Start at Login", info: "\(error.localizedDescription). Move Meshy to /Applications and try again.")
        }
    }

    @objc func doctor() {
        guard let bin = Bins.path("meshyd") else { return UI.alert("meshyd is missing from Meshy.app.", info: "Reinstall Meshy.") }
        let script = FileManager.default.temporaryDirectory.appendingPathComponent("meshy-doctor.command")
        let name = dev.device.map { " --name '\($0.name)' --state '\($0.stateDir)'" } ?? ""
        let body = "#!/bin/zsh\nclear\necho '› meshyd doctor'\n'\(bin)' doctor --control '\(dev.device?.controlSocket ?? Bins.controlSocket)' --sepolia '\(Settings.sepoliaWS)'\(name)\necho\nread -k1 '?Press any key to close'\n"
        do {
            try body.write(to: script, atomically: true, encoding: .utf8)
            try FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: script.path)
            UI.open(script)
        } catch {
            UI.alert("Couldn't open Terminal", info: error.localizedDescription)
        }
    }

    @objc func showLogs() {
        let dir = Bins.home + "/Library/Logs/Meshy"
        try? FileManager.default.createDirectory(atPath: dir, withIntermediateDirectories: true)
        UI.open(URL(fileURLWithPath: dir))
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
        UI.copy(lines.joined(separator: "\n"))
    }

    @objc func openSettings() {
        if isTest { return UI.log("WINDOW: Settings (provider \(store.syncURL), dashboard \(Settings.baseURL))") }
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
            if s.hasPrefix("https://") || s.hasPrefix("http://localhost") { self.store.syncURL = s }
            let d = dash.stringValue.trimmingCharacters(in: .whitespaces)
            if d.hasPrefix("https://") || d.hasPrefix("http://localhost") { Settings.baseURL = d }
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

    // MARK: headless tests: print the menu tree, click items

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

    /// Every clickable item as a "Top › Sub › Item" path.
    func actionPaths() -> [(path: String, item: NSMenuItem)] {
        rebuild()
        var out: [(String, NSMenuItem)] = []
        func walk(_ m: NSMenu, _ prefix: [String]) {
            for it in m.items where !it.isSeparatorItem && it.view == nil {
                let p = prefix + [it.title]
                if let s = it.submenu { walk(s, p) } else if it.isEnabled, it.action != nil { out.append((p.joined(separator: " › "), it)) }
            }
        }
        walk(root, [])
        return out
    }

    /// Clicks the item at a "Top › Sub › Item" path (titles may be prefixes). Returns false if not found.
    func click(_ path: String) -> Bool {
        let want = path.components(separatedBy: " › ")
        guard let hit = actionPaths().first(where: { p in
            let got = p.path.components(separatedBy: " › ")
            return got.count == want.count && zip(got, want).allSatisfy { $0.hasPrefix($1) }
        }) else { return false }
        guard let menu = hit.item.menu, let i = Optional(menu.index(of: hit.item)), i >= 0 else { return false }
        menu.performActionForItem(at: i)
        return true
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
