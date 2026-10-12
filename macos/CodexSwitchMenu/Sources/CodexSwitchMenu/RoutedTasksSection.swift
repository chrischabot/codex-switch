import SwiftUI

struct RoutedTasksSection: View {
    let snapshot: CSSwitch_MenuSnapshot
    let period: CSSwitch_ReportingPeriod?
    let selectedPeriod: OverviewSnapshot.ReportingPeriodKind
    let selectedMetric: UsageMetric

    /// The ten largest named tasks across every provider. Tasks that share a
    /// name and provider, such as repeated `codex exec` runs in one
    /// repository, make one row.
    private var rows: [TaskRow] {
        var rowsByID: [String: TaskRow] = [:]
        for task in period?.taskUsage.tasks ?? [] where task.hasTitle && !task.title.isEmpty {
            let accounts = task.accountKeys.compactMap(snapshot.account(key:))
            guard let provider = accounts.first?.menuProvider else { continue }
            let title = firstLine(task.title)
            let id = "\(provider.rawValue) \(title)"
            var row = rowsByID[id] ?? TaskRow(id: id, provider: provider, title: title)
            row.tokens += task.tokens
            row.costNanoUSD += task.costNanoUsd
            row.accountKeys.formUnion(accounts.map(\.key))
            row.lastCompletedAt = max(row.lastCompletedAt, task.lastCompletedAt.date)
            rowsByID[id] = row
        }
        let ranked = rowsByID.values.sorted { left, right in
            let leftAmount = selectedMetric == .tokens ? left.tokens : left.costNanoUSD
            let rightAmount = selectedMetric == .tokens ? right.tokens : right.costNanoUSD
            if leftAmount != rightAmount { return leftAmount > rightAmount }
            return left.lastCompletedAt > right.lastCompletedAt
        }
        return Array(ranked.prefix(10))
    }

    var body: some View {
        let rows = rows
        if !rows.isEmpty {
            let showsProvider = snapshot.providers.count > 1
            // Account markers sit in fixed columns, account 1 first for every
            // provider, so the values line up down the list.
            let slotsByProvider = Dictionary(grouping: rows.flatMap { row in row.accountKeys.map { (row.provider, $0) } }) { $0.0 }
                .mapValues { pairs in
                    Set(pairs.map(\.1)).compactMap(snapshot.account(key:)).sorted { $0.accountNumber < $1.accountNumber }
                }
            let slotCount = slotsByProvider.values.map(\.count).max() ?? 0
            VStack(alignment: .leading, spacing: 9) {
                HStack(alignment: .firstTextBaseline) {
                    Text("Top tasks")
                        .font(UsageTheme.labelFont)
                    Spacer()
                    Text(selectedPeriod.title)
                        .font(UsageTheme.detailFont)
                        .foregroundStyle(.secondary)
                }
                .padding(.bottom, 1)
                ForEach(rows) { row in
                    HStack(spacing: 8) {
                        if showsProvider {
                            ProviderMark(provider: row.provider, size: 12)
                                .foregroundStyle(.secondary)
                        }
                        Text(row.title)
                            .font(UsageTheme.detailFont)
                            .lineLimit(1)
                            .truncationMode(.tail)
                        Spacer(minLength: 6)
                        Text(selectedMetric == .tokens ? UsageFormatting.tokens(row.tokens) : UsageFormatting.cost(row.costNanoUSD))
                            .font(UsageTheme.detailEmphasisFont)
                            .foregroundStyle(.secondary)
                            .monospacedDigit()
                            .fixedSize()
                        HStack(spacing: 4) {
                            let slots = slotsByProvider[row.provider] ?? []
                            ForEach(slots, id: \.key) { account in
                                let used = row.accountKeys.contains(account.key)
                                HStack(spacing: 3) {
                                    AccountMarker(colour: snapshot.colour(of: account), size: 7)
                                    Text("\(account.accountNumber)")
                                        .font(UsageTheme.detailEmphasisFont)
                                        .monospacedDigit()
                                }
                                .frame(width: 20, alignment: .trailing)
                                .opacity(used ? 1 : 0)
                                .accessibilityElement(children: .ignore)
                                .accessibilityLabel(account.poolName)
                                .accessibilityHidden(!used)
                            }
                        }
                        .frame(width: CGFloat(slotCount) * 24 - 4, alignment: .leading)
                    }
                }
            }
            .padding(UsageTheme.cardInset)
            .cardSurface()
        }
    }

    private func firstLine(_ title: String) -> String {
        title.split(whereSeparator: \.isNewline).first.map(String.init) ?? title
    }
}

private struct TaskRow: Identifiable {
    let id: String
    let provider: CSSwitch_Provider
    let title: String
    var tokens: Int64 = 0
    var costNanoUSD: Int64 = 0
    var accountKeys: Set<String> = []
    var lastCompletedAt: Date = .distantPast
}
