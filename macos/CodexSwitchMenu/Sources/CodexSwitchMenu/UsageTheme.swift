import AppKit
import SwiftUI

enum UsageTheme {
    static let panelWidth: CGFloat = 344
    static let outerInset: CGFloat = 14
    static let sectionGap: CGFloat = 12
    static let cardInset: CGFloat = 14
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

    /// Green is reserved for the working state, blue for capacity.
    private static let accountColours: [Color] = [
        .purple,
        .orange,
        .teal,
        .pink,
        .brown,
        .indigo,
        .red,
        .yellow,
        .mint,
        .cyan,
        Color(red: 0.42, green: 0.13, blue: 0.48),
        Color(red: 0.85, green: 0.30, blue: 0.13),
        Color(red: 0.34, green: 0.55, blue: 0.12),
        Color(red: 0.95, green: 0.39, blue: 0.50),
        Color(red: 0.49, green: 0.31, blue: 0.20),
        Color(red: 0.62, green: 0.11, blue: 0.29),
        Color(red: 0.16, green: 0.55, blue: 0.45),
        Color(red: 0.76, green: 0.61, blue: 0.12),
        Color(red: 0.50, green: 0.45, blue: 0.68),
        Color(red: 0.30, green: 0.30, blue: 0.30),
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

/// The account's colour dot and number, used wherever an account is named.
struct AccountMarker: View {
    let number: Int
    var size: CGFloat = 8

    var body: some View {
        Circle()
            .fill(UsageTheme.accountColour(number))
            .frame(width: size, height: size)
            .accessibilityHidden(true)
    }
}
