import AppKit
import SwiftUI

@MainActor
final class StatusBarController: NSObject {
    private let store: OverviewStore
    private let statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
    private let popover = NSPopover()
    private let panelController: NSHostingController<CodexSwitchPanel>
    private var refreshTask: Task<Void, Never>?

    init(store: OverviewStore) {
        self.store = store
        panelController = NSHostingController(
            rootView: CodexSwitchPanel(
                store: store,
                maximumHeight: NSScreen.main?.visibleFrame.height ?? 700
            )
        )
        super.init()

        panelController.sizingOptions = [.preferredContentSize]
        popover.contentViewController = panelController
        popover.behavior = .transient

        guard let button = statusItem.button else { return }
        button.target = self
        button.action = #selector(togglePopover)
        button.sendAction(on: [.leftMouseUp])
        updateStatusItem()

        refreshTask = Task { [weak self] in
            while let self, !Task.isCancelled {
                await self.store.refresh()
                self.updateStatusItem()
                try? await Task.sleep(for: OverviewStore.refreshInterval)
            }
        }
    }

    func stop() {
        refreshTask?.cancel()
        refreshTask = nil
        store.cancelRefresh()
    }

    @objc private func togglePopover() {
        guard statusItem.button != nil else { return }
        if popover.isShown {
            popover.performClose(nil)
        } else {
            showPopover()
            Task { [weak self] in
                guard let self else { return }
                await self.store.refresh()
                self.updateStatusItem()
            }
        }
    }

    func showPopover() {
        guard let button = statusItem.button else { return }
        let visibleHeight = button.window?.screen?.visibleFrame.height
            ?? NSScreen.main?.visibleFrame.height
            ?? 700
        panelController.rootView = CodexSwitchPanel(
            store: store,
            maximumHeight: max(visibleHeight - 16, 320)
        )
        popover.show(relativeTo: button.bounds, of: button, preferredEdge: .minY)
    }

    private func updateStatusItem() {
        guard let button = statusItem.button else { return }
        let accounts = store.latestSnapshot?.accounts ?? []
        let values = MenuBarAccountValue.values(for: accounts)

        if values.isEmpty {
            showIdentity(button: button, toolTip: "Codex Switch")
            return
        }

        let toolTip = values.map { account in
            "\(account.provider.title) \(account.number): \(UsageFormatting.percent(account.freePercent)) left"
        }.joined(separator: "\n")
        guard let image = StatusStripRenderer.image(for: values) else {
            showIdentity(button: button, toolTip: toolTip)
            return
        }

        button.title = ""
        button.image = image
        button.imagePosition = .imageOnly
        statusItem.length = ceil(image.size.width) + StatusStripRenderer.buttonHorizontalPadding
        button.toolTip = toolTip
        button.setAccessibilityLabel(toolTip.replacingOccurrences(of: "\n", with: "; "))
    }

    private func showIdentity(button: NSStatusBarButton, toolTip: String) {
        if let image = StatusStripRenderer.identityImage() {
            statusItem.length = NSStatusItem.squareLength
            button.image = image
            button.title = ""
            button.imagePosition = .imageOnly
        } else {
            statusItem.length = NSStatusItem.variableLength
            button.image = nil
            button.title = "Codex Switch"
            button.imagePosition = .noImage
        }
        button.toolTip = toolTip
        button.setAccessibilityLabel(toolTip.replacingOccurrences(of: "\n", with: "; "))
    }
}
