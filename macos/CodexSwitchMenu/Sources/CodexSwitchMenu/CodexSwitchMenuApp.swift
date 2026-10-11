import AppKit
import OSLog
import SwiftUI

private let launchLogger = Logger(
    subsystem: Bundle.main.bundleIdentifier ?? "CodexSwitchMenu",
    category: "Launch"
)

@main
@MainActor
final class AppDelegate: NSObject, NSApplicationDelegate {
    private var statusBarController: StatusBarController?
    private let launchConfiguration: SnapshotLaunchConfiguration

    init(launchConfiguration: SnapshotLaunchConfiguration) {
        self.launchConfiguration = launchConfiguration
    }

    static func main() {
        let launchConfiguration: SnapshotLaunchConfiguration
        do {
            launchConfiguration = try SnapshotLaunchConfiguration(
                arguments: Array(CommandLine.arguments.dropFirst())
            )
        } catch {
            launchLogger.error("Invalid staged launch configuration")
            return
        }
        let application = NSApplication.shared
        let delegate = AppDelegate(launchConfiguration: launchConfiguration)
        application.delegate = delegate
        application.run()
    }

    func applicationDidFinishLaunching(_ notification: Notification) {
        let store = OverviewStore(
            client: CodexSwitchClient(configuration: launchConfiguration)
        )
        application.setActivationPolicy(.accessory)
        let controller = StatusBarController(store: store)
        statusBarController = controller
    }

    func applicationShouldHandleReopen(
        _ sender: NSApplication,
        hasVisibleWindows flag: Bool
    ) -> Bool {
        statusBarController?.showPopover()
        return false
    }

    func applicationWillTerminate(_ notification: Notification) {
        statusBarController?.stop()
    }

    private var application: NSApplication { .shared }
}
