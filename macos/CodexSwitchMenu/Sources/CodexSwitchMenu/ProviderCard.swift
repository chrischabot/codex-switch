import SwiftUI

/// A provider's accounts in one card, organised by indicator: who the
/// accounts are, then one block per allowance window with a thin row per
/// account, so one window is read across every account at once and a new
/// account adds one row per block.
struct ProviderCard: View {
    let snapshot: CSSwitch_MenuSnapshot
    let provider: CSSwitch_Provider
    let hideIdentity: Bool
    let now: Date

    private var accounts: [CSSwitch_Account] {
        snapshot.accounts.filter { $0.menuProvider == provider }.sorted { $0.accountNumber < $1.accountNumber }
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 9) {
            VStack(alignment: .leading, spacing: 5) {
                ForEach(accounts, id: \.key) { account in
                    AccountIdentityRow(account: account, colour: snapshot.colour(of: account), hideIdentity: hideIdentity)
                }
            }
            ForEach(IndicatorBlock.blocks(for: accounts, provider: provider)) { block in
                VStack(alignment: .leading, spacing: 4) {
                    Text(block.title)
                        .font(UsageTheme.labelFont)
                    ForEach(block.rows, id: \.account.key) { row in
                        AllowanceRow(
                            number: Int(row.account.accountNumber),
                            colour: snapshot.colour(of: row.account),
                            allowance: row.window,
                            now: now
                        )
                    }
                }
            }
        }
        .padding(UsageTheme.cardInset)
        .frame(maxWidth: .infinity, alignment: .leading)
        .cardSurface()
    }
}

/// Which account this is: its colour and number, its address (censored in
/// privacy mode), its plan and stored resets, and the state that keeps new
/// work off it when there is one.
private struct AccountIdentityRow: View {
    let account: CSSwitch_Account
    let colour: Color
    let hideIdentity: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            HStack(alignment: .firstTextBaseline, spacing: 6) {
                AccountMarker(colour: colour)
                    .alignmentGuide(.firstTextBaseline) { $0[VerticalAlignment.center] + 4 }
                Text("Account \(account.accountNumber)")
                    .font(UsageTheme.detailEmphasisFont)
                    .layoutPriority(2)
                if !account.email.isEmpty {
                    Text(hideIdentity ? UsageFormatting.privateEmail(account.email) : account.email)
                        .font(.system(size: 11))
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                        .truncationMode(.middle)
                }
                Spacer(minLength: 4)
                Text(planAndResets)
                    .font(UsageTheme.detailFont)
                    .foregroundStyle(.secondary)
                    .layoutPriority(2)
            }
            let status = account.usable ? account.reserveStatus : account.unavailableReason
            if !status.isEmpty {
                Text(status)
                    .font(UsageTheme.detailFont)
                    .foregroundStyle(.secondary)
                    .padding(.leading, 14)
            }
        }
    }

    /// "Pro · 3 stored resets", "Max 20x".
    private var planAndResets: String {
        var parts: [String] = []
        if !account.plan.isEmpty {
            parts.append(account.plan.prefix(1).uppercased() + account.plan.dropFirst())
        }
        if account.allowance.hasAvailableResetCredits, account.allowance.availableResetCredits > 0 {
            let count = account.allowance.availableResetCredits
            parts.append(count == 1 ? "1 stored reset" : "\(count) stored resets")
        }
        return parts.joined(separator: " · ")
    }
}

/// One allowance window across the provider's accounts.
struct IndicatorBlock: Identifiable {
    struct Row {
        let account: CSSwitch_Account
        let window: CSSwitch_AllowanceWindow
    }

    let id: String
    let title: String
    let rows: [Row]

