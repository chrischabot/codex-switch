import AppKit
import SwiftUI
import XCTest
@testable import CodexSwitchMenu

final class PanelRenderTests: XCTestCase {
    /// The tallest the menu may be on a 14-inch MacBook Pro at 1512x982:
    /// the screen less its 37-point menu bar and the panel's margins.
    private static let laptopHeight: CGFloat = 982 - 37 - 16
    private static let desktopHeight: CGFloat = 1_440 - 25 - 16

    @MainActor
    func testProviderContentFitsPanelWithLongAccountStatus() async throws {
        var account = CSSwitch_Account()
        account.key = "layout-account"
        account.accountNumber = 1
        account.provider = .chatgpt
        account.email = "alexandria.engineering.department@example.com"
        account.plan = "promax"
        account.offersConfiguredModel = true
        account.unavailableReason = "OpenAI reports its usage limit reached until Fri 30 Oct 17:42"
        var snapshot = CSSwitch_MenuSnapshot()
        snapshot.accounts = [account]
        let availableWidth = UsageTheme.panelWidth - 2 * UsageTheme.outerInset

        for privacy in [false, true] {
            for density in [PanelDensity.regular, .compact] {
                let measurement = LayoutMeasurement()
                let content = ProviderSection(snapshot: snapshot, provider: .chatgpt, hideIdentity: privacy, now: .now)
                    .environment(\.panelDensity, density)
                    .background {
                        GeometryReader { proxy in
                            Color.clear.preference(key: ContentBoundsKey.self, value: proxy.frame(in: .named("viewport")))
                        }
                    }
                    .frame(width: availableWidth, alignment: .leading)
                    .coordinateSpace(name: "viewport")
                    .onPreferenceChange(ContentBoundsKey.self) { bounds in
                        MainActor.assumeIsolated { measurement.bounds = bounds }
                    }
                let host = NSHostingView(rootView: content)
                host.frame = CGRect(x: 0, y: 0, width: availableWidth, height: 400)
                for _ in 0..<6 {
                    host.layoutSubtreeIfNeeded()
                    try await Task.sleep(for: .milliseconds(20))
                    host.setFrameSize(host.fittingSize)
                }
                let bounds = try XCTUnwrap(measurement.bounds)
                XCTAssertGreaterThan(bounds.height, 0)
                XCTAssertGreaterThanOrEqual(bounds.minX, -0.5, "Privacy \(privacy), density \(density): left edge must remain visible")
                XCTAssertLessThanOrEqual(bounds.maxX, availableWidth + 0.5, "Privacy \(privacy), density \(density): right edge must remain visible")
            }
        }
    }

