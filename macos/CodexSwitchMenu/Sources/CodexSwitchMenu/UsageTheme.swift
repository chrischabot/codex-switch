import AppKit
import SwiftUI

enum UsageTheme {
    static let panelWidth: CGFloat = 344
    static let outerInset: CGFloat = 14
    static let sectionGap: CGFloat = 12
    static let cardRadius: CGFloat = 12
    static let traySurface = Color(nsColor: .textBackgroundColor)
    /// Allowance is shared capacity, not an account identity. Reserve blue for
    /// this scale and keep account identity in a distinct categorical palette.
    static let capacityColour = Color.blue

    private static let accountColours: [Color] = [
        .purple,
        .orange,
        .green,
        .pink,
        .brown,
        .teal,
        .red,
        .yellow,
        .indigo,
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
    ]

    static func accountColour(_ number: Int) -> Color {
        accountColours[(number - 1) % accountColours.count]
    }
}

extension View {
    func cardSurface() -> some View {
        background {
            RoundedRectangle(cornerRadius: UsageTheme.cardRadius, style: .continuous)
                .fill(Color.primary.opacity(0.052))
        }
    }
}
