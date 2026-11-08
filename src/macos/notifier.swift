// notifier is the macOS helper that go-notifykit wraps in "<App> Notifier.app".
// It posts one notification through UNUserNotificationCenter, so the banner
// carries the bundle's own name and icon, and it runs a notification's click
// action when the notification is clicked. It has no UI and does not appear in
// the Dock (LSUIElement).
//
// Posting: go-notifykit runs Contents/MacOS/notifier with one JSON object on
// stdin: {"title", "subtitle", "body", "group", "sound", "image", "open"}.
// image is a file go-notifykit has already checked and copied; open is a click
// action go-notifykit has already checked.
//
// Clicking: macOS relaunches the app with nothing on stdin. It then receives
// the click through its delegate, checks the stored action against the same
// rules again, runs it with no shell, and quits. The rules: an http or https
// URL on 127.0.0.1, localhost or ::1, or exactly the executable named by the
// bundle's NotifyKitOpenPath, run with no arguments. Nothing else is run.
//
// The first run shows macOS's "allow notifications" prompt. If the user
// dismisses or declines it, macOS remembers the app as not allowed.
//
// Exit status: 0 posted, 2 bad input, 3 notifications are not allowed for this
// app, 4 timed out, 1 anything else.
//
// Rebuild with `make helper` after changing this file; a test fails when the
// embedded binary was not built from the current source.
import AppKit
import UserNotifications

struct Payload: Decodable {
    let title: String
    let subtitle: String?
    let body: String?
    let group: String?
    let sound: Bool?
    let image: String?
    let open: String?
}

func fail(_ code: Int32, _ message: String) -> Never {
    FileHandle.standardError.write(("notifier: " + message + "\n").data(using: .utf8)!)
    exit(code)
}

// allowedOpen re-checks a click action: a loopback http(s) URL, or the one
// executable the bundle names. It returns what to open, or nil.
func allowedOpen(_ action: String) -> URL? {
    if let url = URL(string: action), let scheme = url.scheme?.lowercased(),
       scheme == "http" || scheme == "https" {
        guard url.user == nil, url.password == nil,
              let host = url.host?.lowercased(),
              ["127.0.0.1", "localhost", "::1", "[::1]"].contains(host) else { return nil }
        return url
    }
    guard action.hasPrefix("/"),
          let own = Bundle.main.object(forInfoDictionaryKey: "NotifyKitOpenPath") as? String,
          !own.isEmpty, action == own,
          FileManager.default.isExecutableFile(atPath: action) else { return nil }
    return URL(fileURLWithPath: action)
}

final class Notifier: NSObject, NSApplicationDelegate, UNUserNotificationCenterDelegate {
    let payload: Payload?

    init(payload: Payload?) { self.payload = payload }

    func applicationDidFinishLaunching(_: Notification) {
        let center = UNUserNotificationCenter.current()
        center.delegate = self
        guard let p = payload else {
            // Launched by a click: the response arrives through the delegate.
            DispatchQueue.main.asyncAfter(deadline: .now() + 10) { NSApp.terminate(nil) }
            return
        }
        // The first run waits for the user to answer the permission prompt.
        DispatchQueue.main.asyncAfter(deadline: .now() + 120) {
            fail(4, "timed out waiting for the notification center")
        }
        center.requestAuthorization(options: [.alert, .sound]) { granted, error in
            guard granted else {
                fail(3, error?.localizedDescription ?? "notifications are not allowed for this app")
            }
            let content = UNMutableNotificationContent()
            content.title = p.title
            if let s = p.subtitle, !s.isEmpty { content.subtitle = s }
            content.body = p.body ?? ""
            if p.sound == true { content.sound = .default }
            if let o = p.open, allowedOpen(o) != nil { content.userInfo = ["open": o] }
            if let image = p.image, !image.isEmpty {
                do {
                    content.attachments = [try UNNotificationAttachment(
                        identifier: "image", url: URL(fileURLWithPath: image), options: nil)]
                } catch {
                    FileHandle.standardError.write("notifier: image not attached: \(error.localizedDescription)\n".data(using: .utf8)!)
                }
            }
            // A group is both the thread and the request identifier, so a
            // newer notification in the same group replaces the older one.
            var id = UUID().uuidString
            if let g = p.group, !g.isEmpty {
                content.threadIdentifier = g
                id = g
            }
            center.add(UNNotificationRequest(identifier: id, content: content, trigger: nil)) { error in
                if let error = error { fail(1, error.localizedDescription) }
                exit(0)
            }
        }
    }

    func userNotificationCenter(_: UNUserNotificationCenter, willPresent _: UNNotification,
                                withCompletionHandler done: @escaping (UNNotificationPresentationOptions) -> Void) {
        done([.banner, .list, .sound])
    }

    func userNotificationCenter(_: UNUserNotificationCenter, didReceive response: UNNotificationResponse,
                                withCompletionHandler done: @escaping () -> Void) {
        if response.actionIdentifier == UNNotificationDefaultActionIdentifier,
           let action = response.notification.request.content.userInfo["open"] as? String,
           let target = allowedOpen(action) {
            if target.isFileURL {
                let process = Process()
                process.executableURL = target
                process.arguments = []
                try? process.run()
            } else {
                NSWorkspace.shared.open(target)
            }
        }
        done()
        NSApp.terminate(nil)
    }
}

// Posting gets JSON on stdin; a click relaunch gets nothing.
let input = FileHandle.standardInput.readDataToEndOfFile()
var payload: Payload? = nil
if !input.isEmpty {
    guard let p = try? JSONDecoder().decode(Payload.self, from: input) else {
        fail(2, "expected one JSON notification on stdin")
    }
    payload = p
}
let app = NSApplication.shared
app.setActivationPolicy(.accessory)
let notifier = Notifier(payload: payload)
app.delegate = notifier
app.run()
