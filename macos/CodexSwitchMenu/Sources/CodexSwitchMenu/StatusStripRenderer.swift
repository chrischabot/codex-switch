import AppKit

@MainActor
enum StatusStripRenderer {
    static let buttonHorizontalPadding: CGFloat = 8

    private static let canvasHeight: CGFloat = 23
    private static let markSize: CGFloat = 17
    private static let markValueSpacing: CGFloat = 7
    private static let valueSpacing: CGFloat = 10
    private static let font = NSFont.systemFont(ofSize: 13.5, weight: .semibold)

    struct Layout: Equatable {
        let values: [String]
        let valueOrigins: [CGFloat]
        let imageSize: NSSize
    }

    static func layout(for accounts: [MenuBarAccountValue]) -> Layout {
        let values = accounts.map { account in
            "\(accountMarker(account.number)) \(compactPercent(account.freePercent))"
        }
        let attributes: [NSAttributedString.Key: Any] = [.font: font]
        var x = markSize + markValueSpacing
        var origins: [CGFloat] = []

        for (index, value) in values.enumerated() {
            origins.append(x)
            x += ceil((value as NSString).size(withAttributes: attributes).width)
            if index < values.count - 1 {
                x += valueSpacing
            }
        }

        return Layout(
            values: values,
            valueOrigins: origins,
            imageSize: NSSize(width: ceil(x), height: canvasHeight)
        )
    }

    static func image(for accounts: [MenuBarAccountValue]) -> NSImage? {
        guard !accounts.isEmpty, let mark = openAIMark() else { return nil }
        let layout = layout(for: accounts)
        let attributes: [NSAttributedString.Key: Any] = [
            .font: font,
            .foregroundColor: NSColor.black,
        ]
        let textHeight = font.ascender - font.descender
        let textY = floor((layout.imageSize.height - textHeight) / 2)

        let image = NSImage(size: layout.imageSize, flipped: false) { bounds in
            let markRect = NSRect(
                x: 0,
                y: floor((bounds.height - markSize) / 2),
                width: markSize,
                height: markSize
            )
            mark.draw(in: markRect, from: .zero, operation: .sourceOver, fraction: 1)

            for (value, x) in zip(layout.values, layout.valueOrigins) {
                (value as NSString).draw(at: NSPoint(x: x, y: textY), withAttributes: attributes)
            }
            return true
        }
        image.isTemplate = true
        image.accessibilityDescription = accessibilityDescription(for: accounts)
        return image
    }

    static func identityImage() -> NSImage? {
        guard let mark = openAIMark() else { return nil }
        mark.size = NSSize(width: markSize, height: markSize)
        mark.accessibilityDescription = "Codex Switch"
        return mark
    }

    private static func accessibilityDescription(for accounts: [MenuBarAccountValue]) -> String {
        accounts.map { account in
            "Account \(account.number), \(UsageFormatting.percent(account.freePercent)) weekly free"
        }.joined(separator: "; ")
    }

    private static func compactPercent(_ value: Double) -> String {
        "\(Int(value.rounded()))%"
    }

    private static func accountMarker(_ number: Int) -> String {
        guard (1...9).contains(number),
              let scalar = UnicodeScalar(0x2460 + number - 1) else {
            return "\(number)"
        }
        return String(Character(scalar))
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
