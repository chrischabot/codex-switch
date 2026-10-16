import AppKit
import SwiftUI

enum UsageTheme {
    static let panelWidth: CGFloat = 360
    static let outerInset: CGFloat = 14
    static let cardRadius: CGFloat = 12
    static let traySurface = Color(nsColor: .textBackgroundColor)

    /// OpenUsage's type scale: one title size, one label size, one detail size.
    static let titleFont = Font.system(size: 14, weight: .semibold)
    static let labelFont = Font.system(size: 13, weight: .semibold)
    static let detailFont = Font.system(size: 12)
    static let detailEmphasisFont = Font.system(size: 12, weight: .medium)

    /// Allowance is capacity, not an account identity. Reserve blue for
    /// this scale and keep account identity in a distinct categorical palette.
    static let capacityColour = Color(nsColor: .systemBlue)
    /// An allowance on pace to empty before it resets.
    static let runOutColour = Color(nsColor: .systemRed)

    /// Green is reserved for the working state, blue for capacity and red
    /// for running out, so no account wears them or anything close.
    private static let accountColours: [Color] = [
        .purple,
        .orange,
        .teal,
        .brown,
        .yellow,
        Color(red: 0.60, green: 0.40, blue: 0.55),
        Color(red: 0.75, green: 0.65, blue: 0.45),
        Color(red: 0.50, green: 0.50, blue: 0.50),
    ]

    static func accountColour(_ number: Int) -> Color {
        accountColours[(number - 1) % accountColours.count]
    }
}

/// How much air the panel has. Regular is OpenUsage's rhythm: 13-point
/// labels, 12-point detail, text rows 26 to 28 points apart. Compact is
/// what the panel falls back to only when regular would not fit the screen,
/// such as six accounts on a 14-inch laptop; it keeps every word and number.
enum PanelDensity {
    case regular, compact

    var cardInset: CGFloat { self == .regular ? 12 : 10 }
    var sectionGap: CGFloat { self == .regular ? 10 : 8 }
    /// Between the identity group and the first window block, and between blocks.
    var groupGap: CGFloat { self == .regular ? 18 : 12 }
    var blockTitleGap: CGFloat { self == .regular ? 8 : 6 }
    var identityFont: Font { .system(size: self == .regular ? 12 : 11) }
    var identityGap: CGFloat { self == .regular ? 4 : 3 }
    var rowFont: Font { .system(size: self == .regular ? 12 : 11) }
    var rowEmphasisFont: Font { .system(size: self == .regular ? 12 : 11, weight: .medium) }
    var rowHeight: CGFloat { self == .regular ? 18 : 16 }
    var rowGap: CGFloat { self == .regular ? 9 : 8 }
    var barHeight: CGFloat { 5 }
    var legendGap: CGFloat { self == .regular ? 5 : 3 }
    var ringSize: CGFloat { self == .regular ? 96 : 92 }
    /// Grid columns, sized for "100% left" and a flame with "Resets in 22d 12h".
    var numberWidth: CGFloat { 24 }
    var percentWidth: CGFloat { self == .regular ? 58 : 52 }
    var timeWidth: CGFloat { self == .regular ? 116 : 106 }
}

private struct PanelDensityKey: EnvironmentKey {
    static let defaultValue = PanelDensity.regular
}

extension EnvironmentValues {
    var panelDensity: PanelDensity {
        get { self[PanelDensityKey.self] }
        set { self[PanelDensityKey.self] = newValue }
    }
}

extension View {
    func cardSurface() -> some View {
        background {
            RoundedRectangle(cornerRadius: UsageTheme.cardRadius, style: .continuous)
                .fill(.fill.quaternary)
        }
    }
}

/// The account's colour dot, used wherever an account is named.
struct AccountMarker: View {
    let colour: Color
    var size: CGFloat = 8

    var body: some View {
        Circle()
            .fill(colour)
            .frame(width: size, height: size)
            .accessibilityHidden(true)
    }
}
