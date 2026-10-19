import Foundation

enum WeeklyPace {
    static func lines(_ snapshot: CSSwitch_MenuSnapshot, locale: Locale = .autoupdatingCurrent) -> [String] {
        snapshot.providers.compactMap { provider in
            guard let burn = snapshot.providerBurns.first(where: { $0.provider == provider }) else { return nil }
            let pool = snapshot.accounts.filter { $0.menuProvider == provider && $0.offersConfiguredModel }.count
            return "\(provider.title): burning \(UsageFormatting.oneDecimal(burn.accountsPerWeek, locale: locale)) accounts a week, \(pool) in the pool"
        }
    }
}
