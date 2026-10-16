// Design studies only. Written by AI on 2026.10.17 by model Claude Fable 5.1.
//
// Both directions share one stacking: the account list is the ring's legend
// (each account row carries its spend for the selected period), every account
// shows its allowance in one line per window or one headline line, and the
// top tasks list takes whatever height is left on the screen.
import SwiftUI
@testable import CodexSwitchMenu

enum OneViewDirection: String, CaseIterable {
    /// Every allowance window is one aligned line: label, bar, left, time.
    case ledger = "1-ledger"
    /// Each account shows the window that binds it; the others are one grey line.
    case headline = "2-headline"

    var caption: String {
        switch self {
        case .ledger: "1 · Ledger — one aligned line per allowance window; the account list is the ring's legend; top tasks fill what is left."
        case .headline: "2 · Headline — one bar per account for the window that binds it, its other windows in one grey line; same summary and tasks."
        }
    }
}

enum OneViewLayout {
    static let panelWidth: CGFloat = 360
    static let outerInset: CGFloat = 14
    static let cardInset: CGFloat = 10
    static let sectionGap: CGFloat = 10
    static let rowFont = Font.system(size: 12)
    static let rowEmphasisFont = Font.system(size: 12, weight: .medium)
    static let timeFont = Font.system(size: 11.5)
    static let windowLabelWidth: CGFloat = 46
    static let percentWidth: CGFloat = 62
    static let timeWidth: CGFloat = 110
}

struct OneViewDesign: View {
    let snapshot: CSSwitch_MenuSnapshot
    let direction: OneViewDirection
    let taskRowLimit: Int

    private var overview: OverviewSnapshot { OverviewSnapshot(menuSnapshot: snapshot) }
    private var period: CSSwitch_ReportingPeriod? { overview.reportingPeriod(.today) }

    var body: some View {
        VStack(spacing: OneViewLayout.sectionGap) {
            header
            DesignSummary(snapshot: snapshot)
            ForEach(snapshot.providers, id: \.self) { provider in
                DesignProviderCard(snapshot: snapshot, provider: provider, period: period, direction: direction)
            }
            DesignTaskList(snapshot: snapshot, period: period, limit: taskRowLimit)
            footer
        }
        .padding(.horizontal, OneViewLayout.outerInset)
        .padding(.top, 12 + 10)
        .padding(.bottom, 0)
        .frame(width: OneViewLayout.panelWidth)
        .background(UsageTheme.traySurface, in: DesignPopoverSurface())
        .focusEffectDisabled()
    }

    private var header: some View {
        HStack(spacing: 6) {
            Text("Balancing \(snapshot.accounts.filter(\.offersConfiguredModel).count) accounts")
                .font(UsageTheme.titleFont)
            Spacer()
            let connected = snapshot.connection.newTasksConfigured
            let limited = snapshot.providers.filter { provider in
                !snapshot.accounts.contains { $0.menuProvider == provider && $0.usable }
            }
            let status = !connected ? "Codex not connected" : limited.isEmpty ? "Working" : limited.map(\.title).joined(separator: ", ") + " limited"
            Image(systemName: connected && limited.isEmpty ? "checkmark.circle.fill" : "pause.circle.fill")
                .foregroundStyle(connected && limited.isEmpty ? Color.green : Color.secondary)
            Text(status).font(UsageTheme.detailEmphasisFont)
            Image(systemName: "eye.slash").foregroundStyle(.secondary).padding(.leading, 4)
        }
        .padding(.leading, 4)
    }

    private var footer: some View {
        HStack {
            Text("Updated " + snapshot.generatedAt.date.formatted(date: .omitted, time: .shortened))
                .foregroundStyle(.secondary)
            Spacer()
            Text("Options")
            Image(systemName: "chevron.down").font(.system(size: 9, weight: .medium))
        }
        .font(UsageTheme.detailFont)
        .padding(.horizontal, 4)
        .padding(.vertical, 10)
        .overlay(alignment: .top) { Divider().padding(.horizontal, -OneViewLayout.outerInset) }
    }
}

/// Ring, metric, total and period in one block. The legend is the account
/// list below: same colours, same order, spend on every row.
private struct DesignSummary: View {
    let snapshot: CSSwitch_MenuSnapshot