    /// Renders the menu bar item and every panel state from a real snapshot,
    /// light and dark, without opening a window or taking focus. Every state
    /// must fit the laptop screen.
    @MainActor
    func testRenderRealSnapshotWithoutOpeningMenu() async throws {
        let environment = ProcessInfo.processInfo.environment
        guard let snapshotPath = environment["CODEX_SWITCH_RENDER_SNAPSHOT"],
              let outputPath = environment["CODEX_SWITCH_RENDER_OUTPUT"] else {
            throw XCTSkip("Run scripts/dev/render-menu with a real menu snapshot")
        }
        let foregroundBefore = NSWorkspace.shared.frontmostApplication?.processIdentifier
        let real = try MenuSnapshotDecoding.decode(Data(contentsOf: URL(filePath: snapshotPath)))
        let output = URL(filePath: outputPath, directoryHint: .isDirectory)
        try FileManager.default.createDirectory(at: output, withIntermediateDirectories: true)

        let states: [(String, CSSwitch_MenuSnapshot)] = [
            ("real", real),
            ("all-fine", real.allFine()),
            ("one-limited", real.oneLimited()),
            ("reserve", real.withReserves()),
            ("six-accounts", real.withSixthAccount()),
        ]
        for (state, snapshot) in states {
            for dark in [false, true] {
                let appearance = dark ? "dark" : "light"
                let size = try await renderPanel(snapshot, period: .today, metric: .cost, privacy: true, dark: dark,
                                                 maximumHeight: Self.laptopHeight,
                                                 to: output.appending(path: "panel-\(state)-\(appearance).png"))
                // The panel clamps to the screen and scrolls past it, so a panel
                // shorter than the screen is one that needed no scrolling.
                print("\(state) \(appearance): \(Int(size.width)) × \(Int(size.height)) pt")
                XCTAssertLessThan(size.height, Self.laptopHeight, "\(state) must fit the laptop screen without scrolling")
                if state == "real" {
                    try screenRender(panel: output.appending(path: "panel-\(state)-\(appearance).png"), dark: dark,
                                     to: output.appending(path: "screen-1512x982-\(appearance).png"))
                }
            }
        }
        for dark in [false, true] {
            let appearance = dark ? "dark" : "light"
            _ = try await renderPanel(real, period: .today, metric: .cost, privacy: false, dark: dark,
                                      maximumHeight: Self.laptopHeight, to: output.appending(path: "panel-real-privacy-off-\(appearance).png"))
            let open = try await renderPanel(real, period: .today, metric: .cost, privacy: true, dark: dark, tasksOpen: true,
                                             maximumHeight: Self.laptopHeight, to: output.appending(path: "panel-real-tasks-open-\(appearance).png"))
            print("real tasks open \(appearance): \(Int(open.width)) × \(Int(open.height)) pt")
            XCTAssertLessThanOrEqual(open.height, Self.laptopHeight, "the open task list fills the screen at most")
        }
        _ = try await renderPanel(real, period: .today, metric: .cost, privacy: true, dark: false,
                                  maximumHeight: Self.desktopHeight, to: output.appending(path: "panel-real-2560x1440-light.png"))
        for (name, period) in [("7-days", OverviewSnapshot.ReportingPeriodKind.sevenDays), ("30-days", .thirtyDays)] {
            _ = try await renderPanel(real, period: period, metric: .cost, privacy: true, dark: false,
                                      maximumHeight: Self.laptopHeight, to: output.appending(path: "panel-real-\(name)-light.png"))
        }
        _ = try await renderPanel(real, period: .today, metric: .tokens, privacy: true, dark: false,
                                  maximumHeight: Self.laptopHeight, to: output.appending(path: "panel-real-tokens-light.png"))

        let values = MenuBarAccountValue.values(for: real.accounts)
        let strip = try XCTUnwrap(StatusStripRenderer.image(for: values))
        try writeMenuBar(strip, background: NSColor(white: 0.93, alpha: 1), ink: .black,
                         to: output.appending(path: "menubar-light.png"))
        try writeMenuBar(strip, background: NSColor(white: 0.12, alpha: 1), ink: .white,
                         to: output.appending(path: "menubar-dark.png"))

        // Rendering must never bring this process to the front; whichever app the owner has in front may change meanwhile.
        XCTAssertNotEqual(NSWorkspace.shared.frontmostApplication?.processIdentifier, ProcessInfo.processInfo.processIdentifier)
        _ = foregroundBefore
        XCTAssertTrue(NSApplication.shared.windows.isEmpty, "Background rendering must not create windows")
    }

    @MainActor
    private func renderPanel(
        _ snapshot: CSSwitch_MenuSnapshot,
        period: OverviewSnapshot.ReportingPeriodKind,
        metric: UsageMetric,
        privacy: Bool,
        dark: Bool,
        tasksOpen: Bool = false,
        maximumHeight: CGFloat,
        to url: URL
    ) async throws -> CGSize {
        let bytes: Data = try snapshot.serializedBytes()
        let cacheFile = FileManager.default.temporaryDirectory.appending(path: "\(UUID().uuidString)/menu-snapshot.pb")
        let store = OverviewStore(client: SavedSnapshot(bytes: bytes), cacheFile: cacheFile)
        await store.refresh()
        let defaults = UserDefaults()
        defaults.setVolatileDomain([
            "privacyModeEnabled": privacy,
            "usageMetric": metric.rawValue,
            "topTasksExpanded": tasksOpen,
        ], forName: UserDefaults.argumentDomain)
        let panel = CodexSwitchPanel(store: store, maximumHeight: maximumHeight, initialPeriod: period)
            .defaultAppStorage(defaults)
            .environment(\.colorScheme, dark ? .dark : .light)
        let hostingView = NSHostingView(rootView: panel)
        hostingView.appearance = NSAppearance(named: dark ? .darkAqua : .aqua)
        hostingView.frame = CGRect(x: 0, y: 0, width: UsageTheme.panelWidth, height: maximumHeight)
        // The task list sizes itself to the room above it, which takes a second pass.
        for _ in 0..<6 {
            hostingView.layoutSubtreeIfNeeded()
            try await Task.sleep(for: .milliseconds(20))
            hostingView.setFrameSize(hostingView.fittingSize)
        }
        hostingView.layoutSubtreeIfNeeded()
        let bitmap = try XCTUnwrap(hostingView.bitmapImageRepForCachingDisplay(in: hostingView.bounds))
        hostingView.cacheDisplay(in: hostingView.bounds, to: bitmap)
        try XCTUnwrap(bitmap.representation(using: .png, properties: [:])).write(to: url)
        return hostingView.bounds.size
    }

