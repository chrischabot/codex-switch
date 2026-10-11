import AppKit
import SwiftUI
import XCTest
@testable import CodexSwitchMenu

final class PanelRenderTests: XCTestCase {
    /// Renders the menu bar item and every panel state from a real snapshot
    /// without opening a window or taking focus.
    @MainActor
    func testRenderRealSnapshotWithoutOpeningMenu() async throws {
        let environment = ProcessInfo.processInfo.environment
        guard let snapshotPath = environment["CODEX_SWITCH_RENDER_SNAPSHOT"],
              let outputPath = environment["CODEX_SWITCH_RENDER_OUTPUT"] else {
            throw XCTSkip("Run scripts/dev/render-menu with a real menu snapshot")
        }
        let foregroundBefore = NSWorkspace.shared.frontmostApplication?.processIdentifier
        let bytes = try Data(contentsOf: URL(filePath: snapshotPath))
        let snapshot = try MenuSnapshotDecoding.decode(bytes)
        let output = URL(filePath: outputPath, directoryHint: .isDirectory)
        try FileManager.default.createDirectory(at: output, withIntermediateDirectories: true)
        let cacheFile = FileManager.default.temporaryDirectory.appending(path: "\(UUID().uuidString)/menu-snapshot.pb")
        let store = OverviewStore(client: SavedSnapshot(bytes: bytes), cacheFile: cacheFile)
        await store.refresh()

        let periods: [(String, OverviewSnapshot.ReportingPeriodKind)] = [
            ("today", .today), ("7-days", .sevenDays), ("30-days", .thirtyDays),
        ]
        for (periodName, period) in periods {
            for metric in UsageMetric.allCases {
                try await renderPanel(store: store, period: period, metric: metric, privacy: false, maximumHeight: 4_000,
                                      to: output.appending(path: "panel-\(periodName)-\(metric.rawValue).png"))
            }
        }
        try await renderPanel(store: store, period: .today, metric: .cost, privacy: true, maximumHeight: 4_000,
                              to: output.appending(path: "panel-today-cost-private.png"))
        try await renderPanel(store: store, period: .thirtyDays, metric: .cost, privacy: false, maximumHeight: 900,
                              to: output.appending(path: "panel-30-days-cost-monitor.png"))

        let values = MenuBarAccountValue.values(for: snapshot.accounts)
        let strip = try XCTUnwrap(StatusStripRenderer.image(for: values))
        try writeMenuBar(strip, background: NSColor(white: 0.93, alpha: 1), ink: .black,
                         to: output.appending(path: "menubar-light.png"))
        try writeMenuBar(strip, background: NSColor(white: 0.12, alpha: 1), ink: .white,
                         to: output.appending(path: "menubar-dark.png"))

        let foregroundAfter = NSWorkspace.shared.frontmostApplication?.processIdentifier
        print("Background panel render: foreground PID before=\(foregroundBefore.map(String.init) ?? "none") after=\(foregroundAfter.map(String.init) ?? "none")")
        XCTAssertTrue(NSApplication.shared.windows.isEmpty, "Background rendering must not create windows")
    }

    @MainActor
    private func renderPanel(
        store: OverviewStore,
        period: OverviewSnapshot.ReportingPeriodKind,
        metric: UsageMetric,
        privacy: Bool,
        maximumHeight: CGFloat,
        to url: URL
    ) async throws {
        let defaults = UserDefaults()
        defaults.setVolatileDomain([
            "privacyModeEnabled": privacy,
            "usageMetric": metric.rawValue,
        ], forName: UserDefaults.argumentDomain)
        let panel = CodexSwitchPanel(store: store, maximumHeight: maximumHeight, initialPeriod: period)
            .defaultAppStorage(defaults)
            .environment(\.colorScheme, .light)
        let hostingView = NSHostingView(rootView: panel)
        hostingView.appearance = NSAppearance(named: .aqua)
        hostingView.frame = CGRect(x: 0, y: 0, width: UsageTheme.panelWidth, height: maximumHeight)
        for _ in 0..<4 {
            hostingView.layoutSubtreeIfNeeded()
            await Task.yield()
        }
        hostingView.setFrameSize(hostingView.fittingSize)
        hostingView.layoutSubtreeIfNeeded()
        let bitmap = try XCTUnwrap(hostingView.bitmapImageRepForCachingDisplay(in: hostingView.bounds))
        hostingView.cacheDisplay(in: hostingView.bounds, to: bitmap)
        try XCTUnwrap(bitmap.representation(using: .png, properties: [:])).write(to: url)
    }

    /// Paints the template image the way the menu bar does: one ink colour on the bar.
    @MainActor
    private func writeMenuBar(_ template: NSImage, background: NSColor, ink: NSColor, to url: URL) throws {
        let scale: CGFloat = 2
        let size = NSSize(width: template.size.width + 24, height: 24)
        let bitmap = try XCTUnwrap(NSBitmapImageRep(
            bitmapDataPlanes: nil, pixelsWide: Int(size.width * scale), pixelsHigh: Int(size.height * scale),
            bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true, isPlanar: false,
            colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0
        ))
        bitmap.size = size
        let tinted = NSImage(size: template.size, flipped: false) { bounds in
            template.draw(in: bounds)
            ink.set()
            bounds.fill(using: .sourceAtop)
            return true
        }
        NSGraphicsContext.saveGraphicsState()
        NSGraphicsContext.current = NSGraphicsContext(bitmapImageRep: bitmap)
        background.setFill()
        NSRect(origin: .zero, size: size).fill()
        tinted.draw(in: NSRect(x: 12, y: (size.height - template.size.height) / 2,
                               width: template.size.width, height: template.size.height))
        NSGraphicsContext.restoreGraphicsState()
        try XCTUnwrap(bitmap.representation(using: .png, properties: [:])).write(to: url)
    }
}

private struct SavedSnapshot: SnapshotLoading {
    let bytes: Data
    func loadSnapshot() async throws -> Data { bytes }
}