    var body: some View {
        let overview = OverviewSnapshot(menuSnapshot: snapshot)
        let presentation = UsagePresentation(snapshot: overview, selectedPeriod: .today)
        HStack(spacing: 12) {
            ZStack {
                if let presentation {
                    ForEach(CostRingArcLayout.arcs(from: presentation.accountCostShares)) { arc in
                        RingSectorShape(startFraction: arc.startFraction, endFraction: arc.endFraction)
                            .fill(UsageTheme.accountColour(arc.colourSlot))
                    }
                }
            }
            .frame(width: 58, height: 58)
            VStack(alignment: .leading, spacing: 8) {
                HStack(alignment: .firstTextBaseline) {
                    HStack(spacing: 4) {
                        Text("API-equivalent cost").font(UsageTheme.labelFont)
                        Image(systemName: "chevron.down").font(.system(size: 9, weight: .semibold)).foregroundStyle(.secondary)
                    }
                    Spacer()
                    if let total = presentation?.totalCostNanoUSD {
                        Text(UsageFormatting.cost(total)).font(.system(size: 17, weight: .semibold)).monospacedDigit()
                    }
                }
                UsagePeriodPicker(periods: OverviewSnapshot.ReportingPeriodKind.allCases,
                                  enabledPeriods: Set(OverviewSnapshot.ReportingPeriodKind.allCases), selection: .today, select: { _ in })
            }
        }
        .padding(OneViewLayout.cardInset)
        .cardSurface()
    }
}

/// One card per provider: a header line, then the provider's accounts
/// separated by hairlines, so the account rows share one grid.
private struct DesignProviderCard: View {
    let snapshot: CSSwitch_MenuSnapshot
    let provider: CSSwitch_Provider
    let period: CSSwitch_ReportingPeriod?
    let direction: OneViewDirection

    var body: some View {
        let accounts = snapshot.accounts.filter { $0.menuProvider == provider }
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 6) {
                ProviderMark(provider: provider, size: 15).foregroundStyle(.secondary)
                Text(provider.title).font(UsageTheme.labelFont)
                Spacer()
                let subtotal = accounts.compactMap { account in
                    period?.accounts.first { $0.accountKey == account.key }
                }
                if !subtotal.isEmpty, subtotal.allSatisfy(\.hasCostNanoUsd) {
                    Text(UsageFormatting.cost(subtotal.reduce(0) { $0 + $1.costNanoUsd }))
                        .font(UsageTheme.detailEmphasisFont).foregroundStyle(.secondary).monospacedDigit()
                }
            }
            .padding(.bottom, 6)
            ForEach(Array(accounts.enumerated()), id: \.element.key) { index, account in
                if index > 0 { Divider().padding(.vertical, 6) }
                DesignAccountRows(
                    account: account,
                    colour: snapshot.colour(of: account),
                    total: period?.accounts.first { $0.accountKey == account.key },
                    now: snapshot.generatedAt.date,
                    direction: direction
                )
            }
        }
        .padding(OneViewLayout.cardInset)
        .frame(maxWidth: .infinity, alignment: .leading)
        .cardSurface()
    }
}

private struct DesignAccountRows: View {
    let account: CSSwitch_Account
    let colour: Color
    let total: CSSwitch_AccountTotal?
    let now: Date
    let direction: OneViewDirection

    /// Shortest window first; a model's own limit follows the limit it sits inside.
    private var windows: [CSSwitch_AllowanceWindow] {
        account.allowance.windows.sorted {
            ($0.periodSeconds, $0.modelID, $0.modelName) < ($1.periodSeconds, $1.modelID, $1.modelName)
        }
    }

