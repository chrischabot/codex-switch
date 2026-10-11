import AppKit
import SwiftUI
import XCTest
@testable import CodexSwitchMenu

final class PanelRenderTests: XCTestCase {
    @MainActor
    func testRenderRealSnapshotWithoutOpeningMenu() async throws {
        let environment = ProcessInfo.processInfo.environment
        guard let snapshotPath = environment["CODEX_SWITCH_RENDER_SNAPSHOT"],
              let outputPath = environment["CODEX_SWITCH_RENDER_OUTPUT"] else {
            throw XCTSkip("Run scripts/dev/render-menu with a real menu snapshot")
        }
        let foregroundBefore = NSWorkspace.shared.frontmostApplication?.processIdentifier
        let snapshot = try CSSwitch_MenuSnapshot(serializedBytes: Data(contentsOf: URL(filePath: snapshotPath)))
        let store = OverviewStore(client: SavedSnapshot(snapshot: snapshot))
        await store.refresh()
        let outputDirectory = URL(filePath: outputPath, directoryHint: .isDirectory)
        try FileManager.default.createDirectory(at: outputDirectory, withIntermediateDirectories: true)

        let periods: [(String, OverviewSnapshot.ReportingPeriodKind)] = [
            ("today", .today), ("7-days", .sevenDays), ("30-days", .thirtyDays),
        ]
        for (periodName, period) in periods {
            for privacyEnabled in [false, true] {
                let defaults = UserDefaults()
                defaults.setVolatileDomain([
                    "privacyModeEnabled": privacyEnabled,
                    "usageMetric": "tokens",
                ], forName: UserDefaults.argumentDomain)
                let panel = CodexSwitchPanel(store: store, maximumHeight: 900, initialPeriod: period)
                    .defaultAppStorage(defaults)
                    .environment(\.colorScheme, .light)
                let hostingView = NSHostingView(rootView: panel)
                hostingView.frame = CGRect(x: 0, y: 0, width: UsageTheme.panelWidth, height: 900)
                for _ in 0..<3 {
                    hostingView.layoutSubtreeIfNeeded()
                    await Task.yield()
                }
                hostingView.setFrameSize(hostingView.fittingSize)
                hostingView.layoutSubtreeIfNeeded()
                let bitmap = try XCTUnwrap(hostingView.bitmapImageRepForCachingDisplay(in: hostingView.bounds))
                hostingView.cacheDisplay(in: hostingView.bounds, to: bitmap)
                let png = try XCTUnwrap(bitmap.representation(using: .png, properties: [:]))
                try png.write(to: outputDirectory.appending(path: "menu-\(periodName)-\(privacyEnabled ? "private" : "visible").png"))
            }
        }
        let foregroundAfter = NSWorkspace.shared.frontmostApplication?.processIdentifier
        print("Background panel render: foreground PID before=\(foregroundBefore.map(String.init) ?? "none") after=\(foregroundAfter.map(String.init) ?? "none")")
        XCTAssertTrue(NSApplication.shared.windows.isEmpty, "Background rendering must not create windows")
    }
}

private struct SavedSnapshot: SnapshotLoading {
    let snapshot: CSSwitch_MenuSnapshot
    func loadCachedSnapshot() async throws -> CSSwitch_MenuSnapshot { snapshot }
    func loadSnapshot() async throws -> CSSwitch_MenuSnapshot { snapshot }
}
