import SwiftUI

struct AccountSection: View {
    let account: CSSwitch_Account
    let hideIdentity: Bool
    let now: Date
    /// The account's usage per day for the last 30 days, oldest first, in the selected metric.
    let dailyUsage: [Int64]

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(alignment: .firstTextBaseline, spacing: 6) {
                AccountMarker(number: Int(account.accountNumber))
                    .alignmentGuide(.firstTextBaseline) { $0[VerticalAlignment.center] + 4 }
                Text("Account \(account.accountNumber)")
                    .font(UsageTheme.labelFont)
                    .layoutPriority(2)
                if !account.email.isEmpty {
                    Text(hideIdentity ? UsageFormatting.privateEmail(account.email) : account.email)
                        .font(.system(size: 11))
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                        .truncationMode(.middle)
                }
                Spacer(minLength: 4)
                if !account.plan.isEmpty {
                    Text(account.plan.capitalized)
                        .font(UsageTheme.detailEmphasisFont)
                        .foregroundStyle(.secondary)
                        .layoutPriority(2)
                }
            }
            if account.hasAllowance {
                let windows = account.allowance.windows.sorted { $0.periodSeconds < $1.periodSeconds }
                ForEach(windows, id: \.periodSeconds) { window in
                    AllowanceRow(
                        allowance: window,
                        now: now,
                        storedResets: window == windows.last && account.allowance.hasAvailableResetCredits
                            ? Int(account.allowance.availableResetCredits) : 0
                    )
                }
            }
            if dailyUsage.contains(where: { $0 > 0 }) {
                HStack(alignment: .center) {
                    Text("Usage trend")
                        .font(UsageTheme.labelFont)
                    Spacer(minLength: 12)
                    UsageTrend(values: dailyUsage, colour: UsageTheme.accountColour(Int(account.accountNumber)))
                }
            }
        }
        .padding(UsageTheme.cardInset)
        .cardSurface()
    }
}

extension CSSwitch_AllowanceWindow {
    var freePercent: Double { 100 - usedPercent }

    var periodTitle: String {
        switch periodSeconds {
        case 604_800: "Weekly"
        case let seconds where seconds % 86_400 == 0: "\(seconds / 86_400)-day"
        case let seconds where seconds % 3_600 == 0: "\(seconds / 3_600)-hour"
        default: "\(periodSeconds / 60)-minute"
        }
    }

    /// When the allowance empties if use continues at this window's average
    /// pace so far; nil when it lasts until the reset.
    func runOutDate(now: Date) -> Date? {
        guard hasResetAt, usedPercent > 0, usedPercent < 100 else { return nil }
        let reset = resetAt.date
        let elapsed = now.timeIntervalSince(reset.addingTimeInterval(-TimeInterval(periodSeconds)))
        guard elapsed > 0 else { return nil }
        let runOut = now.addingTimeInterval(freePercent / usedPercent * elapsed)
        return runOut < reset ? runOut : nil
    }
}

private struct AllowanceRow: View {
    let allowance: CSSwitch_AllowanceWindow
    let now: Date
    let storedResets: Int

    var body: some View {
        let runOut = allowance.runOutDate(now: now)
        VStack(alignment: .leading, spacing: 6) {
            HStack(alignment: .firstTextBaseline) {
                Text(allowance.periodTitle)
                    .font(UsageTheme.labelFont)
                Spacer(minLength: 4)
                Text("\(UsageFormatting.percent(allowance.freePercent)) left")
                    .font(UsageTheme.labelFont)
                    .monospacedDigit()
            }
            GeometryReader { geometry in
                ZStack(alignment: .leading) {
                    Capsule().fill(.fill.secondary)
                    Capsule()
                        .fill(runOut == nil ? UsageTheme.capacityColour : UsageTheme.runOutColour)
                        .frame(width: geometry.size.width * min(max(allowance.freePercent / 100, 0), 1))
                }
            }
            .frame(height: 5)
            if let runOut {
                Text(UsageFormatting.runOutLabel(runOut, now: now))
                    .font(UsageTheme.detailFont)
                    .foregroundStyle(UsageTheme.runOutColour)
                    .monospacedDigit()
            }
            HStack {
                if storedResets > 0 {
                    Text(storedResets == 1 ? "1 stored reset" : "\(storedResets) stored resets")
                }
                Spacer(minLength: 4)
                if allowance.hasResetAt {
                    Text(UsageFormatting.resetLabel(allowance.resetAt.date, now: now))
                }
            }
            .font(UsageTheme.detailFont)
            .foregroundStyle(.secondary)
            .monospacedDigit()
        }
    }
}

/// One bar per day, today on the right, scaled to the busiest day.
private struct UsageTrend: View {
    let values: [Int64]
    let colour: Color

    private static let barWidth: CGFloat = 3
    private static let barGap: CGFloat = 2
    private static let height: CGFloat = 18

    var body: some View {
        let busiest = Double(values.max() ?? 0)
        HStack(alignment: .bottom, spacing: Self.barGap) {
            ForEach(values.indices, id: \.self) { index in
                let share = busiest > 0 ? Double(values[index]) / busiest : 0
                RoundedRectangle(cornerRadius: 1, style: .continuous)
                    .fill(colour.opacity(values[index] > 0 ? 1 : 0.35))
                    .frame(width: Self.barWidth, height: max(Self.barWidth, Self.height * share))
            }
        }
        .frame(height: Self.height, alignment: .bottom)
        .accessibilityHidden(true)
    }
}
