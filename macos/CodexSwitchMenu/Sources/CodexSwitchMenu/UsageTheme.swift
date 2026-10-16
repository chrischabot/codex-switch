import AppKit
import SwiftUI

enum UsageTheme {
    static let panelWidth: CGFloat = 360
    static let outerInset: CGFloat = 14
    static let sectionGap: CGFloat = 10
    static let cardInset: CGFloat = 11
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