    /// The window that stops the account first: the earliest to run out,
    /// otherwise the one with the least left.
    private var bindingWindow: CSSwitch_AllowanceWindow? {
        let runningOut = windows.compactMap { window in window.runOutDate(now: now).map { (window, $0) } }
        if let soonest = runningOut.min(by: { $0.1 < $1.1 }) { return soonest.0 }
        return windows.min { $0.freePercent < $1.freePercent }
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 5) {
            HStack(alignment: .firstTextBaseline, spacing: 6) {
                AccountMarker(colour: colour)
                    .alignmentGuide(.firstTextBaseline) { $0[VerticalAlignment.center] + 4 }
                Text("Account \(account.accountNumber)").font(UsageTheme.labelFont)
                Text(account.plan.prefix(1).uppercased() + account.plan.dropFirst())
                    .font(OneViewLayout.rowFont).foregroundStyle(.secondary)
                if account.allowance.hasAvailableResetCredits, account.allowance.availableResetCredits > 0 {
                    Text("\(account.allowance.availableResetCredits) stored resets")
                        .font(OneViewLayout.rowFont).foregroundStyle(.secondary)
                }
                if !account.usable, !account.unavailableReason.isEmpty {
                    Text(account.unavailableReason).font(OneViewLayout.rowFont).foregroundStyle(.secondary).lineLimit(1)
                }
                Spacer(minLength: 4)
                if let total, total.hasCostNanoUsd {
                    Text(UsageFormatting.cost(total.costNanoUsd))
                        .font(OneViewLayout.rowEmphasisFont).monospacedDigit()
                }
            }
            switch direction {
            case .ledger:
                ForEach(windows, id: \.self) { window in
                    DesignWindowLine(window: window, provider: account.menuProvider, now: now)
                }
            case .headline:
                if let binding = bindingWindow {
                    DesignWindowLine(window: binding, provider: account.menuProvider, now: now)
                    let others = windows.filter { $0 != binding }
                    if !others.isEmpty {
                        HStack(spacing: 0) {
                            ForEach(Array(others.enumerated()), id: \.element) { index, window in
                                if index > 0 { Text(" · ").foregroundStyle(.secondary) }
                                Text("\(window.title(for: account.menuProvider)) \(UsageFormatting.percent(window.freePercent)) left")
                                    .foregroundStyle(window.runOutDate(now: now) == nil ? AnyShapeStyle(.secondary) : AnyShapeStyle(UsageTheme.runOutColour))
                            }
                        }
                        .font(OneViewLayout.timeFont).monospacedDigit()
                        .padding(.leading, OneViewLayout.windowLabelWidth + 7)
                    }
                }
            }
        }
    }
}

/// Label, bar, what is left, and the one time that matters: when it runs
/// out if it will, otherwise when it resets.
private struct DesignWindowLine: View {
    let window: CSSwitch_AllowanceWindow
    let provider: CSSwitch_Provider
    let now: Date

    var body: some View {
        let runOut = window.runOutDate(now: now)
        HStack(spacing: 7) {
            Text(window.title(for: provider))
                .font(OneViewLayout.rowEmphasisFont).lineLimit(1)
                .frame(width: OneViewLayout.windowLabelWidth, alignment: .leading)
            GeometryReader { geometry in
                Capsule().fill(.fill.secondary)
                    .overlay(alignment: .leading) {
                        Capsule().fill(runOut == nil ? UsageTheme.capacityColour : UsageTheme.runOutColour)
                            .frame(width: geometry.size.width * min(max(window.freePercent / 100, 0), 1))
                    }
            }
            .frame(height: 5)
            Text("\(UsageFormatting.percent(window.freePercent)) left")
                .font(OneViewLayout.rowEmphasisFont).monospacedDigit().lineLimit(1)
                .frame(width: OneViewLayout.percentWidth, alignment: .trailing)
            Group {
                if let runOut {
                    Text(UsageFormatting.runOutLabel(runOut, now: now)).foregroundStyle(UsageTheme.runOutColour)
                } else if window.hasResetAt {
                    Text(UsageFormatting.resetLabel(window.resetAt.date, now: now)).foregroundStyle(.secondary)
                }
            }
            .font(OneViewLayout.timeFont).monospacedDigit().lineLimit(1)
            .frame(width: OneViewLayout.timeWidth, alignment: .trailing)
        }
    }
}

private struct DesignTaskList: View {
    let snapshot: CSSwitch_MenuSnapshot
    let period: CSSwitch_ReportingPeriod?
    let limit: Int

    private var rows: [DesignTaskRow] {
        var grouped: [String: DesignTaskRow] = [:]
        for task in period?.taskUsage.tasks ?? [] where task.hasTitle && !task.title.isEmpty {
            let accounts = task.accountKeys.compactMap(snapshot.account(key:))
            guard let provider = accounts.first?.menuProvider else { continue }
            let title = task.title.split(whereSeparator: \.isNewline).first.map(String.init) ?? task.title
            let key = "\(provider.rawValue) \(title)"
            var row = grouped[key] ?? DesignTaskRow(id: key, title: title, provider: provider)
            row.cost += task.costNanoUsd
            row.accounts.formUnion(accounts.map(\.key))
            row.lastCompleted = max(row.lastCompleted, task.lastCompletedAt.date)
            grouped[key] = row
        }
        return Array(grouped.values.sorted {
            $0.cost == $1.cost ? $0.lastCompleted > $1.lastCompleted : $0.cost > $1.cost
        }.prefix(limit))
    }