    /// Shortest window first; a model's own limit follows the limit it sits
    /// inside. Accounts keep their number order within every block.
    static func blocks(for accounts: [CSSwitch_Account], provider: CSSwitch_Provider) -> [IndicatorBlock] {
        var rowsByKey: [String: [Row]] = [:]
        var windowsByKey: [String: CSSwitch_AllowanceWindow] = [:]
        for account in accounts where account.hasAllowance {
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

    /// Where the bar would stand now if the window were spent evenly from
    /// start to reset: 1 at the start, 0 at the reset.
    func evenPaceFreeFraction(now: Date) -> Double? {
        guard hasResetAt, periodSeconds > 0 else { return nil }
        let elapsed = now.timeIntervalSince(resetAt.date.addingTimeInterval(-TimeInterval(periodSeconds)))
        return 1 - min(max(elapsed / TimeInterval(periodSeconds), 0), 1)
    }
}

/// One account's line in an indicator block: its number, the bar, what is
/// left, and the one time that matters: a flame and when it hits the limit
/// if it will, otherwise when it resets. The reset of a running-out window
/// is on hover.
private struct AllowanceRow: View {
    let number: Int
    let colour: Color
    let allowance: CSSwitch_AllowanceWindow
    let now: Date

    private static let numberWidth: CGFloat = 22
    private static let percentWidth: CGFloat = 56
    private static let timeWidth: CGFloat = 112

    var body: some View {
        let runOut = allowance.runOutDate(now: now)
        let reset = allowance.hasResetAt ? UsageFormatting.resetLabel(allowance.resetAt.date, now: now) : nil
        HStack(spacing: 8) {
            HStack(spacing: 3) {
                AccountMarker(colour: colour, size: 7)
                Text("\(number)")
                    .font(UsageTheme.detailEmphasisFont)
                    .monospacedDigit()
            }
            .frame(width: Self.numberWidth, alignment: .leading)
            AllowanceBar(
                freeFraction: min(max(allowance.freePercent / 100, 0), 1),
                runningOut: runOut != nil,
                evenPaceFreeFraction: runOut == nil ? nil : allowance.evenPaceFreeFraction(now: now)
            )
            Text("\(UsageFormatting.percent(allowance.freePercent)) left")
                .font(UsageTheme.detailFont)
                .monospacedDigit()
                .lineLimit(1)
                .frame(width: Self.percentWidth, alignment: .trailing)
            Group {
                if let runOut {
                    HStack(spacing: 4) {
                        Image(systemName: "flame.fill")
                            .font(.system(size: 10))
                            .foregroundStyle(UsageTheme.runOutColour)
                        Text("Limit in " + UsageFormatting.duration(runOut.timeIntervalSince(now)))
                    }
                    .help(reset ?? "")
                } else if let reset {
                    Text(reset)
                }
            }
            .font(UsageTheme.detailFont)
            .foregroundStyle(.secondary)
            .monospacedDigit()
            .lineLimit(1)
            .frame(width: Self.timeWidth, alignment: .trailing)
        }
        .frame(height: 16)
        .accessibilityElement(children: .combine)
        .accessibilityLabel("Account \(number): \(UsageFormatting.percent(allowance.freePercent)) left")
    }
}

/// Full-width capsule: blue while the window lasts to its reset, red when
/// it will run out, with a tick where even pace would leave it now.
private struct AllowanceBar: View {
    let freeFraction: Double
    let runningOut: Bool
    let evenPaceFreeFraction: Double?

    private static let height: CGFloat = 5

    var body: some View {
        GeometryReader { geometry in
            let track = geometry.size.width
            ZStack(alignment: .leading) {
                Capsule().fill(.fill.secondary)
                Capsule()
                    .fill(runningOut ? UsageTheme.runOutColour : UsageTheme.capacityColour)
                    .frame(width: freeFraction > 0 ? max(Self.height, track * freeFraction) : 0)
            }
            .overlay(alignment: .leading) {
                if let evenPaceFreeFraction {
                    RoundedRectangle(cornerRadius: 1)
                        .fill(Color.primary.opacity(0.55))
                        .frame(width: 2, height: Self.height + 4)
                        .offset(x: min(max(track * evenPaceFreeFraction - 1, 0), max(track - 2, 0)))
                }
            }
            .frame(height: Self.height)
            .frame(maxHeight: .infinity, alignment: .center)
        }
        .accessibilityHidden(true)
    }
}
