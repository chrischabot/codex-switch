import Foundation

enum UsageMetric: String, CaseIterable, Hashable, Identifiable, Sendable {
    case cost, tokens
    var id: String { rawValue }
    var title: LocalizedStringResource {
        switch self {
        case .cost: "API-equivalent cost"
        case .tokens: "Tokens"
        }
    }
}

struct UsagePresentation: Equatable, Sendable {
    let totalCostNanoUSD: Int64?
    let totalTokens: Int64
    let costFractionDigits: Int
    let accountTokenShares: [Share]
    let accountCostShares: [Share]

    init?(snapshot: OverviewSnapshot, selectedPeriod: OverviewSnapshot.ReportingPeriodKind) {
        guard let period = snapshot.reportingPeriod(selectedPeriod), !period.accounts.isEmpty else { return nil }
        let accounts = snapshot.menuSnapshot.accounts
        totalTokens = period.accounts.reduce(0) { $0 + $1.tokens }
        totalCostNanoUSD = period.accounts.allSatisfy(\.hasCostNanoUsd) ? period.accounts.reduce(0) { $0 + $1.costNanoUsd } : nil
        costFractionDigits = totalCostNanoUSD.map(UsageFormatting.costFractionDigits) ?? 2
        func shares(metric: UsageMetric, total: Int64) -> [Share] {
            var cursor = 0.0
            return period.accounts.compactMap { item in
                guard let account = accounts.first(where: { $0.key == item.accountKey }) else { return nil }
                let start = cursor
                let amount = metric == .tokens ? item.tokens : item.costNanoUsd
                if total > 0 { cursor += Double(amount) / Double(total) }
                return Share(id: account.key, label: "Account \(account.accountNumber)", accountNumber: Int(account.accountNumber), tokens: item.tokens, costNanoUSD: item.hasCostNanoUsd ? item.costNanoUsd : nil, startFraction: start, endFraction: cursor)
            }
        }
        accountTokenShares = shares(metric: .tokens, total: totalTokens)
        accountCostShares = totalCostNanoUSD.map { shares(metric: .cost, total: $0) } ?? []
    }

    struct Share: Equatable, Identifiable, Sendable {
        let id: String
        let label: String
        let accountNumber: Int
        let tokens: Int64
        let costNanoUSD: Int64?
        let startFraction: Double
        let endFraction: Double
    }
}