    var body: some View {
        let rows = rows
        if !rows.isEmpty {
            let slotsByProvider = Dictionary(grouping: rows.flatMap { row in row.accounts.map { (row.provider, $0) } }) { $0.0 }
                .mapValues { pairs in
                    Set(pairs.map(\.1)).compactMap(snapshot.account(key:)).sorted { $0.accountNumber < $1.accountNumber }
                }
            let slotCount = slotsByProvider.values.map(\.count).max() ?? 0
            VStack(alignment: .leading, spacing: 4) {
                HStack {
                    Text("Top tasks").font(UsageTheme.labelFont)
                    Spacer()
                    Text("Today").font(UsageTheme.detailFont).foregroundStyle(.secondary)
                }
                .padding(.bottom, 2)
                ForEach(rows) { row in
                    HStack(spacing: 6) {
                        ProviderMark(provider: row.provider, size: 12).foregroundStyle(.secondary)
                        Text(row.title).font(OneViewLayout.rowFont).lineLimit(1)
                        Spacer(minLength: 6)
                        Text(UsageFormatting.cost(row.cost)).font(OneViewLayout.rowEmphasisFont)
                            .foregroundStyle(.secondary).monospacedDigit().fixedSize()
                        HStack(spacing: 4) {
                            ForEach(slotsByProvider[row.provider] ?? [], id: \.key) { account in
                                HStack(spacing: 3) {
                                    AccountMarker(colour: snapshot.colour(of: account), size: 7)
                                    Text("\(account.accountNumber)").font(OneViewLayout.rowEmphasisFont).monospacedDigit()
                                }
                                .frame(width: 20, alignment: .trailing)
                                .opacity(row.accounts.contains(account.key) ? 1 : 0)
                            }
                        }
                        .frame(width: CGFloat(slotCount) * 24 - 4, alignment: .leading)
                    }
                }
            }
            .padding(OneViewLayout.cardInset)
            .frame(maxWidth: .infinity, alignment: .leading)
            .cardSurface()
        }
    }
}

private struct DesignTaskRow: Identifiable {
    let id: String
    let title: String
    let provider: CSSwitch_Provider
    var cost: Int64 = 0
    var accounts: Set<String> = []
    var lastCompleted = Date.distantPast
}

// One fill covers both the chevron and body; no second material peeks out at the corner.
// The live NSPopover leaves hasFullSizeContent at false beside an opaque SwiftUI background.
// This previews a continuous surface; a native popover must verify hasFullSizeContent=true later.
struct DesignPopoverSurface: Shape {
    func path(in rect: CGRect) -> Path {
        let width = rect.width
        let height = rect.height
        var path = Path()
        path.move(to: CGPoint(x: 0, y: 24))
        path.addQuadCurve(to: CGPoint(x: 14, y: 10), control: CGPoint(x: 0, y: 10))
        path.addQuadCurve(to: CGPoint(x: 23, y: 3), control: CGPoint(x: 19, y: 10))
        path.addQuadCurve(to: CGPoint(x: 31, y: 3), control: CGPoint(x: 27, y: -3))
        path.addQuadCurve(to: CGPoint(x: 55, y: 10), control: CGPoint(x: 43, y: 10))
        path.addLine(to: CGPoint(x: width - 14, y: 10))
        path.addQuadCurve(to: CGPoint(x: width, y: 24), control: CGPoint(x: width, y: 10))
        path.addLine(to: CGPoint(x: width, y: height - 14))
        path.addQuadCurve(to: CGPoint(x: width - 14, y: height), control: CGPoint(x: width, y: height))
        path.addLine(to: CGPoint(x: 14, y: height))
        path.addQuadCurve(to: CGPoint(x: 0, y: height - 14), control: CGPoint(x: 0, y: height))
        path.closeSubpath()
        return path
    }
}

extension CSSwitch_MenuSnapshot {
    /// The real snapshot with one more Claude account: the last Claude account
    /// again, under the next number, with the same totals and days.
    func withSixthAccount() -> CSSwitch_MenuSnapshot {
        guard let source = accounts.last(where: { $0.menuProvider == .claude }),
              let sourceIndex = accounts.lastIndex(of: source) else { return self }
        var copy = self
        var added = source
        added.key = source.key + "-design-copy"
        added.accountNumber = (accounts.filter { $0.menuProvider == .claude }.map(\.accountNumber).max() ?? 0) + 1
        copy.accounts.insert(added, at: sourceIndex + 1)
        for index in copy.reportingPeriods.indices {
            if var total = copy.reportingPeriods[index].accounts.first(where: { $0.accountKey == source.key }) {
                total.accountKey = added.key
                copy.reportingPeriods[index].accounts.append(total)
            }
        }
        for day in accountDays where day.accountKey == source.key {
            var copied = day
            copied.accountKey = added.key
            copy.accountDays.append(copied)
        }
        return copy
    }
}
