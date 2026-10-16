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
        .padding(UsageTheme.cardInset)
        .frame(maxWidth: .infinity, alignment: .leading)
        .cardSurface()
    }
}

/// The ring with the period's total inside, and one legend line per account
/// beside it: a single column up to three accounts, two columns beyond.
private struct UsageBreakdown: View {
    let presentation: UsagePresentation
    let selectedMetric: UsageMetric

    var body: some View {
        if let total {
            let shares = shares
            let columns = shares.count > 3 ? 2 : 1
            let rows = (shares.count + columns - 1) / columns
            HStack(spacing: 8) {
                BreakdownRing(
                    shares: shares,
                    primary: selectedMetric == .cost && presentation.hasMultipleProviders ? UsageFormatting.ringCost(total) : format(total),
                    unit: selectedMetric == .tokens ? "tokens" : nil,
                    accessibilityLabel: "\(selectedMetric.title) \(format(total))"
                )
                HStack(alignment: .top, spacing: 12) {
                    ForEach(0..<columns, id: \.self) { column in
                        VStack(alignment: .leading, spacing: 6) {
                            ForEach(shares.dropFirst(column * rows).prefix(rows)) { share in
                                HStack(spacing: 5) {
                                    AccountMarker(colour: UsageTheme.accountColour(share.colourSlot), size: 7)
                                    Text(share.label)
                                        .font(UsageTheme.detailFont)
                                        .lineLimit(1)
                                    Spacer(minLength: 4)
                                    Text(value(share))
                                        .font(UsageTheme.detailFont)
                                        .foregroundStyle(.secondary)
                                        .monospacedDigit()
                                        .lineLimit(1)
                                }
                            }
                        }
                        .frame(maxWidth: .infinity, alignment: .leading)
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
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

    private func format(_ amount: Int64) -> String {
        switch selectedMetric {
        case .cost: UsageFormatting.cost(amount)
        case .tokens: UsageFormatting.tokens(amount)
        }
    }

    private func value(_ share: UsagePresentation.Share) -> String {
        switch selectedMetric {
        case .cost: share.costNanoUSD.map { UsageFormatting.cost($0) } ?? ""
        case .tokens: UsageFormatting.tokens(share.tokens)
        }
    }
}

private struct MetricMenu: View {
    let selection: UsageMetric
    let enabledMetrics: Set<UsageMetric>
    let select: (UsageMetric) -> Void

    var body: some View {
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
                    .font(UsageTheme.labelFont)
                    .foregroundStyle(.primary)
                Image(systemName: "chevron.down")
                    .font(.system(size: 9, weight: .semibold))
                    .foregroundStyle(.secondary)
            }
        }
        .menuStyle(.button)
        .buttonStyle(.plain)
        .fixedSize()
        .accessibilityLabel("Usage metric")
        .accessibilityValue(Text(selection.title))
    }
}

private struct BreakdownRing: View {
    let shares: [UsagePresentation.Share]
    let primary: String
    let unit: LocalizedStringResource?
    let accessibilityLabel: String

    var body: some View {
        ZStack {
            ForEach(CostRingArcLayout.arcs(from: shares)) { arc in
                RingSectorShape(
                    startFraction: arc.startFraction,
                    endFraction: arc.endFraction
                )
                .fill(UsageTheme.accountColour(arc.colourSlot))
            }
            VStack(spacing: 0) {
                Text(primary)
                    .font(.system(size: 11, weight: .semibold, design: .rounded))
                    .monospacedDigit()
                    .lineLimit(1)
                    .minimumScaleFactor(0.6)
                if let unit {
                    Text(unit)
                        .font(.system(size: 8, weight: .medium))
                        .foregroundStyle(.tertiary)
                }
            }
            .frame(width: 36)
        }
        .frame(width: 62, height: 62)
        .accessibilityElement(children: .combine)
        .accessibilityLabel(accessibilityLabel)
    }
}
