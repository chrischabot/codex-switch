import SwiftUI

/// A provider's accounts in one card, organised by allowance window: first
/// who the accounts are, one line each, then one block per window with one
/// grid row per account, so one window is read across every account at
/// once and a new account adds one row per block. Every row in every block
/// sits on the same grid, and every group on the same left edge.
struct ProviderSection: View {
    let snapshot: CSSwitch_MenuSnapshot
    let provider: CSSwitch_Provider
    let hideIdentity: Bool
    let now: Date
    @Environment(\.panelDensity) private var density

    var body: some View {
        let accounts = snapshot.accounts.filter { $0.menuProvider == provider }.sorted { $0.accountNumber < $1.accountNumber }
        VStack(alignment: .leading, spacing: 6) {
            ProviderHeader(provider: provider)
                .padding(.horizontal, density.cardInset)
            VStack(alignment: .leading, spacing: density.groupGap) {
                AccountIdentities(accounts: accounts, snapshot: snapshot, hideIdentity: hideIdentity)
                ForEach(IndicatorBlock.blocks(for: accounts, provider: provider)) { block in
                    VStack(alignment: .leading, spacing: density.blockTitleGap) {
                        Text(block.title)
                            .font(UsageTheme.labelFont)
                        Grid(alignment: .leading, horizontalSpacing: density.columnGap, verticalSpacing: density.rowGap) {
                            ForEach(block.rows, id: \.account.key) { row in
                                AllowanceGridRow(
                                    number: Int(row.account.accountNumber),
                                    colour: snapshot.colour(of: row.account),
                                    allowance: row.window,
                                    now: now
                                )
                            }
                        }
                    }
                }
            }
            .padding(density.cardInset)
            .frame(maxWidth: .infinity, alignment: .leading)
            .cardSurface()
        }
    }
}

/// One line per account: its colour, number, address (censored in privacy
/// mode, never cut), plan, stored resets, and the one state that keeps new
/// work off it when there is one, in the proxy's words. The dot sits on the
/// same edge as the dots in the rows below.
private struct AccountIdentities: View {
    let accounts: [CSSwitch_Account]
    let snapshot: CSSwitch_MenuSnapshot
    let hideIdentity: Bool
    @Environment(\.panelDensity) private var density

    var body: some View {
        VStack(alignment: .leading, spacing: density.identityGap) {
            ForEach(accounts, id: \.key) { account in
                HStack(alignment: .firstTextBaseline, spacing: 0) {
                    HStack(spacing: 3) {
                        AccountMarker(colour: snapshot.colour(of: account), size: 7)
                        Text("\(account.accountNumber)")
                            .font(density.rowEmphasisFont)
                            .monospacedDigit()
                    }
                    .frame(width: density.numberWidth + density.columnGap, alignment: .leading)
                    Text(detail(account))
                        .font(density.identityFont)
                        .foregroundStyle(.secondary)
                        .fixedSize(horizontal: false, vertical: true)
                }
            }
        }
    }

    /// "josh@example.com · Pro · 3 stored resets", then any state: a reserve,
    /// a limit reached, or "not in pool" for an account whose plan cannot
    /// run the configured model.
    private func detail(_ account: CSSwitch_Account) -> String {
        var parts = [hideIdentity ? UsageFormatting.privateEmail(account.email) : account.email, account.planTitle]
        if account.allowance.hasAvailableResetCredits, account.allowance.availableResetCredits > 0 {
            let count = account.allowance.availableResetCredits
            parts.append(count == 1 ? "1 stored reset" : "\(count) stored resets")
        }
        if !account.offersConfiguredModel {
            parts.append("not in pool")
        } else if !account.usable {
            parts.append(account.unavailableReason)
        } else {
            parts.append(account.reserveStatus)
        }
        return parts.filter { !$0.isEmpty }.joined(separator: " · ")
    }
}

/// One allowance window across the provider's pool accounts.
struct IndicatorBlock: Identifiable {
    struct Row {
        let account: CSSwitch_Account
        let window: CSSwitch_AllowanceWindow
    }

    let id: String
    let title: String
    let rows: [Row]

    /// Shortest window first; a model's own limit follows the limit it sits
    /// inside. Accounts keep their number order within every block. An
    /// account that cannot run the configured model has no rows: its
    /// allowance is not the pool's, and its identity line says so.
    static func blocks(for accounts: [CSSwitch_Account], provider: CSSwitch_Provider) -> [IndicatorBlock] {
        var rowsByKey: [String: [Row]] = [:]
        var windowsByKey: [String: CSSwitch_AllowanceWindow] = [:]
        for account in accounts where account.hasAllowance && account.offersConfiguredModel {
            for window in account.allowance.windows {
                let key = "\(window.periodSeconds) \(window.modelID) \(window.modelName)"
                rowsByKey[key, default: []].append(Row(account: account, window: window))
                windowsByKey[key] = window
            }
        }
        return windowsByKey.sorted { left, right in
            (left.value.periodSeconds, left.value.modelID, left.value.modelName)
                < (right.value.periodSeconds, right.value.modelID, right.value.modelName)
        }.map { key, window in
            IndicatorBlock(id: key, title: window.title(for: provider), rows: rowsByKey[key] ?? [])
        }
    }
}

