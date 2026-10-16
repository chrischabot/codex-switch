import SwiftUI

/// A provider's accounts in one card, organised by allowance window: one
/// block per window with a thin row per account, so one window is read
/// across every account at once and a new account adds one row per block.
/// The first block also says who each account is; later blocks show only
/// the colour and number.
struct ProviderCard: View {
    let snapshot: CSSwitch_MenuSnapshot
    let provider: CSSwitch_Provider
    let hideIdentity: Bool
    let now: Date

    var body: some View {
        let accounts = snapshot.accounts.filter { $0.menuProvider == provider }.sorted { $0.accountNumber < $1.accountNumber }
        let blocks = IndicatorBlock.blocks(for: accounts, provider: provider)
        VStack(alignment: .leading, spacing: 9) {
            ForEach(Array(blocks.enumerated()), id: \.element.id) { index, block in
                VStack(alignment: .leading, spacing: 5) {
                    Text(block.title)
                        .font(UsageTheme.labelFont)
                    ForEach(block.rows, id: \.account.key) { row in
                        AllowanceRow(
                            account: row.account,
                            colour: snapshot.colour(of: row.account),
                            allowance: row.window,
                            now: now,
                            identity: index == 0 ? (hideIdentity ? UsageFormatting.privateEmail(row.account.email) : row.account.email) : nil
                        )
                    }
                    if index == 0 {
                        ForEach(accounts.filter { !$0.offersConfiguredModel }, id: \.key) { account in
                            OutOfPoolRow(
                                account: account,
                                colour: snapshot.colour(of: account),
                                identity: hideIdentity ? UsageFormatting.privateEmail(account.email) : account.email
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
    /// account that cannot run the configured model has no row: its
    /// allowance is not the pool's.
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
}

extension CSSwitch_Account {
    /// "Pro", "Max 20x".
    var planTitle: String { plan.prefix(1).uppercased() + plan.dropFirst() }
}

/// One account's line in a window block: its colour and number, the bar,
/// what is left, and the time that matters: a flame and how long until it
/// hits the limit if it will, otherwise how long until it resets. In the
/// provider's first block the row also carries the account's address and
/// plan, and the state that keeps new work off it when there is one.
private struct AllowanceRow: View {
    let account: CSSwitch_Account
    let colour: Color
    let allowance: CSSwitch_AllowanceWindow
    let now: Date
    let identity: String?

    static let numberWidth: CGFloat = 22
    private static let percentWidth: CGFloat = 36
    private static let timeWidth: CGFloat = 60

    var body: some View {
        let runOut = allowance.runOutDate(now: now)
        VStack(alignment: .leading, spacing: 2) {
            HStack(spacing: 8) {
                AccountNumber(number: Int(account.accountNumber), colour: colour)
                AllowanceBar(
                    freeFraction: min(max(allowance.freePercent / 100, 0), 1),
                    runningOut: runOut != nil
                )
                Text(UsageFormatting.percent(allowance.freePercent))
                    .font(UsageTheme.detailEmphasisFont)
                    .monospacedDigit()
                    .lineLimit(1)
                    .frame(width: Self.percentWidth, alignment: .trailing)
                Group {
                    if let runOut {
                        HStack(spacing: 3) {
                            Image(systemName: "flame.fill")
                                .font(.system(size: 10))
                                .foregroundStyle(UsageTheme.runOutColour)
                            Text(UsageFormatting.duration(runOut.timeIntervalSince(now)))
                        }
                        .help(UsageFormatting.resetLabel(allowance.resetAt.date, now: now))
                    } else if allowance.hasResetAt {
                        Text(UsageFormatting.duration(allowance.resetAt.date.timeIntervalSince(now)))
                    }
                }
                .font(UsageTheme.detailFont)
                .foregroundStyle(.secondary)
                .monospacedDigit()
                .lineLimit(1)
                .frame(width: Self.timeWidth, alignment: .trailing)
            }
            .frame(height: 16)
            if let identity {
                AccountIdentityLine(identity: identity, plan: account.planTitle)
                    .padding(.leading, Self.numberWidth + 8)
                let status = account.usable ? account.reserveStatus : account.unavailableReason
                if !status.isEmpty {
                    Text(status)
                        .font(.system(size: 11))
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                        .padding(.leading, Self.numberWidth + 8)
                }
            }
        }
        .accessibilityElement(children: .combine)
        .accessibilityLabel("Account \(account.accountNumber): \(UsageFormatting.percent(allowance.freePercent)) left")
    }
}

/// An account that cannot run the configured model: listed, greyed, with no
/// allowance to compare.
private struct OutOfPoolRow: View {
    let account: CSSwitch_Account
    let colour: Color
    let identity: String

    var body: some View {
        HStack(spacing: 8) {
            AccountNumber(number: Int(account.accountNumber), colour: colour)
            AccountIdentityLine(identity: identity, plan: account.planTitle)
            Spacer(minLength: 0)
        }
        .foregroundStyle(.secondary)
        .frame(height: 16)
        .accessibilityElement(children: .combine)
        .accessibilityLabel("Account \(account.accountNumber), not in the pool")
    }
}

/// The address and plan on one line. The address is never cut: the plan
/// goes first, then the type shrinks a little.
private struct AccountIdentityLine: View {
    let identity: String
    let plan: String

    var body: some View {
        ViewThatFits(in: .horizontal) {
            Text(plan.isEmpty ? identity : "\(identity) · \(plan)")
                .fixedSize()
            Text(identity)
                .fixedSize()
            Text(identity)
                .lineLimit(1)
                .minimumScaleFactor(0.75)
        }
        .font(.system(size: 11))
        .foregroundStyle(.secondary)
    }
}

private struct AccountNumber: View {
    let number: Int
    let colour: Color

    var body: some View {
        HStack(spacing: 3) {
            AccountMarker(colour: colour, size: 7)
            Text("\(number)")
                .font(UsageTheme.detailEmphasisFont)
                .monospacedDigit()
        }
        .frame(width: AllowanceRow.numberWidth, alignment: .leading)
    }
}

/// Full-width capsule: blue while the window lasts to its reset, red when it will run out.
private struct AllowanceBar: View {
    let freeFraction: Double
    let runningOut: Bool

    private static let height: CGFloat = 6

    var body: some View {
        GeometryReader { geometry in
            ZStack(alignment: .leading) {
                Capsule().fill(.fill.secondary)
                Capsule()
                    .fill(runningOut ? UsageTheme.runOutColour : UsageTheme.capacityColour)
                    .frame(width: freeFraction > 0 ? max(Self.height, geometry.size.width * freeFraction) : 0)
            }
            .frame(height: Self.height)
            .frame(maxHeight: .infinity, alignment: .center)
        }
        .accessibilityHidden(true)
    }
}