    /// The panel under the menu bar of a 1512x982 screen, as it would hang from the status item.
    @MainActor
    private func screenRender(panel: URL, dark: Bool, to url: URL) throws {
        let panelImage = try XCTUnwrap(NSImage(contentsOf: panel))
        let (width, height, menuBar) = (1_512, 982, 37)
        let bitmap: NSBitmapImageRep = try XCTUnwrap(NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: width, pixelsHigh: height,
            bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true, isPlanar: false,
            colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0))
        NSGraphicsContext.saveGraphicsState()
        NSGraphicsContext.current = NSGraphicsContext(bitmapImageRep: bitmap)
        NSColor(white: dark ? 0.16 : 0.86, alpha: 1).setFill()
        NSRect(x: 0, y: 0, width: width, height: height).fill()
        NSColor(white: dark ? 0.1 : 0.94, alpha: 1).setFill()
        NSRect(x: 0, y: height - menuBar, width: width, height: menuBar).fill()
        let size = panelImage.size
        panelImage.draw(in: NSRect(x: CGFloat(width) - size.width - 20, y: CGFloat(height - menuBar - 8) - size.height,
                                   width: size.width, height: size.height))
        NSGraphicsContext.restoreGraphicsState()
        let png: Data = try XCTUnwrap(bitmap.representation(using: .png, properties: [:]))
        try png.write(to: url)
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

@MainActor
private final class LayoutMeasurement {
    var bounds: CGRect?
}

private struct ContentBoundsKey: PreferenceKey {
    static let defaultValue: CGRect = .zero
    static func reduce(value: inout CGRect, nextValue: () -> CGRect) {
        let next = nextValue()
        if !next.isEmpty { value = next }
    }
}

private struct SavedSnapshot: SnapshotLoading {
    let bytes: Data
    func loadSnapshot() async throws -> Data { bytes }
}

/// The states the panel must show, derived from the real snapshot so the
/// names, totals and tasks stay real.
extension CSSwitch_MenuSnapshot {
    /// Every window a fifth used with its reset ahead: nothing runs out.
    func allFine() -> CSSwitch_MenuSnapshot {
        var copy = self
        for index in copy.accounts.indices {
            for window in copy.accounts[index].allowance.windows.indices {
                copy.accounts[index].allowance.windows[window].usedPercent = 20
                copy.accounts[index].allowance.limitReached = false
            }
        }
        return copy
    }

    /// The first Codex account at its weekly limit, so new work goes to the others.
    func oneLimited() -> CSSwitch_MenuSnapshot {
        var copy = self
        guard let index = copy.accounts.firstIndex(where: { $0.menuProvider == .chatgpt && $0.usable }) else { return copy }
        for window in copy.accounts[index].allowance.windows.indices {
            copy.accounts[index].allowance.windows[window].usedPercent = 100
        }
        copy.accounts[index].allowance.limitReached = true
        copy.accounts[index].usable = false
        copy.accounts[index].unavailableReason = "OpenAI reports its usage limit reached"
        copy.accounts[index].state = .limited
        return copy
    }

    /// One account under a soft reserve and one under a hard one, in the
    /// proxy's own words.
    func withReserves() -> CSSwitch_MenuSnapshot {
        var copy = self
        let codex = copy.accounts.indices.filter { copy.accounts[$0].menuProvider == .chatgpt && copy.accounts[$0].usable }
        if let second = codex.dropFirst().first {
            copy.accounts[second].reserveStatus = "new work elsewhere · 15% reserve"
        }
        if let claude = copy.accounts.lastIndex(where: { $0.menuProvider == .claude }) {
            copy.accounts[claude].usable = false
            copy.accounts[claude].unavailableReason = "held back · 10% reserve"
        }
        return copy
    }

    /// One more Claude account: the last Claude account again, under the
    /// next number, with the same totals and days.
    func withSixthAccount() -> CSSwitch_MenuSnapshot {
        guard accounts.count < 6,
              let source = accounts.last(where: { $0.menuProvider == .claude }),
              let sourceIndex = accounts.lastIndex(of: source) else { return self }
        var copy = self
        var added = source
        added.key = source.key + "-render-copy"
        added.accountNumber = (accounts.filter { $0.menuProvider == .claude }.map(\.accountNumber).max() ?? 0) + 1
        copy.accounts.insert(added, at: sourceIndex + 1)
        for index in copy.reportingPeriods.indices {
            if var total = copy.reportingPeriods[index].accounts.first(where: { $0.accountKey == source.key }) {
                total.accountKey = added.key
                copy.reportingPeriods[index].accounts.append(total)
            }
        }
        for day in accountDays where day.accountKey == source.key {
            var copied = day
            copied.accountKey = added.key
            copy.accountDays.append(copied)
        }
        return copy
    }
}
