import Foundation

/// How many accounts' weekly allowances the pool is getting through, from
/// each account's weekly window: its share used so far divided by the share
/// of the window that has passed. "Burning 1.3 Codex and 2.6 Claude accounts a
/// week" says whether the pool is big enough, in the owner's word for it.
enum WeeklyPace {
    static let weekSeconds: Int64 = 604_800

    /// One account's weekly pace, or nil until a seventh of its week has
    /// passed: an hour into a week, one busy session would project the whole week.
    static func accountsPerWeek(_ account: CSSwitch_Account, now: Date) -> Double? {
        guard account.hasAllowance,
              let week = account.allowance.windows.first(where: {
                  $0.periodSeconds == weekSeconds && $0.modelID.isEmpty && $0.modelName.isEmpty && $0.hasResetAt
              }) else { return nil }
        let elapsed = now.timeIntervalSince(week.resetAt.date.addingTimeInterval(-TimeInterval(weekSeconds)))
        guard elapsed >= TimeInterval(weekSeconds) / 7 else { return nil }
        return week.usedPercent / 100 / (elapsed / TimeInterval(weekSeconds))
    }

    /// The pool's pace per provider, in the snapshot's provider order;
    /// providers with no measurable account are left out.
    static func accountsPerWeek(_ snapshot: CSSwitch_MenuSnapshot, now: Date) -> [(provider: CSSwitch_Provider, accounts: Double)] {
        snapshot.providers.compactMap { provider in
            let paces = snapshot.accounts.filter { $0.menuProvider == provider }.compactMap { accountsPerWeek($0, now: now) }
            return paces.isEmpty ? nil : (provider, paces.reduce(0, +))
        }
    }

    /// "Codex: burning 4.1 accounts a week, 2 in the pool", one line per
    /// provider whose week can be read: need against have, which says whether
    /// the pool lasts. Nothing before any week can be read.
    static func lines(_ snapshot: CSSwitch_MenuSnapshot, now: Date, locale: Locale = .autoupdatingCurrent) -> [String] {
        accountsPerWeek(snapshot, now: now).map { pace in
            let pool = snapshot.accounts.filter { $0.menuProvider == pace.provider && $0.offersConfiguredModel }.count
            return "\(pace.provider.title): burning \(UsageFormatting.oneDecimal(pace.accounts, locale: locale)) accounts a week, \(pool) in the pool"
        }
    }
}
