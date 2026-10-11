import Foundation

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

    static func weekdayTime(
        _ date: Date,
        locale: Locale = Locale(identifier: "en_GB"),
        timeZone: TimeZone = .current
    ) -> String {
        let formatter = DateFormatter()
        formatter.locale = locale
        formatter.timeZone = timeZone
        formatter.dateFormat = "EEE HH:mm"
        return formatter.string(from: date)
    }

    static func resetLabel(
        _ date: Date,
        now: Date = Date(),
        locale: Locale = Locale(identifier: "en_GB"),
        timeZone: TimeZone = .current
    ) -> String {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = timeZone
        let sameWeekday = calendar.component(.weekday, from: date)
            == calendar.component(.weekday, from: now)
        let prefix = sameWeekday && date.timeIntervalSince(now) > 24 * 60 * 60
            ? "Resets next "
            : "Resets "
        return prefix + weekdayTime(date, locale: locale, timeZone: timeZone)
    }

    static func percent(_ value: Double, locale: Locale = Locale(identifier: "en_GB")) -> String {
        decimal(
            value,
            minimumFractionDigits: value.rounded() == value ? 0 : 1,
            maximumFractionDigits: value.rounded() == value ? 0 : 1,
            locale: locale
        ) + "%"
    }

    static func tokens(_ value: Int64, locale: Locale = Locale(identifier: "en_GB")) -> String {
        compactTokens(value, locale: locale) + " tokens"
    }

    static func compactTokens(_ value: Int64, locale: Locale = Locale(identifier: "en_GB")) -> String {
        let magnitude: (divisor: Double, suffix: String)
        switch abs(value) {
        case 1_000_000_000...:
            magnitude = (1_000_000_000, "B")
        case 1_000_000...:
            magnitude = (1_000_000, "M")
        case 1_000...:
            magnitude = (1_000, "K")
        default:
            magnitude = (1, "")
        }
        let scaled = Double(value) / magnitude.divisor
        let fractionDigits: Int
        switch abs(scaled) {
        case 100...:
            fractionDigits = 0
        case 10...:
            fractionDigits = 1
        default:
            fractionDigits = magnitude.divisor == 1 ? 0 : 2
        }
        return decimal(
            scaled,
            minimumFractionDigits: 0,
            maximumFractionDigits: fractionDigits,
            locale: locale
        ) + magnitude.suffix
    }

    static func ringTokens(_ value: Int64, locale: Locale = Locale(identifier: "en_GB")) -> String {
        let magnitude: (divisor: Double, suffix: String)
        switch abs(value) {
        case 1_000_000_000...:
            magnitude = (1_000_000_000, "B")
        case 1_000_000...:
            magnitude = (1_000_000, "M")
        case 1_000...:
            magnitude = (1_000, "K")
        default:
            magnitude = (1, "")
        }
        return decimal(
            Double(value) / magnitude.divisor,
            minimumFractionDigits: 0,
            maximumFractionDigits: magnitude.divisor == 1 ? 0 : 1,
            locale: locale
        ) + magnitude.suffix
    }

    static func baseCost(
        _ nanoUSD: Int64,
        fractionDigits: Int? = nil,
        locale: Locale = Locale(identifier: "en_GB")
    ) -> String {
        let decimals = fractionDigits ?? costFractionDigits(nanoUSD)
        return "$" + decimal(
            Double(nanoUSD) / 1_000_000_000,
            minimumFractionDigits: decimals,
            maximumFractionDigits: decimals,
            locale: locale
        )
    }

    static func compactBaseCost(_ nanoUSD: Int64, locale: Locale = Locale(identifier: "en_GB")) -> String {
        let dollars = Double(nanoUSD) / 1_000_000_000
        let magnitude: (divisor: Double, suffix: String)
        switch abs(dollars) {
        case 1_000_000_000...: magnitude = (1_000_000_000, "B")
        case 1_000_000...: magnitude = (1_000_000, "M")
        case 1_000...: magnitude = (1_000, "K")
        default: magnitude = (1, "")
        }
        return "$" + decimal(
            dollars / magnitude.divisor,
            minimumFractionDigits: 0,
            maximumFractionDigits: magnitude.divisor == 1 ? 2 : 1,
            locale: locale
        ) + magnitude.suffix
    }

    static func costFractionDigits(_ totalCostNanoUSD: Int64?) -> Int { 2 }

    /// Foundation's modern `FormatStyle` currently ignores the user's custom
    /// Number format in Language & Region. `NumberFormatter` honours the
    /// underlying AppleICUNumberSymbols override, so keep that compatibility
    /// at this single presentation boundary.
    private static func decimal(
        _ value: Double,
        minimumFractionDigits: Int,
        maximumFractionDigits: Int,
        locale: Locale
    ) -> String {
        let formatter = NumberFormatter()
        formatter.locale = locale
        formatter.numberStyle = .decimal
        formatter.usesGroupingSeparator = true
        formatter.minimumFractionDigits = minimumFractionDigits
        formatter.maximumFractionDigits = maximumFractionDigits
        return formatter.string(from: NSNumber(value: value)) ?? String(value)
    }
}