extension CSSwitch_AllowanceWindow {
    var freePercent: Double { 100 - usedPercent }

    /// "Session", "Weekly", or the model the limit belongs to, such as "Fable".
    func title(for provider: CSSwitch_Provider) -> String {
        guard modelID.isEmpty && modelName.isEmpty else { return modelName.isEmpty ? modelID : modelName }
        return switch periodSeconds {
        case 18_000 where provider == .claude: "Session"
        case 604_800: "Weekly"
        case let seconds where seconds % 86_400 == 0: "\(seconds / 86_400)-day"
        case let seconds where seconds % 3_600 == 0: "\(seconds / 3_600)-hour"
        default: "\(periodSeconds / 60)-minute"
        }
    }

    /// When the allowance empties if use continues at this window's average
    /// pace so far; nil when it lasts until the reset. The pace is read only
    /// once a seventh of the window has passed (a day of a week): an hour into
    /// a week, one busy session would project the whole week.
    func runOutDate(now: Date) -> Date? {
        guard hasResetAt, usedPercent > 0, usedPercent < 100 else { return nil }
        let reset = resetAt.date
        let elapsed = now.timeIntervalSince(reset.addingTimeInterval(-TimeInterval(periodSeconds)))
        guard elapsed >= TimeInterval(periodSeconds) / 7 else { return nil }
        let runOut = now.addingTimeInterval(freePercent / usedPercent * elapsed)
        return runOut < reset ? runOut : nil
    }

    /// Nothing left to use before the reset.
    var isEmpty: Bool { usedPercent >= 100 }
}

extension CSSwitch_Account {
    /// "Pro", "Max 20x".
    var planTitle: String { plan.prefix(1).uppercased() + plan.dropFirst() }
}

/// One account's line in a window block: its colour and number, the bar,
/// what is left, and the next time that changes its state: a red flame and
/// "Limit in 9h 55m" if it will run out first, otherwise "Resets in 44m".
/// An empty window says so in red; a window with no room to spare is red
/// too. The reset of a running-out window is on hover.
private struct AllowanceGridRow: View {
    let number: Int
    let colour: Color
    let allowance: CSSwitch_AllowanceWindow
    let now: Date
    @Environment(\.panelDensity) private var density

    var body: some View {
        let runOut = allowance.runOutDate(now: now)
        let isLow = allowance.isEmpty || runOut != nil || allowance.freePercent < 10
        GridRow {
            HStack(spacing: 3) {
                AccountMarker(colour: colour, size: 7)
                Text("\(number)")
                    .font(density.rowEmphasisFont)
                    .monospacedDigit()
            }
            .frame(width: density.numberWidth, alignment: .leading)
            AllowanceBar(freeFraction: min(max(allowance.freePercent / 100, 0), 1), low: isLow, height: density.barHeight)
                .frame(maxWidth: .infinity)
            Text(allowance.isEmpty ? "Empty" : "\(UsageFormatting.percent(allowance.freePercent)) left")
                .font(density.rowEmphasisFont)
                .foregroundStyle(allowance.isEmpty ? AnyShapeStyle(UsageTheme.runOutColour) : AnyShapeStyle(.primary))
                .monospacedDigit()
                .lineLimit(1)
                .frame(width: density.percentWidth, alignment: .trailing)
            HStack(spacing: 3) {
                if runOut != nil {
                    Image(systemName: "flame.fill")
                        .font(.system(size: 10))
                        .foregroundStyle(UsageTheme.runOutColour)
                }
                Text(timeLabel(runOut: runOut))
                    .font(density.rowFont)
                    .foregroundStyle(.secondary)
                    .monospacedDigit()
                    .lineLimit(1)
            }
            .frame(width: density.timeWidth, alignment: .trailing)
            .help(runOut == nil || !allowance.hasResetAt ? "" : UsageFormatting.resetLabel(allowance.resetAt.date, now: now))
        }
        .frame(height: density.rowHeight)
        .accessibilityElement(children: .combine)
        .accessibilityLabel("Account \(number): \(allowance.isEmpty ? "empty" : "\(UsageFormatting.percent(allowance.freePercent)) left"), \(timeLabel(runOut: runOut))")
    }

    private func timeLabel(runOut: Date?) -> String {
        guard allowance.hasResetAt else { return "" }
        if let runOut { return "Limit in " + UsageFormatting.duration(runOut.timeIntervalSince(now)) }
        return UsageFormatting.resetLabel(allowance.resetAt.date, now: now)
    }
}

/// Full-width capsule of what is left: blue while the window lasts to its
/// reset, red when it is running out or nearly gone, empty when it is empty.
private struct AllowanceBar: View {
    let freeFraction: Double
    let low: Bool
    let height: CGFloat

    var body: some View {
        GeometryReader { geometry in
            ZStack(alignment: .leading) {
                Capsule().fill(.fill.secondary)
                Capsule()
                    .fill(low ? UsageTheme.runOutColour : UsageTheme.capacityColour)
                    .frame(width: freeFraction > 0 ? max(height, geometry.size.width * freeFraction) : 0)
            }
            .frame(height: height)
            .frame(maxHeight: .infinity, alignment: .center)
        }
        .accessibilityHidden(true)
    }
}
