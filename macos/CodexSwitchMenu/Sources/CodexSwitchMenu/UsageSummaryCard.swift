import SwiftUI

struct UsageSummaryCard: View {
    let presentation: UsagePresentation?
    let selectedPeriod: OverviewSnapshot.ReportingPeriodKind
    let selectedMetric: UsageMetric
    let enabledPeriods: Set<OverviewSnapshot.ReportingPeriodKind>
    let enabledMetrics: Set<UsageMetric>
    let selectPeriod: (OverviewSnapshot.ReportingPeriodKind) -> Void
    let selectMetric: (UsageMetric) -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            MetricMenu(
                selection: selectedMetric,
                enabledMetrics: enabledMetrics,
                select: selectMetric
            )
            UsagePeriodPicker(
                periods: OverviewSnapshot.ReportingPeriodKind.allCases,
                enabledPeriods: enabledPeriods,
                selection: selectedPeriod,
                select: selectPeriod
            )
            if let presentation {
                UsageBreakdown(
                    presentation: presentation,
                    selectedMetric: selectedMetric
                )
            }
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 12)
        .frame(maxWidth: .infinity, alignment: .leading)
        .cardSurface()
    }
}

private struct UsageBreakdown: View {
    let presentation: UsagePresentation
    let selectedMetric: UsageMetric

    var body: some View {
        if let total {
            HStack(spacing: 14) {
                BreakdownRing(
                    shares: shares,
                    primary: primaryText(total),
                    secondary: selectedMetric == .tokens ? "tokens" : nil,
                    accessibilityLabel: accessibilityLabel(total)
                )
                AccountUsageDetails(
                    shares: shares,
                    selectedMetric: selectedMetric,
                    costFractionDigits: presentation.costFractionDigits
                )
            }
        }
    }

    private var shares: [UsagePresentation.Share] {
        switch selectedMetric {
        case .cost: presentation.accountCostShares
        case .tokens: presentation.accountTokenShares
        }
    }

    private var total: Int64? {
        switch selectedMetric {
        case .cost: presentation.totalCostNanoUSD
        case .tokens: presentation.totalTokens
        }
    }

    private func primaryText(_ total: Int64) -> String {
        switch selectedMetric {
        case .cost: UsageFormatting.compactBaseCost(total)
        case .tokens: UsageFormatting.ringTokens(total)
        }
    }

    private func accessibilityLabel(_ total: Int64) -> String {
        switch selectedMetric {
        case .cost:
            "API-equivalent cost \(UsageFormatting.baseCost(total, fractionDigits: presentation.costFractionDigits))"
        case .tokens:
            UsageFormatting.tokens(total)
        }
    }
}

private struct AccountUsageDetails: View {
    let shares: [UsagePresentation.Share]
    let selectedMetric: UsageMetric
    let costFractionDigits: Int

    var body: some View {
        VStack(alignment: .leading, spacing: 7) {
            ForEach(shares) { share in
                AccountUsageRow(
                    label: share.label,
                    accountNumber: share.accountNumber,
                    value: valueText(for: share)
                )
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private func valueText(for share: UsagePresentation.Share) -> String {
        switch selectedMetric {
        case .cost:
            guard let costNanoUSD = share.costNanoUSD else { return "" }
            return UsageFormatting.baseCost(
                costNanoUSD,
                fractionDigits: costFractionDigits
            )
        case .tokens:
            return UsageFormatting.tokens(share.tokens)
        }
    }
}

private struct AccountUsageRow: View {
    let label: String
    let accountNumber: Int
    let value: String

    var body: some View {
        HStack(spacing: 7) {
            Circle()
                .fill(UsageTheme.accountColour(accountNumber))
                .frame(width: 8, height: 8)
                .accessibilityHidden(true)
            Text(label)
                .font(.caption.weight(.medium))
                .lineLimit(1)
            Spacer(minLength: 4)
            Text(value)
                .font(.caption)
                .foregroundStyle(.secondary)
                .monospacedDigit()
                .lineLimit(1)
        }
    }
}

private struct MetricMenu: View {
    let selection: UsageMetric
    let enabledMetrics: Set<UsageMetric>
    let select: (UsageMetric) -> Void

    var body: some View {
        if enabledMetrics.count == 1 {
            Text(selection.title)
                .font(.subheadline.weight(.semibold))
        } else {
            Menu {
                ForEach(UsageMetric.allCases.filter { enabledMetrics.contains($0) }) { metric in
                    Button {
                        select(metric)
                    } label: {
                        if metric == selection {
                            Label(metric.title, systemImage: "checkmark")
                        } else {
                            Text(metric.title)
                        }
                    }
                }
            } label: {
                HStack(spacing: 4) {
                    Text(selection.title)
                        .font(.subheadline.weight(.semibold))
                    Image(systemName: "chevron.down")
                        .font(.system(size: 8, weight: .semibold))
                        .foregroundStyle(.secondary)
                }
            }
            .menuStyle(.button)
            .buttonStyle(.plain)
            .accessibilityLabel("Usage metric")
            .accessibilityValue(Text(selection.title))
        }
    }
}

private struct BreakdownRing: View {
    let shares: [UsagePresentation.Share]
    let primary: String
    let secondary: LocalizedStringResource?
    let accessibilityLabel: String

    var body: some View {
        ZStack {
            ForEach(CostRingArcLayout.arcs(from: shares)) { arc in
                RingSectorShape(
                    startFraction: arc.startFraction,
                    endFraction: arc.endFraction
                )
                .fill(UsageTheme.accountColour(arc.accountNumber))
            }
            VStack(spacing: 1) {
                Text(primary)
                    .font(.system(size: 14, weight: .semibold, design: .rounded))
                    .monospacedDigit()
                    .lineLimit(1)
                    .minimumScaleFactor(0.8)
                if let secondary {
                    Text(secondary)
                        .font(.system(size: 8.5, weight: .medium))
                        .foregroundStyle(.tertiary)
                }
            }
            .frame(width: 70)
        }
        .frame(width: 104, height: 104)
        .accessibilityElement(children: .combine)
        .accessibilityLabel(accessibilityLabel)
    }
}
