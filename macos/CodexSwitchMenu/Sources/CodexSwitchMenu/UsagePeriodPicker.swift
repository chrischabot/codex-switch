import SwiftUI

struct UsagePeriodPicker: View {
    let periods: [OverviewSnapshot.ReportingPeriodKind]
    let enabledPeriods: Set<OverviewSnapshot.ReportingPeriodKind>
    let selection: OverviewSnapshot.ReportingPeriodKind
    let select: (OverviewSnapshot.ReportingPeriodKind) -> Void

    var body: some View {
        HStack(spacing: 2) {
            ForEach(periods, id: \.self) { period in
                UsagePeriodButton(
                    period: period,
                    isSelected: period == selection,
                    isEnabled: enabledPeriods.contains(period)
                ) {
                    select(period)
                }
            }
        }
        .padding(3)
        .background(.quinary, in: Capsule())
        .frame(maxWidth: .infinity)
    }
}

private struct UsagePeriodButton: View {
    let period: OverviewSnapshot.ReportingPeriodKind
    let isSelected: Bool
    let isEnabled: Bool
    let select: () -> Void

    var body: some View {
        Button(action: select) {
            Text(period.title)
                .font(.system(size: 11, weight: isSelected ? .semibold : .medium))
                .foregroundStyle(isSelected ? AnyShapeStyle(.primary) : AnyShapeStyle(.secondary))
                .padding(.horizontal, 12)
                .padding(.vertical, 4)
                .frame(maxWidth: .infinity)
                .contentShape(Capsule())
        }
        .buttonStyle(.plain)
        .disabled(!isEnabled)
        .background {
            if isSelected {
                Capsule()
                    .fill(.background)
                    .shadow(color: .black.opacity(0.12), radius: 2, y: 1)
            }
        }
        .accessibilityAddTraits(isSelected ? .isSelected : [])
    }
}
