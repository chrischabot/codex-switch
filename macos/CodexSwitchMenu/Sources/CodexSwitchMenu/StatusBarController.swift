import AppKit
import SwiftUI

/// A borderless panel that can take key input without activating the app,
/// so Escape closes it and its menus open, while the app behind keeps focus.
private final class MenuBarPanel: NSPanel {
    override var canBecomeKey: Bool { true }
}

@MainActor
final class StatusBarController: NSObject {
    private let store: OverviewStore
    private let updates: UpdateController?
    private let statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
    private let panel: MenuBarPanel
    private let panelController: NSHostingController<CodexSwitchPanel>
    private var refreshTask: Task<Void, Never>?
    private var eventMonitors: [Any] = []
    private var resizeObserver: NSObjectProtocol?
    /// Where the panel's top-left corner sits while it is shown, so it grows
    /// downward when its content changes height.
    private var anchorTopLeft: NSPoint = .zero

    private static let cornerRadius: CGFloat = 12
    /// Space between the menu bar and the panel, and between the panel and the screen's edges.
    private static let screenMargin: CGFloat = 6

    init(store: OverviewStore, updates: UpdateController? = nil) {
        self.store = store
        self.updates = updates
        panelController = NSHostingController(
            rootView: CodexSwitchPanel(
                store: store,
                maximumHeight: NSScreen.main?.visibleFrame.height ?? 700,
                updates: updates
            )
        )
        panelController.sizingOptions = [.preferredContentSize]
        panel = MenuBarPanel(
            contentRect: NSRect(x: 0, y: 0, width: UsageTheme.panelWidth, height: 400),
            styleMask: [.borderless, .nonactivatingPanel, .fullSizeContentView],
            backing: .buffered,
            defer: true
        )
        super.init()

        panel.contentViewController = panelController
        panel.level = .popUpMenu
        panel.hidesOnDeactivate = false
        panel.hasShadow = true
        panel.isMovable = false
        panel.isOpaque = false
        panel.backgroundColor = .clear
        panel.collectionBehavior = [.canJoinAllSpaces, .fullScreenAuxiliary]
        panel.isReleasedWhenClosed = false
        if let contentView = panel.contentView {
            contentView.wantsLayer = true
            contentView.layer?.cornerRadius = Self.cornerRadius
            contentView.layer?.cornerCurve = .continuous
            contentView.layer?.masksToBounds = true
        }
        resizeObserver = NotificationCenter.default.addObserver(
            forName: NSWindow.didResizeNotification, object: panel, queue: .main
        ) { [weak self] _ in
            MainActor.assumeIsolated { self?.keepAnchored() }
        }

        guard let button = statusItem.button else { return }
        button.target = self
        button.action = #selector(togglePanel)
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
        closePanel()
    }

    @objc private func togglePanel() {
        guard statusItem.button != nil else { return }
        if panel.isVisible {
            closePanel()
        } else {
            showPanel()
            Task { [weak self] in
                guard let self else { return }
                await self.store.refresh()
                self.updateStatusItem()
            }
        }
    }

    /// Opens the panel directly under the status item, on that item's screen,
    /// kept inside the screen when the item sits near its edge.
    func showPanel() {
        guard let button = statusItem.button, let buttonWindow = button.window else { return }
        let screen = buttonWindow.screen ?? NSScreen.main
        let visibleFrame = screen?.visibleFrame ?? NSRect(x: 0, y: 0, width: 1_512, height: 945)
        let buttonFrame = buttonWindow.convertToScreen(button.convert(button.bounds, to: nil))
        let width = UsageTheme.panelWidth
        let x = min(max(buttonFrame.midX - width / 2, visibleFrame.minX + Self.screenMargin),
                    visibleFrame.maxX - width - Self.screenMargin)
        anchorTopLeft = NSPoint(x: x, y: visibleFrame.maxY - Self.screenMargin)
        let maximumHeight = anchorTopLeft.y - visibleFrame.minY - Self.screenMargin
        panelController.rootView = CodexSwitchPanel(store: store, maximumHeight: max(maximumHeight, 320), updates: updates)
        panelController.view.layoutSubtreeIfNeeded()
        let size = panelController.view.fittingSize
        panel.setFrame(NSRect(x: anchorTopLeft.x, y: anchorTopLeft.y - size.height, width: width, height: size.height),
                       display: false)
        panel.makeKeyAndOrderFront(nil)
        startDismissMonitors()
    }

    private func closePanel() {
        stopDismissMonitors()
        panel.orderOut(nil)
    }

    private func keepAnchored() {
        guard panel.isVisible else { return }
        let frame = panel.frame
        let anchored = NSRect(x: anchorTopLeft.x, y: anchorTopLeft.y - frame.height, width: frame.width, height: frame.height)
        if anchored != frame { panel.setFrame(anchored, display: true) }
    }

    /// A click anywhere outside the panel, other than on the status item
    /// (whose own action toggles) or in one of the panel's own menus, or
    /// Escape, closes it.
    private func startDismissMonitors() {
        stopDismissMonitors()
        let clicks: NSEvent.EventTypeMask = [.leftMouseDown, .rightMouseDown, .otherMouseDown]
        if let local = NSEvent.addLocalMonitorForEvents(matching: clicks.union(.keyDown), handler: { [weak self] event in
            let isEscape = event.type == .keyDown && event.keyCode == 53
            let window = event.window
            let isOwnWindow = window == nil || window === self?.panel || window === self?.statusItem.button?.window
                || String(describing: type(of: window!)).contains("Menu")
            MainActor.assumeIsolated {
                if isEscape || (event.type != .keyDown && !isOwnWindow) {
                    self?.closePanel()
                }
            }
            return isEscape ? nil : event
        }) {
            eventMonitors.append(local)
        }
        if let global = NSEvent.addGlobalMonitorForEvents(matching: clicks, handler: { [weak self] _ in
            Task { @MainActor [weak self] in self?.closePanel() }
        }) {
            eventMonitors.append(global)
        }
    }

    private func stopDismissMonitors() {
        for monitor in eventMonitors { NSEvent.removeMonitor(monitor) }
        eventMonitors = []
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
