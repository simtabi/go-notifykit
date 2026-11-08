// notifier is the macOS helper that go-notifykit wraps in "<App> Notifier.app".
// It reads one notification as JSON on stdin and posts it through
// UNUserNotificationCenter, so the banner carries the bundle's own name and
// icon. It has no UI and does not appear in the Dock (LSUIElement).
//
// The first run shows macOS's "allow notifications" prompt. If the user
// dismisses or declines it, macOS remembers the app as not allowed and every
// later run exits 3 until it is switched on in System Settings > Notifications.
//
// Exit status: 0 posted, 2 bad input, 3 the user has not allowed
// notifications for this app, 4 timed out, 1 anything else.
//
// Rebuild with `make helper` after changing this file; a test fails when the
// embedded binary was not built from the current source.
import Foundation
import UserNotifications

struct Payload: Decodable {
    let title: String
    let subtitle: String?
    let body: String?
    let group: String?
    let sound: Bool?
}

func finish(_ code: Int32, _ message: String) -> Never {
    if code != 0 {
        FileHandle.standardError.write(("notifier: " + message + "\n").data(using: .utf8)!)
    }
    exit(code)
}

let input = FileHandle.standardInput.readDataToEndOfFile()
guard let payload = try? JSONDecoder().decode(Payload.self, from: input) else {
    finish(2, "expected one JSON notification on stdin")
}

let center = UNUserNotificationCenter.current()
let done = DispatchSemaphore(value: 0)
var status: Int32 = 1
var message = "no answer from the notification center"

center.requestAuthorization(options: [.alert, .sound]) { granted, error in
    guard granted else {
        status = 3
        message = error?.localizedDescription ?? "notifications are not allowed for this app"
        done.signal()
        return
    }
    let content = UNMutableNotificationContent()
    content.title = payload.title
    if let s = payload.subtitle, !s.isEmpty { content.subtitle = s }
    content.body = payload.body ?? ""
    if payload.sound == true { content.sound = .default }
    // A group is both the thread and the request identifier, so a newer
    // notification in the same group replaces the older one.
    var id = UUID().uuidString
    if let g = payload.group, !g.isEmpty {
        content.threadIdentifier = g
        id = g
    }
    center.add(UNNotificationRequest(identifier: id, content: content, trigger: nil)) { error in
        if let error = error {
            status = 1
            message = error.localizedDescription
        } else {
            status = 0
        }
        done.signal()
    }
}

// The first run waits for the user to answer the permission prompt.
if done.wait(timeout: .now() + 120) == .timedOut {
    finish(4, "timed out waiting for the notification center")
}
finish(status, status == 0 ? "posted" : message)
