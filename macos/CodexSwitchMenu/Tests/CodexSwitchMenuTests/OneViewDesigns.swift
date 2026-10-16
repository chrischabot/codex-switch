// Design studies only. Written by AI on 2026.10.17 by model Claude Fable 5.1.
//
// OpenUsage's language for an allowance window, stacked for five or six
// accounts: a semibold label, a red flame with "Limit in 9h 42m" only when
// the window will run out, a full-width bar with an even-pace tick, then
// "15% left" and "Resets in 4d 17h" in quiet grey. Relative times only; red
// only where a limit will be hit. The two directions differ in one thing:
// how many lines of text a window block spends.
import SwiftUI
@testable import CodexSwitchMenu

enum OneViewDirection: String, CaseIterable {
    /// One text line per window (state left, time right) above the bar.
    case compact = "a-compact"
    /// OpenUsage's block as it is: label line, bar, detail line.
    case faithful = "b-faithful"

    var caption: String {
        switch self {
        case .compact: "A · Compact — OpenUsage's window block folded to one text line above the bar; top tasks take the height that is left."
        case .faithful: "B · Faithful — OpenUsage's window block as it is (label line, bar, detail line); fewer top tasks fit."
        }
    }
}

enum OneViewLayout {
    static let panelWidth: CGFloat = 360
    static let outerInset: CGFloat = 14
    static let cardInset: CGFloat = 11
    static let sectionGap: CGFloat = 10
    static let labelFont = Font.system(size: 13, weight: .semibold)
    static let supportingFont = Font.system(size: 12)
    static let barHeight: CGFloat = 5
    static let windowInnerSpacing: CGFloat = 4
        static let accountGap: CGFloat = 7
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
                DesignProviderSection(snapshot: snapshot, provider: provider, period: period, direction: direction)
            }
            if taskRowLimit > 0 {
                DesignTaskList(snapshot: snapshot, period: period, limit: taskRowLimit)
            }
            footer
        }
        .padding(.horizontal, OneViewLayout.outerInset)
        .padding(.top, 12 + 10)
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
/// list below: the same colours in the same order, with each account's
/// spend on its own line.
private struct DesignSummary: View {
    let snapshot: CSSwitch_MenuSnapshot

