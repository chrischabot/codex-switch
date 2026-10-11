struct MenuBarAccountValue: Equatable, Sendable {
    let number: Int
    let freePercent: Double

    static func values(for accounts: [CSSwitch_Account]) -> [Self] {
        accounts.compactMap { account in
            guard let allowance = account.allowance.windows.max(by: { $0.periodSeconds < $1.periodSeconds }) else { return nil }
            return Self(number: Int(account.accountNumber), freePercent: allowance.freePercent)
        }
    }

}
