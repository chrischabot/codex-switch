import Foundation

/// Numbers and dates follow the Mac's Language & Region settings, including a
/// custom number format. `NumberFormatter` and `DateFormatter` honour those
/// overrides; Foundation's `FormatStyle` ignores the custom number format.
enum UsageFormatting {
    static func privateEmail(_ value: String) -> String {
        let parts = value.split(separator: "@", maxSplits: 1, omittingEmptySubsequences: false)
        guard parts.count == 2,
              let first = parts[0].first,
              !parts[1].isEmpty else {
            return value
        }

        let domainParts = parts[1].split(separator: ".", omittingEmptySubsequences: false)
        let suffix = domainParts.count > 1 ? ".\(domainParts.last!)" : ""
        return "\(first)••••••••@••••••••\(suffix)"
    }

    /// "Resets Sun 07:52" within a week; "Resets 8 Nov 10:20" beyond it.
    static func resetLabel(
        _ date: Date,
        now: Date,
        locale: Locale = .autoupdatingCurrent,
        timeZone: TimeZone = .autoupdatingCurrent
    ) -> String {
        "Resets " + dayAndTime(date, now: now, locale: locale, timeZone: timeZone)
    }

    /// "Runs out Tue 14:20", in the same form as the reset time beside it.
    static func runOutLabel(
        _ date: Date,
        now: Date,
        locale: Locale = .autoupdatingCurrent,
        timeZone: TimeZone = .autoupdatingCurrent
    ) -> String {
        "Runs out " + dayAndTime(date, now: now, locale: locale, timeZone: timeZone)
    }

    static func percent(_ value: Double, locale: Locale = .autoupdatingCurrent) -> String {
        decimal(value.rounded(), fractionDigits: 0, locale: locale) + "%"
    }

    /// Token counts to three significant figures: 946K, 34.1M, 6.88B.
    static func tokens(_ value: Int64, locale: Locale = .autoupdatingCurrent) -> String {
        let suffixes = ["", "K", "M", "B", "T"]
        var scaled = abs(Double(value))
        var power = 0
        while power < suffixes.count - 1, scaled >= 999.5 {
            scaled /= 1_000
            power += 1
        }
        let fractionDigits = switch scaled {
        case _ where power == 0: 0
        case ..<9.995: 2
        case ..<99.95: 1
        default: 0
        }
        return decimal(scaled, fractionDigits: fractionDigits, locale: locale) + suffixes[power]
    }

    /// API-equivalent cost in US dollars, about three significant figures:
    /// $4.99, $45.1, $648, $1,175. OpenAI prices in dollars, so the symbol is
    /// fixed while the digits follow the Mac.
    static func cost(_ nanoUSD: Int64, locale: Locale = .autoupdatingCurrent) -> String {
        let dollars = abs(Double(nanoUSD)) / 1_000_000_000
        let fractionDigits = switch dollars {
        case 0: 0
        case ..<9.995: 2
        case ..<99.95: 1
        default: 0
        }
        return "$" + decimal(dollars, fractionDigits: fractionDigits, locale: locale)
    }

    private static func dayAndTime(_ date: Date, now: Date, locale: Locale, timeZone: TimeZone) -> String {
        let day = DateFormatter()
        day.locale = locale
        day.timeZone = timeZone
        day.setLocalizedDateFormatFromTemplate(date.timeIntervalSince(now) < 7 * 24 * 60 * 60 ? "EEE" : "dMMM")
        let time = DateFormatter()
        time.locale = locale
        time.timeZone = timeZone
        time.setLocalizedDateFormatFromTemplate("jmm")
        return day.string(from: date) + " " + time.string(from: date)
    }

    private static func decimal(_ value: Double, fractionDigits: Int, locale: Locale) -> String {
        let formatter = NumberFormatter()
        formatter.locale = locale
        formatter.numberStyle = .decimal
        formatter.usesGroupingSeparator = true
        formatter.minimumFractionDigits = fractionDigits
        formatter.maximumFractionDigits = fractionDigits
        return formatter.string(from: NSNumber(value: value)) ?? String(value)
    }
}
