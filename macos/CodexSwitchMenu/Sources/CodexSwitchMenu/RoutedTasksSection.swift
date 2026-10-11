import SwiftUI

struct RoutedTasksSection: View {
    let snapshot: CSSwitch_MenuSnapshot
    let period: CSSwitch_ReportingPeriod?
    let selectedPeriod: OverviewSnapshot.ReportingPeriodKind
    let selectedMetric: UsageMetric

    private var hasUsage: Bool { period?.hasTaskUsage == true }

    private var namedTasks: [CSSwitch_CompletedTask] {
        let tasks = hasUsage ? period!.taskUsage.tasks : snapshot.completedTasks
        let named = tasks.filter { $0.hasTitle && !$0.title.isEmpty }
        guard hasUsage else { return Array(named.prefix(10)) }
        let rankedTasks = named.sorted { left, right in
            let leftAmount = selectedMetric == .tokens ? left.tokens : left.costNanoUsd
            let rightAmount = selectedMetric == .tokens ? right.tokens : right.costNanoUsd
            if leftAmount != rightAmount { return leftAmount > rightAmount }
            return left.lastCompletedAt.date > right.lastCompletedAt.date
        }
        return Array(rankedTasks.prefix(10))
    }

    var body: some View {
        if !namedTasks.isEmpty {
            VStack(alignment: .leading, spacing: 10) {
                HStack {
                    Text(hasUsage ? "Top tasks" : "Recent tasks")
                        .font(.subheadline.weight(.semibold))
                    Spacer()
                    if hasUsage {
                        Text(selectedPeriod.title)
                            .font(.caption.weight(.medium))
                            .foregroundStyle(.secondary)
                    }
                }
                ForEach(namedTasks, id: \.taskID) { task in
                    HStack(spacing: 8) {
                        Text(task.title)
                            .font(.caption.weight(.medium))
                            .lineLimit(1)
                            .truncationMode(.tail)
                        Spacer(minLength: 0)
                        if hasUsage, let amount = amount(task) {
                            Text(amount)
                                .font(.caption.weight(.medium))
                                .foregroundStyle(.secondary)
                                .monospacedDigit()
                                .fixedSize()
                        }
                        ForEach(task.accountKeys, id: \.self) { accountKey in
                            if let account = snapshot.accounts.first(where: { $0.key == accountKey }) {
                                HStack(spacing: 3) {
                                    Circle()
                                        .fill(UsageTheme.accountColour(Int(account.accountNumber)))
                                        .frame(width: 6, height: 6)
                                        .accessibilityHidden(true)
                                    Text("\(account.accountNumber)")
                                        .font(.caption)
                                        .monospacedDigit()
                                }
                                .accessibilityLabel("Account \(account.accountNumber)")
                            }
                        }
                    }
                }
            }
            .padding(14)
            .cardSurface()
        }
    }

    private func amount(_ task: CSSwitch_CompletedTask) -> String? {
        switch selectedMetric {
        case .tokens:
            task.hasTokens ? UsageFormatting.compactTokens(task.tokens) : nil
        case .cost:
            task.hasCostNanoUsd ? UsageFormatting.compactBaseCost(task.costNanoUsd) : nil
        }
    }
}
