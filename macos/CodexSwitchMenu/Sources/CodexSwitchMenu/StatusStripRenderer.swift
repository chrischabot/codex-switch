import AppKit

/// Draws the menu bar item: the OpenAI mark, then each account's number in a
/// filled badge followed by its free allowance, as one template image.
@MainActor
enum StatusStripRenderer {
    static let buttonHorizontalPadding: CGFloat = 8

    private static let canvasHeight: CGFloat = 22
    private static let markSize: CGFloat = 16
    private static let markSpacing: CGFloat = 7
    private static let badgeSize: CGFloat = 14
    private static let badgeValueSpacing: CGFloat = 3
    private static let accountSpacing: CGFloat = 9
    private static let valueFont = NSFont.monospacedDigitSystemFont(ofSize: 13, weight: .semibold)
    private static let badgeFont = NSFont.monospacedDigitSystemFont(ofSize: 10.5, weight: .bold)

    struct Layout: Equatable {
        let badges: [String]
        let values: [String]
        let badgeOrigins: [CGFloat]
        let imageSize: NSSize
    }

    static func layout(for accounts: [MenuBarAccountValue]) -> Layout {
        let values = accounts.map { "\(Int($0.freePercent.rounded()))%" }
        var x = markSize + markSpacing
        var origins: [CGFloat] = []
        for (index, value) in values.enumerated() {
            origins.append(x)
            x += badgeSize + badgeValueSpacing
            x += ceil((value as NSString).size(withAttributes: [.font: valueFont]).width)
            if index < values.count - 1 { x += accountSpacing }
        }
        return Layout(
            badges: accounts.map { "\($0.number)" },
            values: values,
            badgeOrigins: origins,
            imageSize: NSSize(width: ceil(x), height: canvasHeight)
        )
    }

    static func image(for accounts: [MenuBarAccountValue]) -> NSImage? {
        guard !accounts.isEmpty, let mark = openAIMark() else { return nil }
        let layout = layout(for: accounts)
        let image = NSImage(size: layout.imageSize, flipped: false) { bounds in
            mark.draw(
                in: NSRect(x: 0, y: floor((bounds.height - markSize) / 2), width: markSize, height: markSize),
                from: .zero, operation: .sourceOver, fraction: 1
            )
            let valueHeight = valueFont.ascender - valueFont.descender
            let valueY = floor((bounds.height - valueHeight) / 2)
            for index in layout.values.indices {
                let badge = NSRect(
                    x: layout.badgeOrigins[index],
                    y: floor((bounds.height - badgeSize) / 2),
                    width: badgeSize, height: badgeSize
                )
                drawBadge(layout.badges[index], in: badge)
                (layout.values[index] as NSString).draw(
                    at: NSPoint(x: badge.maxX + badgeValueSpacing, y: valueY),
                    withAttributes: [.font: valueFont, .foregroundColor: NSColor.black]
                )
            }
            return true
        }
        image.isTemplate = true
        image.accessibilityDescription = accessibilityDescription(for: accounts)
        return image
    }

    /// A filled rounded square with the account number cut out of it, so the
    /// number reads as a label and never as part of the percentage.
    private static func drawBadge(_ number: String, in rect: NSRect) {
        NSColor.black.setFill()
        NSBezierPath(roundedRect: rect, xRadius: 3.5, yRadius: 3.5).fill()
        let attributes: [NSAttributedString.Key: Any] = [.font: badgeFont, .foregroundColor: NSColor.black]
        let size = (number as NSString).size(withAttributes: attributes)
        let origin = NSPoint(x: rect.midX - size.width / 2, y: rect.midY - size.height / 2 + 0.5)
        NSGraphicsContext.saveGraphicsState()
        NSGraphicsContext.current?.compositingOperation = .destinationOut
        (number as NSString).draw(at: origin, withAttributes: attributes)
        NSGraphicsContext.restoreGraphicsState()
    }

    static func identityImage() -> NSImage? {
        guard let mark = openAIMark() else { return nil }
        mark.size = NSSize(width: markSize, height: markSize)
        mark.accessibilityDescription = "Codex Switch"
        return mark
    }

    private static func accessibilityDescription(for accounts: [MenuBarAccountValue]) -> String {
        accounts.map { account in
            "Account \(account.number), \(UsageFormatting.percent(account.freePercent)) left"
        }.joined(separator: "; ")
    }

    private static func openAIMark() -> NSImage? {
        guard let url = Bundle.module.url(
            forResource: "openai",
            withExtension: "svg",
            subdirectory: "Resources"
        ), let image = NSImage(contentsOf: url) else { return nil }
        image.isTemplate = true
        return image
    }
}
