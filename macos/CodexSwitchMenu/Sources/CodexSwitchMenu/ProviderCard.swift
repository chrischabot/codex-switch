import SwiftUI

/// A provider's accounts, organised by allowance window: who the accounts
/// are in one paragraph under the provider's name, then a card with one
/// block per window and one grid row per account, so one window is read
/// across every account at once and a new account adds one row per block.
/// Every row in every block sits on the same grid.
struct ProviderSection: View {
    let snapshot: CSSwitch_MenuSnapshot
    let provider: CSSwitch_Provider
    let providerTotal: String?
    let hideIdentity: Bool
    let now: Date

    var body: some View {
        let accounts = snapshot.accounts.filter { $0.menuProvider == provider }.sorted { $0.accountNumber < $1.accountNumber }
        VStack(alignment: .leading, spacing: 5) {
            ProviderHeader(provider: provider, total: providerTotal)
                .padding(.horizontal, UsageTheme.cardInset)
            AccountIdentities(accounts: accounts, snapshot: snapshot, hideIdentity: hideIdentity)
                .padding(.horizontal, UsageTheme.cardInset)
            VStack(alignment: .leading, spacing: 16) {
                ForEach(IndicatorBlock.blocks(for: accounts, provider: provider)) { block in
                    VStack(alignment: .leading, spacing: 8) {
                        Text(block.title)
                            .font(UsageTheme.labelFont)
                        Grid(alignment: .leading, horizontalSpacing: AllowanceGrid.columnGap, verticalSpacing: AllowanceGrid.rowGap) {
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
            .padding(UsageTheme.cardInset)
            .frame(maxWidth: .infinity, alignment: .leading)
            .cardSurface()
        }
    }
}

/// One line per account: its colour, number, address (censored in privacy
/// mode), plan, and any state that keeps new work off it, in the proxy's
/// words. A line wraps when its address needs it; nothing is cut.
private struct AccountIdentities: View {
    let accounts: [CSSwitch_Account]
    let snapshot: CSSwitch_MenuSnapshot
    let hideIdentity: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            ForEach(accounts, id: \.key) { account in
                let address = hideIdentity ? UsageFormatting.privateEmail(account.email) : account.email
                // An account outside the pool for the configured model is listed by its plan alone.
                let status = account.usable ? account.reserveStatus : account.offersConfiguredModel ? account.unavailableReason : ""
                let detail = [address, account.planTitle, status].filter { !$0.isEmpty }.joined(separator: " · ")
                HStack(alignment: .firstTextBaseline, spacing: 3) {
                    AccountMarker(colour: snapshot.colour(of: account), size: 7)
                        .alignmentGuide(.firstTextBaseline) { $0[VerticalAlignment.center] + 3 }
                    Text("\(account.accountNumber)")
                        .fontWeight(.medium)
                        .frame(width: AllowanceGrid.numberWidth - 10, alignment: .leading)
                    Text(detail)
                        .fixedSize(horizontal: false, vertical: true)
                }
            }
        }
        .font(.system(size: 11))
        .foregroundStyle(.secondary)
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

/// One allowance window across the provider's accounts. An account that
/// cannot run the configured model has a row with nothing in it beyond its
/// number: it is listed, not pooled.
struct IndicatorBlock: Identifiable {
    struct Row {
        let account: CSSwitch_Account
        let window: CSSwitch_AllowanceWindow?
    }

    let id: String
    let title: String
    let rows: [Row]

    /// Shortest window first; a model's own limit follows the limit it sits
    /// inside. Accounts keep their number order within every block.
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
        let outOfPool = accounts.filter { !$0.offersConfiguredModel }.map { Row(account: $0, window: nil) }
        let ordered = windowsByKey.sorted { left, right in
            (left.value.periodSeconds, left.value.modelID, left.value.modelName)
                < (right.value.periodSeconds, right.value.modelID, right.value.modelName)
        }
        return ordered.enumerated().map { index, entry in
            let rows = (rowsByKey[entry.key] ?? []) + (index == 0 ? outOfPool : [])
            return IndicatorBlock(id: entry.key, title: entry.value.title(for: provider),
                                  rows: rows.sorted { $0.account.accountNumber < $1.account.accountNumber })
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

/// The columns every account row shares, so edges line up across blocks
/// and providers by construction.
enum AllowanceGrid {
    static let columnGap: CGFloat = 8
    static let rowGap: CGFloat = 8
    static let rowHeight: CGFloat = 16
    static let numberWidth: CGFloat = 22
    /// "100% left"
    static let percentWidth: CGFloat = 52
    static let flameWidth: CGFloat = 10
    /// "Resets in 22d 12h"
    static let timeWidth: CGFloat = 92
    static let barHeight: CGFloat = 6
    static let textFont = Font.system(size: 11)
    static let textEmphasisFont = Font.system(size: 11, weight: .medium)
}

/// One account's line in a window block: its colour and number, the bar,
/// what is left, and the time that matters: a red flame and "Limit in 9h
/// 55m" if it will run out, otherwise "Resets in 44m". An empty window says
/// so in red; a window with no room to spare is red too.
private struct AllowanceGridRow: View {
    let number: Int
    let colour: Color
    let allowance: CSSwitch_AllowanceWindow?
    let now: Date

    var body: some View {
        let runOut = allowance?.runOutDate(now: now)
        let isEmpty = allowance?.isEmpty ?? false
        let isLow = isEmpty || runOut != nil || (allowance.map { $0.freePercent < 10 } ?? false)
        GridRow {
            HStack(spacing: 3) {
                AccountMarker(colour: colour, size: 7)
                Text("\(number)")
                    .font(UsageTheme.detailEmphasisFont)
                    .monospacedDigit()
            }
            .frame(width: AllowanceGrid.numberWidth, alignment: .leading)
            AllowanceBar(freeFraction: allowance.map { min(max($0.freePercent / 100, 0), 1) } ?? 0, low: isLow)
                .frame(maxWidth: .infinity)
            Text(allowance.map { isEmpty ? "Empty" : "\(UsageFormatting.percent($0.freePercent)) left" } ?? "")
                .font(AllowanceGrid.textEmphasisFont)
                .foregroundStyle(isEmpty ? AnyShapeStyle(UsageTheme.runOutColour) : AnyShapeStyle(.primary))
                .monospacedDigit()
                .lineLimit(1)
                .frame(width: AllowanceGrid.percentWidth, alignment: .trailing)
            Image(systemName: "flame.fill")
                .font(.system(size: 10))
                .foregroundStyle(UsageTheme.runOutColour)
                .opacity(runOut == nil ? 0 : 1)
                .frame(width: AllowanceGrid.flameWidth)
            Text(allowance == nil ? "Not in pool" : timeLabel(runOut: runOut))
                .font(AllowanceGrid.textFont)
                .foregroundStyle(.secondary)
                .monospacedDigit()
                .lineLimit(1)
                .frame(width: AllowanceGrid.timeWidth, alignment: .trailing)
                .help(runOut == nil || allowance?.hasResetAt != true ? "" : UsageFormatting.resetLabel(allowance!.resetAt.date, now: now))
        }
        .frame(height: AllowanceGrid.rowHeight)
        .accessibilityElement(children: .combine)
        .accessibilityLabel(accessibilityText(runOut: runOut))
    }

    private func timeLabel(runOut: Date?) -> String {
        guard let allowance, allowance.hasResetAt else { return "" }
        if let runOut { return "Limit in " + UsageFormatting.duration(runOut.timeIntervalSince(now)) }
        return UsageFormatting.resetLabel(allowance.resetAt.date, now: now)
    }

    private func accessibilityText(runOut: Date?) -> String {
        guard let allowance else { return "Account \(number), not in the pool" }
        let left = allowance.isEmpty ? "empty" : "\(UsageFormatting.percent(allowance.freePercent)) left"
        return "Account \(number): \(left), \(timeLabel(runOut: runOut))"
    }
}

/// Full-width capsule of what is left: blue while the window lasts to its
/// reset, red when it is running out or nearly gone, empty when it is empty.
private struct AllowanceBar: View {
    let freeFraction: Double
    let low: Bool

    var body: some View {
        GeometryReader { geometry in
            ZStack(alignment: .leading) {
                Capsule().fill(.fill.secondary)
                Capsule()
                    .fill(low ? UsageTheme.runOutColour : UsageTheme.capacityColour)
                    .frame(width: freeFraction > 0 ? max(AllowanceGrid.barHeight, geometry.size.width * freeFraction) : 0)
            }
            .frame(height: AllowanceGrid.barHeight)
            .frame(maxHeight: .infinity, alignment: .center)
        }
        .accessibilityHidden(true)
    }
}
