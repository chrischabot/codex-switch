// Written by AI on 2026.10.17 by model GPT-6.1 Sol.
import AppKit
import SwiftUI
import XCTest
@testable import CodexSwitchMenu

final class OneViewDesignRenderTests: XCTestCase {
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

        for arrangement in OneViewArrangement.allCases {
            let panel = OneViewDesign(snapshot: snapshot, arrangement: arrangement)
                .defaultAppStorage(defaults).environment(\.colorScheme, .light)
            let hostingView = NSHostingView(rootView: panel)
            hostingView.appearance = NSAppearance(named: .aqua)
            hostingView.frame = CGRect(x: 0, y: 0, width: arrangement.width, height: 900)
            hostingView.layoutSubtreeIfNeeded()
            hostingView.setFrameSize(hostingView.fittingSize)
            hostingView.layoutSubtreeIfNeeded()
            let panelSize = hostingView.bounds.size
            print("\(arrangement.rawValue): \(Int(panelSize.width)) × \(Int(panelSize.height)) pt; no ScrollView")
            let panelBitmap = try XCTUnwrap(hostingView.bitmapImageRepForCachingDisplay(in: hostingView.bounds))
            hostingView.cacheDisplay(in: hostingView.bounds, to: panelBitmap)
            let panelImage = NSImage(size: panelSize)
            panelImage.addRepresentation(panelBitmap)
            try XCTUnwrap(panelBitmap.representation(using: .png, properties: [:]))
                .write(to: directory.appending(path: "\(arrangement.rawValue)-panel.png"))
            try (arrangement.caption + "\n").write(to: directory.appending(path: "\(arrangement.rawValue)-panel.txt"), atomically: true, encoding: .utf8)

            for (width, height, menuBar) in [(1512, 982, 37), (2560, 1440, 25)] {
                XCTAssertLessThanOrEqual(panelSize.height, CGFloat(height - menuBar - 16), "The whole pool must fit")
                try renderScreen(panelImage: panelImage, width: width, height: height, menuBar: menuBar,
                                 to: directory.appending(path: "\(arrangement.rawValue)-\(width)x\(height).png"))
                try (arrangement.caption + " \(width)×\(height); privacy on.\n")
                    .write(to: directory.appending(path: "\(arrangement.rawValue)-\(width)x\(height).txt"), atomically: true, encoding: .utf8)
            }
        }
        XCTAssertEqual(NSWorkspace.shared.frontmostApplication?.processIdentifier, foregroundBefore)
        XCTAssertTrue(NSApplication.shared.windows.isEmpty, "Design rendering must not open a window")
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
