// Design studies only. Written by AI on 2026.10.17 by model GPT-6.1 Sol.
import SwiftUI
@testable import CodexSwitchMenu

enum OneViewArrangement: String, CaseIterable {
    case compactList = "a-compact-list"
    case accountCards = "b-account-cards"
    case providerColumns = "c-provider-columns"

    var width: CGFloat {
        switch self {
        case .compactList: 460
        case .accountCards: 720
        case .providerColumns: 720
        }
    }

    var caption: String {
        switch self {
        case .compactList: "A · Compact list — one familiar column; allowance windows sit side by side within each account."
        case .accountCards: "B · Account cards — one row per provider, with account cards arranged across the panel."
        case .providerColumns: "C · Provider columns — Codex and Claude side by side, with accounts stacked beneath each provider."
        }
    }
}

struct OneViewDesign: View {
    let snapshot: CSSwitch_MenuSnapshot
    let arrangement: OneViewArrangement

    private var overview: OverviewSnapshot { OverviewSnapshot(menuSnapshot: snapshot) }
    private var period: CSSwitch_ReportingPeriod? { overview.reportingPeriod(.today) }

    var body: some View {
        VStack(spacing: 8) {
            header
            DesignUsageSummary(snapshot: snapshot)
            switch arrangement {
            case .compactList:
                ForEach(snapshot.providers, id: \.self) { provider in
                    providerSection(provider, horizontalAccounts: false, horizontalWindows: true)
                }
            case .accountCards:
                ForEach(snapshot.providers, id: \.self) { provider in
                    providerSection(provider, horizontalAccounts: true, horizontalWindows: false)
                }
            case .providerColumns:
                HStack(alignment: .top, spacing: 14) {
                    ForEach(snapshot.providers, id: \.self) { provider in
                        providerSection(provider, horizontalAccounts: false, horizontalWindows: false)
                            .frame(maxWidth: .infinity, alignment: .topLeading)
                    }
                }
            }
            DesignTaskList(snapshot: snapshot, period: period)
            HStack {
                Text("Updated " + snapshot.generatedAt.date.formatted(date: .omitted, time: .shortened))
                    .foregroundStyle(.secondary)
                Spacer()
                Text("Options").foregroundStyle(.primary)
                Image(systemName: "chevron.down").font(.system(size: 9, weight: .medium))
            }
            .font(UsageTheme.detailFont)
            .padding(.top, 8)
            .overlay(alignment: .top) { Divider() }
        }
        .padding(14)
        .padding(.top, 10)
        .frame(width: arrangement.width)
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
    }

    private func providerSection(_ provider: CSSwitch_Provider, horizontalAccounts: Bool, horizontalWindows: Bool) -> some View {
        let accounts = snapshot.accounts.filter { $0.menuProvider == provider }
        return VStack(alignment: .leading, spacing: 8) {
            ProviderHeader(provider: provider)
            if horizontalAccounts {
                HStack(alignment: .top, spacing: 10) {
                    ForEach(accounts, id: \.key) { account in
                        accountCard(account, horizontalWindows: horizontalWindows)
                            .frame(maxWidth: .infinity, alignment: .topLeading)
                    }
                }
            } else {
                ForEach(accounts, id: \.key) { account in
                    accountCard(account, horizontalWindows: horizontalWindows)
                }
            }
        }
    }

    private func accountCard(_ account: CSSwitch_Account, horizontalWindows: Bool) -> some View {
        DesignAccountCard(
            account: account,
            colour: snapshot.colour(of: account),
            total: period?.accounts.first { $0.accountKey == account.key },
            now: snapshot.generatedAt.date,
            horizontalWindows: horizontalWindows,
            storedResetsInHeader: arrangement != .accountCards
        )
    }
}

private struct DesignAccountCard: View {
    let account: CSSwitch_Account
    let colour: Color
    let total: CSSwitch_AccountTotal?
    let now: Date
    let horizontalWindows: Bool
    let storedResetsInHeader: Bool

    var body: some View {
        let windows = account.allowance.windows.sorted {
            ($0.periodSeconds, $0.modelID, $0.modelName) < ($1.periodSeconds, $1.modelID, $1.modelName)
        }
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 6) {
                AccountMarker(colour: colour)
                Text("Account \(account.accountNumber)").font(UsageTheme.labelFont).fixedSize()
                Text(account.plan.prefix(1).uppercased() + account.plan.dropFirst())
                    .font(UsageTheme.detailFont).foregroundStyle(.secondary)
                if storedResetsInHeader && account.allowance.hasAvailableResetCredits && account.allowance.availableResetCredits > 0 {
                    Text("\(account.allowance.availableResetCredits) stored resets")
                        .font(UsageTheme.detailFont).foregroundStyle(.secondary)
                }
                Spacer(minLength: 2)
                if let total, total.hasCostNanoUsd {
                    Text(UsageFormatting.cost(total.costNanoUsd))
                        .font(UsageTheme.detailEmphasisFont).monospacedDigit()
                }
            }
            if horizontalWindows && windows.count == 1, let window = windows.first {
                HStack(spacing: 8) {
                    Text(window.title(for: account.menuProvider)).font(UsageTheme.detailEmphasisFont)
                    capacityBar(window)
                    Text("\(UsageFormatting.percent(window.freePercent)) left")
                        .font(UsageTheme.detailEmphasisFont).monospacedDigit()
                }
                HStack {
                    if window.hasResetAt {
                        Text(UsageFormatting.resetLabel(window.resetAt.date, now: now))
                            .foregroundStyle(.secondary)
                    }
                    Spacer(minLength: 4)
                    if let runOut = window.runOutDate(now: now) {
                        Text(UsageFormatting.runOutLabel(runOut, now: now)).foregroundStyle(UsageTheme.runOutColour)
                    }
                }
                .font(UsageTheme.detailFont).monospacedDigit()
            } else if horizontalWindows {
                HStack(alignment: .top, spacing: 12) {
                    ForEach(windows, id: \.self) { window in
                        DesignAllowance(window: window, provider: account.menuProvider, now: now, compact: true)
                            .frame(maxWidth: .infinity, alignment: .leading)
                    }
                }
            } else {
                ForEach(windows, id: \.self) { window in
                    DesignAllowance(window: window, provider: account.menuProvider, now: now, compact: false)
                }
            }
            if !storedResetsInHeader && account.allowance.hasAvailableResetCredits && account.allowance.availableResetCredits > 0 {
                Text("\(account.allowance.availableResetCredits) stored resets")
                    .font(UsageTheme.detailFont).foregroundStyle(.secondary)
            }
            if !account.usable && !account.unavailableReason.isEmpty {
                Text(account.unavailableReason).font(UsageTheme.detailFont).foregroundStyle(.secondary)
            }
        }
        .padding(8)
        .frame(maxWidth: .infinity, alignment: .leading)
        .cardSurface()
    }

    private func capacityBar(_ window: CSSwitch_AllowanceWindow) -> some View {
        GeometryReader { geometry in
            Capsule().fill(.fill.secondary)
                .overlay(alignment: .leading) {
                    Capsule().fill(window.runOutDate(now: now) == nil ? UsageTheme.capacityColour : UsageTheme.runOutColour)
                        .frame(width: geometry.size.width * min(max(window.freePercent / 100, 0), 1))
                }
        }
        .frame(height: 4)
    }
}

