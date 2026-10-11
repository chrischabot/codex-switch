struct MenuBarAccountValue: Equatable, Sendable {
    let number: Int
    let freePercent: Double

    /// The accounts that can run the configured model, each with the free
    /// share of its longest allowance window.
    static func values(for accounts: [CSSwitch_Account]) -> [Self] {
        accounts.filter(\.offersConfiguredModel).compactMap { account in
            guard let allowance = account.allowance.windows.max(by: { $0.periodSeconds < $1.periodSeconds }) else { return nil }
            return Self(number: Int(account.accountNumber), freePercent: allowance.freePercent)
        }
    }
}
