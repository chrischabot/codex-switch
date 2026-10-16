// Written by AI on 2026.10.17 by model Claude Fable 5.1 (first version by GPT-6.1 Sol).
import AppKit
import SwiftUI
import XCTest
@testable import CodexSwitchMenu

final class OneViewDesignRenderTests: XCTestCase {
    /// Screens the panel must fit without scrolling: the menu bar's height
    /// and the gap under it come off the top, 16 points stay free below.
    private static let screens: [(width: Int, height: Int, menuBar: Int)] = [(1512, 982, 37), (2560, 1440, 25)]
    private static let maximumTaskRows = 10

    @MainActor
    func testRenderOneViewDesigns() async throws {
        let environment = ProcessInfo.processInfo.environment
        guard let input = environment["CODEX_SWITCH_RENDER_SNAPSHOT"],
              let output = environment["CODEX_SWITCH_RENDER_OUTPUT"] else {
            throw XCTSkip("Run scripts/dev/render-menu SNAPSHOT.pb OUTPUT_DIRECTORY one-view")
        }
        let snapshot = try MenuSnapshotDecoding.decode(Data(contentsOf: URL(filePath: input)))
        let directory = URL(filePath: output, directoryHint: .isDirectory)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        let foregroundBefore = NSWorkspace.shared.frontmostApplication?.processIdentifier
        let defaults = UserDefaults()
        defaults.setVolatileDomain(["privacyModeEnabled": true], forName: UserDefaults.argumentDomain)
        print("Design input: \(snapshot.accounts.count) accounts; " + snapshot.providers.map { provider in
            "\(provider.title): \(snapshot.accounts.filter { $0.menuProvider == provider }.count)"
        }.joined(separator: ", "))
        for account in snapshot.accounts {
            print("\(account.poolName): \(account.allowance.windows.map { $0.title(for: account.menuProvider) }.joined(separator: ", "))")
        }

        for direction in OneViewDirection.allCases {
            try render(snapshot, direction: direction, name: direction.rawValue, caption: direction.caption,
                       defaults: defaults, to: directory)
        }
        let chosen = OneViewDirection.compact
        let six = snapshot.withSixthAccount()
        XCTAssertEqual(six.accounts.count, snapshot.accounts.count + 1)
        try render(six, direction: chosen, name: chosen.rawValue + "-six-accounts",
                   caption: chosen.caption + " Six accounts: the last Claude account duplicated in the render only.",
                   defaults: defaults, to: directory)

        XCTAssertEqual(NSWorkspace.shared.frontmostApplication?.processIdentifier, foregroundBefore)
        XCTAssertTrue(NSApplication.shared.windows.isEmpty, "Design rendering must not open a window")
    }

    /// Renders the panel for every screen with as many top tasks as fit, and
    /// writes the laptop fit as the plain panel image.
    @MainActor
    private func render(_ snapshot: CSSwitch_MenuSnapshot, direction: OneViewDirection, name: String, caption: String,
                        defaults: UserDefaults, to directory: URL) throws {
        func panelView(taskRows: Int) -> NSHostingView<some View> {
            let panel = OneViewDesign(snapshot: snapshot, direction: direction, taskRowLimit: taskRows)
                .defaultAppStorage(defaults).environment(\.colorScheme, .light)
            let hostingView = NSHostingView(rootView: panel)
            hostingView.appearance = NSAppearance(named: .aqua)
            hostingView.frame = CGRect(x: 0, y: 0, width: OneViewLayout.panelWidth, height: 900)
            hostingView.layoutSubtreeIfNeeded()
            hostingView.setFrameSize(hostingView.fittingSize)
            hostingView.layoutSubtreeIfNeeded()
            return hostingView
        }
        let oneRow = panelView(taskRows: 1).bounds.height
        let allRows = panelView(taskRows: Self.maximumTaskRows).bounds.height
        let rowHeight = (allRows - oneRow) / CGFloat(Self.maximumTaskRows - 1)
        let withoutTasks = oneRow - rowHeight

        for (index, screen) in Self.screens.enumerated() {
            let budget = CGFloat(screen.height - screen.menuBar - 8 - 16)
            let taskRows = min(Self.maximumTaskRows, max(0, Int((budget - withoutTasks) / rowHeight)))
            let hostingView = panelView(taskRows: taskRows)
            let panelSize = hostingView.bounds.size
            let screenName = "\(screen.width)x\(screen.height)"
            print("\(name) \(screenName): \(Int(panelSize.width)) × \(Int(panelSize.height)) pt, \(taskRows) top tasks; no ScrollView")
            XCTAssertLessThanOrEqual(panelSize.height, budget, "The whole pool must fit on \(screenName)")
            let panelBitmap = try XCTUnwrap(hostingView.bitmapImageRepForCachingDisplay(in: hostingView.bounds))
            hostingView.cacheDisplay(in: hostingView.bounds, to: panelBitmap)
            let panelImage = NSImage(size: panelSize)
            panelImage.addRepresentation(panelBitmap)
            let fit = "\(screenName); \(taskRows) top tasks fit; privacy on."
            if index == 0 {
                try XCTUnwrap(panelBitmap.representation(using: .png, properties: [:]))
                    .write(to: directory.appending(path: "\(name)-panel.png"))
                try (caption + " Panel as it fits \(fit)\n").write(to: directory.appending(path: "\(name)-panel.txt"), atomically: true, encoding: .utf8)
            }
            try renderScreen(panelImage: panelImage, width: screen.width, height: screen.height, menuBar: screen.menuBar,
                             to: directory.appending(path: "\(name)-\(screenName).png"))
            try (caption + " \(fit)\n").write(to: directory.appending(path: "\(name)-\(screenName).txt"), atomically: true, encoding: .utf8)
        }
    }

    @MainActor
    private func renderScreen(panelImage: NSImage, width: Int, height: Int, menuBar: Int, to url: URL) throws {
        let bitmap = try XCTUnwrap(NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: width, pixelsHigh: height,
            bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true, isPlanar: false,
            colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0))
        NSGraphicsContext.saveGraphicsState()
        NSGraphicsContext.current = NSGraphicsContext(bitmapImageRep: bitmap)
        NSColor(white: 0.86, alpha: 1).setFill()
        NSRect(x: 0, y: 0, width: width, height: height).fill()
        NSColor(white: 0.94, alpha: 1).setFill()
        NSRect(x: 0, y: height - menuBar, width: width, height: menuBar).fill()
        panelImage.draw(in: NSRect(x: CGFloat(width) - panelImage.size.width - 20,
                                   y: CGFloat(height - menuBar - 8) - panelImage.size.height,
                                   width: panelImage.size.width, height: panelImage.size.height))
        NSGraphicsContext.restoreGraphicsState()
        try XCTUnwrap(bitmap.representation(using: .png, properties: [:])).write(to: url)
    }
}