    var body: some View {
        let overview = OverviewSnapshot(menuSnapshot: snapshot)
        let presentation = UsagePresentation(snapshot: overview, selectedPeriod: .today)
        HStack(spacing: 14) {
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
                        Text("API-equivalent cost").font(OneViewLayout.labelFont)
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

/// The provider's name and spend above one grouped card of its accounts,
/// as OpenUsage puts a provider's name above its card.
private struct DesignProviderSection: View {
    let snapshot: CSSwitch_MenuSnapshot
    let provider: CSSwitch_Provider
    let period: CSSwitch_ReportingPeriod?
    let direction: OneViewDirection

    var body: some View {
        let accounts = snapshot.accounts.filter { $0.menuProvider == provider }
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 7) {
                ProviderMark(provider: provider, size: 16).foregroundStyle(.secondary)
                Text(provider.title).font(UsageTheme.titleFont)
                Spacer()
                let subtotal = accounts.compactMap { account in
                    period?.accounts.first { $0.accountKey == account.key }
                }
                if !subtotal.isEmpty, subtotal.allSatisfy(\.hasCostNanoUsd) {
                    Text(UsageFormatting.cost(subtotal.reduce(0) { $0 + $1.costNanoUsd }))
                        .font(OneViewLayout.supportingFont).foregroundStyle(.secondary).monospacedDigit()
                }
            }
            .padding(.horizontal, 4)
            VStack(alignment: .leading, spacing: 0) {
                ForEach(Array(accounts.enumerated()), id: \.element.key) { index, account in
                    if index > 0 { Divider().padding(.vertical, OneViewLayout.accountGap) }
                    DesignAccount(
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
}

private struct DesignAccount: View {
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

    var body: some View {
        VStack(alignment: .leading, spacing: 7) {
            HStack(alignment: .firstTextBaseline, spacing: 6) {
                AccountMarker(colour: colour)
                    .alignmentGuide(.firstTextBaseline) { $0[VerticalAlignment.center] + 4 }
                Text("Account \(account.accountNumber)").font(OneViewLayout.labelFont)
                Text(accountDetail).font(OneViewLayout.supportingFont).foregroundStyle(.secondary).lineLimit(1)
                Spacer(minLength: 4)
                if let total, total.hasCostNanoUsd {
                    Text(UsageFormatting.cost(total.costNanoUsd))
                        .font(OneViewLayout.supportingFont).monospacedDigit()
                }
            }
            ForEach(windows, id: \.self) { window in
                DesignWindowBlock(window: window, provider: account.menuProvider, now: now, direction: direction)
            }
        }
    }

    /// "Pro · 3 stored resets", "Max 20x", or why the account is out of the pool.
    private var accountDetail: String {
        var parts: [String] = []
        if !account.plan.isEmpty { parts.append(account.plan.prefix(1).uppercased() + account.plan.dropFirst()) }
        if account.allowance.hasAvailableResetCredits, account.allowance.availableResetCredits > 0 {
            parts.append("\(account.allowance.availableResetCredits) stored resets")
        }
        if !account.usable, !account.unavailableReason.isEmpty { parts.append(account.unavailableReason) }
        return parts.joined(separator: " · ")
    }
}

/// OpenUsage's window block. Compact: "Weekly  15% left" left and the time
/// right, bar below. Faithful: label and flame line, bar, "15% left" and
/// reset line.
private struct DesignWindowBlock: View {
    let window: CSSwitch_AllowanceWindow
    let provider: CSSwitch_Provider
    let now: Date
    let direction: OneViewDirection

    var body: some View {
        let runOut = window.runOutDate(now: now)
        VStack(alignment: .leading, spacing: OneViewLayout.windowInnerSpacing) {
            switch direction {
            case .compact:
                HStack(alignment: .firstTextBaseline, spacing: 8) {
                    Text(window.title(for: provider)).font(OneViewLayout.labelFont)
                    Text("\(UsageFormatting.percent(window.freePercent)) left").font(OneViewLayout.supportingFont).monospacedDigit()
                    Spacer()
                    if let runOut {
                        limitLabel(runOut)
                    } else {
                        resetLabel
                    }
                }
                bar(runOut: runOut)
            case .faithful:
                HStack(alignment: .firstTextBaseline, spacing: 8) {
                    Text(window.title(for: provider)).font(OneViewLayout.labelFont)
                    Spacer()
                    if let runOut { limitLabel(runOut) }
                }
                bar(runOut: runOut)
                HStack(alignment: .firstTextBaseline) {
                    Text("\(UsageFormatting.percent(window.freePercent)) left").font(OneViewLayout.supportingFont).monospacedDigit()
                    Spacer()
                    resetLabel
                }
            }
        }
    }

    private func limitLabel(_ runOut: Date) -> some View {
        HStack(spacing: 4) {
            Image(systemName: "flame.fill").font(.system(size: 11)).foregroundStyle(UsageTheme.runOutColour)
            Text("Limit in " + DesignDuration.compact(runOut.timeIntervalSince(now)))
                .font(OneViewLayout.supportingFont).foregroundStyle(.secondary).monospacedDigit()
        }
    }

    @ViewBuilder
    private var resetLabel: some View {
        if window.hasResetAt {
            Text("Resets in " + DesignDuration.compact(window.resetAt.date.timeIntervalSince(now)))
                .font(OneViewLayout.supportingFont).foregroundStyle(.secondary).monospacedDigit()
        }
    }

    /// Full-width capsule: blue while the window lasts to its reset, red when
    /// it will run out, with a tick where even pace would leave it now.
    private func bar(runOut: Date?) -> some View {
        GeometryReader { geometry in
            let track = geometry.size.width
            ZStack(alignment: .leading) {
                Capsule().fill(.fill.secondary)
                Capsule().fill(runOut == nil ? UsageTheme.capacityColour : UsageTheme.runOutColour)
                    .frame(width: max(OneViewLayout.barHeight, track * min(max(window.freePercent / 100, 0), 1)))
            }
            .overlay(alignment: .leading) {
                if runOut != nil, let pace = evenPaceFreeFraction {
                    RoundedRectangle(cornerRadius: 1)
                        .fill(Color.primary.opacity(0.55))
                        .frame(width: 2, height: OneViewLayout.barHeight + 4)
                        .offset(x: min(max(track * pace - 1, 0), track - 2))
                }
            }
        }
        .frame(height: OneViewLayout.barHeight)
    }

    /// Where the bar would stand now if the window were spent evenly.
    private var evenPaceFreeFraction: Double? {
        guard window.hasResetAt, window.periodSeconds > 0 else { return nil }
        let elapsed = now.timeIntervalSince(window.resetAt.date.addingTimeInterval(-TimeInterval(window.periodSeconds)))
        return 1 - min(max(elapsed / TimeInterval(window.periodSeconds), 0), 1)
    }
}

/// "4d 17h", "9h 42m", "12m": two units at the day scale, minutes below it.
enum DesignDuration {
    static func compact(_ seconds: TimeInterval) -> String {
        let minutes = max(1, Int((max(seconds, 0) / 60).rounded(.up)))
        let days = minutes / 1_440
        let hours = minutes % 1_440 / 60
        if days > 0 { return "\(days)d \(hours)h" }
        if hours > 0 { return "\(hours)h \(minutes % 60)m" }
        return "\(minutes)m"
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
                HStack(alignment: .firstTextBaseline) {
                    Text("Top tasks").font(OneViewLayout.labelFont)
                    Spacer()
                    Text("Today").font(OneViewLayout.supportingFont).foregroundStyle(.secondary)
                }
                .padding(.bottom, 2)
                ForEach(rows) { row in
                    HStack(spacing: 7) {
                        ProviderMark(provider: row.provider, size: 12).foregroundStyle(.secondary)
                        Text(row.title).font(OneViewLayout.supportingFont).lineLimit(1)
                        Spacer(minLength: 6)
                        Text(UsageFormatting.cost(row.cost)).font(OneViewLayout.supportingFont)
                            .foregroundStyle(.secondary).monospacedDigit().fixedSize()
                        HStack(spacing: 4) {
                            ForEach(slotsByProvider[row.provider] ?? [], id: \.key) { account in
                                HStack(spacing: 3) {
                                    AccountMarker(colour: snapshot.colour(of: account), size: 7)
                                    Text("\(account.accountNumber)").font(OneViewLayout.supportingFont).monospacedDigit()
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