private struct DesignTaskList: View {
    let snapshot: CSSwitch_MenuSnapshot
    let period: CSSwitch_ReportingPeriod?

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
        }.prefix(10))
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            HStack {
                Text("Top tasks").font(UsageTheme.labelFont)
                Spacer()
                Text("Today").font(UsageTheme.detailFont).foregroundStyle(.secondary)
            }.padding(.bottom, 3)
            ForEach(rows) { row in
                HStack(spacing: 6) {
                    ProviderMark(provider: row.provider, size: 12).foregroundStyle(.secondary)
                    Text(row.title).font(UsageTheme.detailFont).lineLimit(1)
                    Spacer(minLength: 0)
                    Text(UsageFormatting.cost(row.cost)).font(UsageTheme.detailEmphasisFont)
                        .foregroundStyle(.secondary).monospacedDigit().fixedSize()
                    HStack(spacing: 4) {
                        ForEach(snapshot.accounts.filter { $0.menuProvider == row.provider }, id: \.key) { account in
                            HStack(spacing: 3) {
                                AccountMarker(colour: snapshot.colour(of: account), size: 6)
                                Text("\(account.accountNumber)").font(UsageTheme.detailEmphasisFont)
                            }.frame(width: 20).opacity(row.accounts.contains(account.key) ? 1 : 0)
                        }
                    }
                }
            }
        }.padding(10).cardSurface()
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

private struct DesignAllowance: View {
    let window: CSSwitch_AllowanceWindow
    let provider: CSSwitch_Provider
    let now: Date
    let compact: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(alignment: .firstTextBaseline, spacing: 4) {
                Text(window.title(for: provider)).font(.system(size: 12, weight: .medium))
                Spacer(minLength: 0)
                Text("\(UsageFormatting.percent(window.freePercent)) left")
                    .font(.system(size: 12, weight: .semibold)).monospacedDigit()
            }
            GeometryReader { geometry in
                Capsule().fill(.fill.secondary)
                    .overlay(alignment: .leading) {
                        Capsule().fill(window.runOutDate(now: now) == nil ? UsageTheme.capacityColour : UsageTheme.runOutColour)
                            .frame(width: geometry.size.width * min(max(window.freePercent / 100, 0), 1))
                    }
            }
            .frame(height: 4)
            if window.hasResetAt {
                Text(UsageFormatting.resetLabel(window.resetAt.date, now: now))
                    .font(.system(size: compact ? 11 : 12)).foregroundStyle(.secondary)
                    .monospacedDigit().fixedSize(horizontal: false, vertical: true)
            }
            if let runOut = window.runOutDate(now: now) {
                Text(UsageFormatting.runOutLabel(runOut, now: now))
                    .font(.system(size: compact ? 11 : 12)).foregroundStyle(UsageTheme.runOutColour)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
    }
}

private struct DesignUsageSummary: View {
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
            .frame(width: 64, height: 64)
            VStack(alignment: .leading, spacing: 7) {
                HStack(alignment: .firstTextBaseline) {
                    HStack(spacing: 4) {
                        Text("API-equivalent cost").font(UsageTheme.detailEmphasisFont)
                        Image(systemName: "chevron.down").font(.system(size: 9))
                    }
                    Spacer()
                    if let total = presentation?.totalCostNanoUSD {
                        Text(UsageFormatting.cost(total)).font(.system(size: 19, weight: .semibold)).monospacedDigit()
                    }
                }
                UsagePeriodPicker(periods: OverviewSnapshot.ReportingPeriodKind.allCases,
                                  enabledPeriods: Set(OverviewSnapshot.ReportingPeriodKind.allCases), selection: .today, select: { _ in })
            }
        }
        .padding(10)
        .cardSurface()
    }
}

// One fill covers both the chevron and body; no second material peeks out at the corner.
// The live NSPopover leaves hasFullSizeContent at false beside an opaque SwiftUI background.
// This previews a continuous surface; a native popover must verify hasFullSizeContent=true later.
private struct DesignPopoverSurface: Shape {
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
