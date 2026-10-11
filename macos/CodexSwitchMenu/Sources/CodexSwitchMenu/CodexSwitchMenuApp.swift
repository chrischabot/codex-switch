import AppKit
import OSLog
import ServiceManagement
import SwiftUI

private let launchLogger = Logger(
    subsystem: Bundle.main.bundleIdentifier ?? "CodexSwitchMenu",
    category: "Launch"
)

@main
@MainActor
final class AppDelegate: NSObject, NSApplicationDelegate {
    private var statusBarController: StatusBarController?
    private let launchConfiguration: LaunchConfiguration

    init(launchConfiguration: LaunchConfiguration) {
        self.launchConfiguration = launchConfiguration
    }

    static func main() {
        let launchConfiguration: LaunchConfiguration
        do {
            launchConfiguration = try LaunchConfiguration(arguments: Array(CommandLine.arguments.dropFirst()))
        } catch {
            launchLogger.error("Invalid launch arguments: \(String(describing: error), privacy: .public)")
            return
        }
        let application = NSApplication.shared
        let delegate = AppDelegate(launchConfiguration: launchConfiguration)
        application.delegate = delegate
        application.run()
    }

    func applicationDidFinishLaunching(_ notification: Notification) {
        let cacheFile = URL.cachesDirectory
            .appending(path: Bundle.main.bundleIdentifier ?? "CodexSwitchMenu", directoryHint: .isDirectory)
            .appending(path: "menu-snapshot.pb")
        let store = OverviewStore(
            client: SnapshotClient(dataDirectory: launchConfiguration.dataDirectory),
            cacheFile: cacheFile
        )
        NSApplication.shared.setActivationPolicy(.accessory)
        statusBarController = StatusBarController(store: store)
        if launchConfiguration.isProduction {
            registerLoginItem()
        }
    }

    /// Leaves any state other than `.notRegistered` alone so the login item can be turned off in System Settings.
    private func registerLoginItem() {
        let service = SMAppService.mainApp
        guard service.status == .notRegistered else { return }
        do {
            try service.register()
        } catch {
            launchLogger.error("Login item registration failed: \(error.localizedDescription, privacy: .public)")
        }
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
}
