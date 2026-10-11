import Foundation

struct OverviewSnapshot: Equatable, Sendable {
    let menuSnapshot: CSSwitch_MenuSnapshot
    var generatedAt: Date { menuSnapshot.generatedAt.date }

    func reportingPeriod(_ kind: ReportingPeriodKind) -> CSSwitch_ReportingPeriod? {
        menuSnapshot.reportingPeriods.first { $0.kind == kind.protobufKind }
    }

    func supports(_ metric: UsageMetric, period: ReportingPeriodKind) -> Bool {
        guard let observed = reportingPeriod(period), !observed.accounts.isEmpty else { return false }
        return metric == .tokens || observed.accounts.allSatisfy(\.hasCostNanoUsd)
    }

    enum ReportingPeriodKind: CaseIterable, Hashable, Sendable {
        case today, sevenDays, thirtyDays
        var title: LocalizedStringResource {
            switch self {
            case .today: "Today"
            case .sevenDays: "7 Days"
            case .thirtyDays: "30 Days"
            }
        }
        var protobufKind: CSSwitch_ReportingPeriodKind {
            switch self {
            case .today: .reportingPeriodToday
            case .sevenDays: .reportingPeriodSevenDays
            case .thirtyDays: .reportingPeriodThirtyDays
            }
        }
    }
}
