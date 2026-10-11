import SwiftUI

struct RoutedTasksSection: View {
    let snapshot: CSSwitch_MenuSnapshot
    let period: CSSwitch_ReportingPeriod?
    let selectedPeriod: OverviewSnapshot.ReportingPeriodKind
    let selectedMetric: UsageMetric

    private var hasUsage: Bool { period?.hasTaskUsage == true }

    /// The ten largest named tasks. Tasks that share a name, such as repeated
    /// `codex exec` runs in one repository, make one row.
    private var rows: [TaskRow] {
        let tasks = hasUsage ? period!.taskUsage.tasks : snapshot.completedTasks
        var rowsByTitle: [String: TaskRow] = [:]
        for task in tasks where task.hasTitle && !task.title.isEmpty {
            let title = firstLine(task.title)
            var row = rowsByTitle[title] ?? TaskRow(title: title)
            row.tokens += task.tokens
            row.costNanoUSD += task.costNanoUsd
            row.accountNumbers.formUnion(accountNumbers(task))
            row.lastCompletedAt = max(row.lastCompletedAt, task.lastCompletedAt.date)
            rowsByTitle[title] = row
        }
        let ranked = rowsByTitle.values.sorted { left, right in
            guard hasUsage else { return left.lastCompletedAt > right.lastCompletedAt }
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
            let accountSlots = Array(Set(rows.flatMap(\.accountNumbers))).sorted()
            VStack(alignment: .leading, spacing: 9) {
                HStack(alignment: .firstTextBaseline) {
                    Text(hasUsage ? "Top tasks" : "Recent tasks")
                        .font(UsageTheme.labelFont)
                    Spacer()
                    if hasUsage {
                        Text(selectedPeriod.title)
                            .font(UsageTheme.detailFont)
                            .foregroundStyle(.secondary)
                    }
                }
                .padding(.bottom, 1)
                ForEach(rows) { row in
                    HStack(spacing: 8) {
                        Text(row.title)
                            .font(UsageTheme.detailFont)
                            .lineLimit(1)
                            .truncationMode(.tail)
                        Spacer(minLength: 6)
                        if hasUsage {
                            Text(selectedMetric == .tokens ? UsageFormatting.tokens(row.tokens) : UsageFormatting.cost(row.costNanoUSD))
                                .font(UsageTheme.detailEmphasisFont)
                                .foregroundStyle(.secondary)
                                .monospacedDigit()
                                .fixedSize()
                        }
                        HStack(spacing: 4) {
                            ForEach(accountSlots, id: \.self) { number in
                                HStack(spacing: 3) {
                                    AccountMarker(number: number, size: 7)
                                    Text("\(number)")
                                        .font(UsageTheme.detailEmphasisFont)
                                        .monospacedDigit()
                                }
                                .frame(width: 20, alignment: .trailing)
                                .opacity(row.accountNumbers.contains(number) ? 1 : 0)
                                .accessibilityElement(children: .ignore)
                                .accessibilityLabel("Account \(number)")
                                .accessibilityHidden(!row.accountNumbers.contains(number))
                            }
                        }
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

    private func accountNumbers(_ task: CSSwitch_CompletedTask) -> Set<Int> {
        Set(task.accountKeys.compactMap { key in
            snapshot.accounts.first(where: { $0.key == key }).map { Int($0.accountNumber) }
        })
    }
}

private struct TaskRow: Identifiable {
    let title: String
    var tokens: Int64 = 0
    var costNanoUSD: Int64 = 0
    var accountNumbers: Set<Int> = []
    var lastCompletedAt: Date = .distantPast
    var id: String { title }
}
