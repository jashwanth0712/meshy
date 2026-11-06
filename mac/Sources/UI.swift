// Every dialog, browser open and clipboard write goes through here. Under MESHY_TEST the
// calls print what they would do ("OPEN: …", "ALERT: …") and take answers from
// MESHY_TEST_INPUT (a JSON array; each prompt or form consumes one element), so
// mac/test.sh can click every menu item headlessly and check what happens.
import AppKit

enum Field {
    case text(String, value: String, placeholder: String, options: [String])
    case popup(String, options: [String], selected: Int)
}

enum UI {
    private static var inputs: [Any] = {
        guard isTest, let s = ProcessInfo.processInfo.environment["MESHY_TEST_INPUT"], let d = s.data(using: .utf8),
              let a = try? JSONSerialization.jsonObject(with: d) as? [Any] else { return [] }
        return a
    }()
    private static func next() -> Any? { inputs.isEmpty ? nil : inputs.removeFirst() }
    static func log(_ s: String) { print(s); fflush(stdout) }

    static func open(_ url: URL) {
        if isTest { return log("OPEN: \(url.absoluteString)") }
        NSWorkspace.shared.open(url)
    }

    static func copy(_ s: String) {
        if isTest { return log("COPY: \(s)") }
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(s, forType: .string)
    }

    static func alert(_ text: String, info: String = "") {
        if isTest { return log("ALERT: \(text)" + (info.isEmpty ? "" : " | \(info.replacingOccurrences(of: "\n", with: " "))")) }
        NSApp.activate(ignoringOtherApps: true)
        let a = NSAlert()
        a.messageText = text
        a.informativeText = info
        a.runModal()
    }

    static func confirm(_ title: String, info: String, ok: String) -> Bool {
        if isTest { log("CONFIRM: \(title)"); return (next() as? Bool) ?? false }
        NSApp.activate(ignoringOtherApps: true)
        let a = NSAlert()
        a.messageText = title
        a.informativeText = info
        a.addButton(withTitle: ok)
        a.addButton(withTitle: "Cancel")
        return a.runModal() == .alertFirstButtonReturn
    }

    /// Asks a question with buttons; returns the index of the button pressed.
    static func choose(_ title: String, info: String, buttons: [String]) -> Int {
        if isTest { log("CHOOSE: \(title) [\(buttons.joined(separator: ", "))]"); return (next() as? Int) ?? 0 }
        NSApp.activate(ignoringOtherApps: true)
        let a = NSAlert()
        a.messageText = title
        a.informativeText = info
        for b in buttons { a.addButton(withTitle: b) }
        return a.runModal().rawValue - NSApplication.ModalResponse.alertFirstButtonReturn.rawValue
    }

    static func prompt(_ title: String, info: String, placeholder: String) -> String? {
        form(title, info: info, fields: [.text("", value: "", placeholder: placeholder, options: [])], ok: "OK")?.first.flatMap { $0.isEmpty ? nil : $0 }
    }

    /// A small form in an alert. Returns one string per field (popups give the selected index), or nil if cancelled.
    static func form(_ title: String, info: String, fields: [Field], ok: String) -> [String]? {
        if isTest {
            log("FORM: \(title)")
            let v = next()
            if let s = v as? String { return [s] }
            return (v as? [Any])?.map { "\($0)" }
        }
        NSApp.activate(ignoringOtherApps: true)
        let a = NSAlert()
        a.messageText = title
        a.informativeText = info
        var views: [NSView] = []
        var rows: [[NSView]] = []
        for f in fields {
            switch f {
            case let .text(label, value, placeholder, options):
                let v: NSTextField
                if options.isEmpty {
                    v = NSTextField(string: value)
                } else {
                    let c = NSComboBox()
                    c.addItems(withObjectValues: options)
                    c.stringValue = value
                    c.completes = true
                    v = c
                }
                v.placeholderString = placeholder
                v.widthAnchor.constraint(equalToConstant: 240).isActive = true
                views.append(v)
                rows.append(label.isEmpty ? [v] : [NSTextField(labelWithString: label), v])
            case let .popup(label, options, selected):
                let p = NSPopUpButton()
                p.addItems(withTitles: options)
                p.selectItem(at: selected)
                views.append(p)
                rows.append([NSTextField(labelWithString: label), p])
            }
        }
        if fields.count == 1, case .text = fields[0] {
            a.accessoryView = views[0]
            views[0].frame = NSRect(x: 0, y: 0, width: 320, height: 24)
        } else {
            let grid = NSGridView(views: rows)
            grid.column(at: 0).xPlacement = .trailing
            grid.rowSpacing = 8
            grid.frame = NSRect(x: 0, y: 0, width: 340, height: CGFloat(rows.count) * 30)
            a.accessoryView = grid
        }
        a.addButton(withTitle: ok)
        a.addButton(withTitle: "Cancel")
        a.window.initialFirstResponder = views.first
        guard a.runModal() == .alertFirstButtonReturn else { return nil }
        return views.map { v in
            if let p = v as? NSPopUpButton { return String(p.indexOfSelectedItem) }
            return ((v as? NSTextField)?.stringValue ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
        }
    }

    /// Folder picker; under test the next input is an array of paths.
    static func chooseFolders(message: String) -> [String]? {
        if isTest { log("FOLDERS: \(message)"); return (next() as? [String]) }
        NSApp.activate(ignoringOtherApps: true)
        let panel = NSOpenPanel()
        panel.canChooseDirectories = true
        panel.canChooseFiles = false
        panel.allowsMultipleSelection = true
        panel.prompt = "Share"
        panel.message = message
        panel.directoryURL = FileManager.default.homeDirectoryForCurrentUser
        guard panel.runModal() == .OK, !panel.urls.isEmpty else { return nil }
        return panel.urls.map(\.path)
    }

    static func email(subject: String, body: String) {
        if isTest { return log("EMAIL: \(subject) | \(body.replacingOccurrences(of: "\n", with: " "))") }
        if let svc = NSSharingService(named: .composeEmail) {
            svc.subject = subject
            svc.perform(withItems: [body])
        } else {
            copy(body)
        }
    }
}
