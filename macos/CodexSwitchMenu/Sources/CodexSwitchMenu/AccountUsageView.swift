import SwiftUI

struct AccountSection: View {
    let account: CSSwitch_Account
    let hideIdentity: Bool

    var body: some View {
        VStack(spacing: 10) {
            HStack(alignment: .firstTextBaseline, spacing: 7) {
                Circle()
                    .fill(UsageTheme.accountColour(Int(account.accountNumber)))
                    .frame(width: 7, height: 7)
                    .accessibilityHidden(true)
                Text("Account \(account.accountNumber)")
                    .font(.subheadline.weight(.semibold))
                if !account.plan.isEmpty {
                    Text(account.plan.capitalized)
                        .font(.caption.weight(.medium))
                        .foregroundStyle(.secondary)
                }
                if !account.email.isEmpty {
                    Text(hideIdentity ? UsageFormatting.privateEmail(account.email) : account.email)
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                        .truncationMode(.middle)
                }
                Spacer(minLength: 0)
            }
            if account.hasAllowance {
                ForEach(account.allowance.windows, id: \.periodSeconds) { window in
                    AllowanceRow(allowance: window)
                }
                if account.allowance.hasAvailableResetCredits {
                    HStack {
                        Text("Stored resets")
                            .fontWeight(.semibold)
                        Spacer()
                        Text("\(account.allowance.availableResetCredits) available")
                            .foregroundStyle(.secondary)
                            .monospacedDigit()
                    }
                    .font(.caption)
                }
            }
        }
        .padding(14)
        .cardSurface()
    }
}

extension CSSwitch_AllowanceWindow {
    var freePercent: Double { 100 - usedPercent }

    var periodTitle: String {
        switch periodSeconds {
        case 18_000: "5-hour limit"
        case 604_800: "Weekly"
        case 2_592_000: "30-day limit"
        default:
            if periodSeconds % 86_400 == 0 {
                "\(periodSeconds / 86_400)-day limit"
            } else if periodSeconds % 3_600 == 0 {
                "\(periodSeconds / 3_600)-hour limit"
            } else {
                "\(periodSeconds / 60)-minute limit"
            }
        }
    }
}

private struct AllowanceRow: View {
    let allowance: CSSwitch_AllowanceWindow

    var body: some View {
        VStack(spacing: 5) {
            HStack {
                Text(allowance.periodTitle)
                    .font(.subheadline.weight(.semibold))
                Spacer(minLength: 4)
                Text("\(UsageFormatting.percent(allowance.freePercent)) left")
                    .font(.subheadline.weight(.semibold))
                    .monospacedDigit()
            }
            GeometryReader { geometry in
                ZStack(alignment: .leading) {
                    Capsule().fill(Color.secondary.opacity(0.18))
                    Capsule()
                        .fill(UsageTheme.capacityColour)
                        .frame(width: geometry.size.width * min(max(allowance.freePercent / 100, 0), 1))
                }
            }
            .frame(height: 5)
            if allowance.hasResetAt {
                Text(allowance.periodSeconds > 604_800
                    ? "Resets " + allowance.resetAt.date.formatted(.dateTime.day().month(.abbreviated).hour().minute())
                    : UsageFormatting.resetLabel(allowance.resetAt.date))
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .monospacedDigit()
                    .frame(maxWidth: .infinity, alignment: .trailing)
            }
        }
    }
}
