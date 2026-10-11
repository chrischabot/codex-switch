import Foundation
import SwiftUI
import XCTest
@testable import CodexSwitchMenu

final class FormattingTests: XCTestCase {
    private let locale = Locale(identifier: "en_US")
    private let utc = TimeZone(secondsFromGMT: 0)!

    func testPrivacyMaskKeepsEmailShapeWithoutRevealingAddress() {
        XCTAssertEqual(
            UsageFormatting.privateEmail("alice@example.com"),
            "a••••••••@••••••••.com"
        )
        XCTAssertEqual(
            UsageFormatting.privateEmail("bob@example.com"),
            "b••••••••@••••••••.com"
        )
        XCTAssertEqual(UsageFormatting.privateEmail("account-1"), "account-1")
    }

    func testOperationalTimesUseWeekdayAndTime() throws {
        let date = try Date.ISO8601FormatStyle().parse("2026-07-16T18:40:00Z")
        let now = try Date.ISO8601FormatStyle().parse("2026-07-16T12:00:00Z")
        let nextWeek = try Date.ISO8601FormatStyle().parse("2026-07-23T18:40:00Z")

        XCTAssertEqual(
            UsageFormatting.weekdayTime(date, locale: locale, timeZone: utc),
            "Thu 18:40"
        )
        XCTAssertEqual(
            UsageFormatting.resetLabel(nextWeek, now: now, locale: locale, timeZone: utc),
            "Resets next Thu 18:40"
        )
    }

    func testFormatsCompactTokens() {
        XCTAssertEqual(UsageFormatting.tokens(2_090_000_000, locale: locale), "2.09B tokens")
        XCTAssertEqual(UsageFormatting.compactTokens(2_090_000_000, locale: locale), "2.09B")
        XCTAssertEqual(UsageFormatting.compactTokens(40_160_000, locale: locale), "40.2M")
        XCTAssertEqual(UsageFormatting.compactTokens(378_420_000, locale: locale), "378M")
        XCTAssertEqual(UsageFormatting.ringTokens(265_980_000, locale: locale), "266M")
    }

    func testFormattingUsesSuppliedNumberSymbols() {
        let dutch = Locale(identifier: "nl_NL")
        XCTAssertEqual(UsageFormatting.tokens(2_090_000_000, locale: dutch), "2,09B tokens")
    }

    func testAccountColoursKeepTheLockedFiveAndDoNotRepeatThroughTwenty() {
        let colours = (1...20).map(UsageTheme.accountColour)

        XCTAssertEqual(Array(colours.prefix(5)), [.purple, .orange, .green, .pink, .brown])
        XCTAssertEqual(Set(colours).count, 20)
    }

    @MainActor
    func testStatusStripShowsOnlyConcreteRoundedAllowanceValues() {
        let layout = StatusStripRenderer.layout(for: [
            MenuBarAccountValue(number: 1, freePercent: 74.6),
            MenuBarAccountValue(number: 2, freePercent: 25.4),
        ])

        XCTAssertEqual(layout.values, ["① 75%", "② 25%"])
    }
}
