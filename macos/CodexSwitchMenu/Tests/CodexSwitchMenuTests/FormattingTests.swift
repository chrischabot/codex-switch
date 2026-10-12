import Foundation
import SwiftUI
import XCTest
@testable import CodexSwitchMenu

final class FormattingTests: XCTestCase {
    private let locale = Locale(identifier: "en_GB")
    private let utc = TimeZone(secondsFromGMT: 0)!

    func testPrivacyMaskKeepsEmailShapeWithoutRevealingAddress() {
        XCTAssertEqual(
            UsageFormatting.privateEmail("alice@example.com"),
            "a••••••••@••••••••.com"
        )
        XCTAssertEqual(UsageFormatting.privateEmail("account-1"), "account-1")
    }

    func testResetsUseWeekdayWithinAWeekAndDateBeyond() throws {
        let now = try Date.ISO8601FormatStyle().parse("2026-10-11T15:00:00Z")
        let sunday = try Date.ISO8601FormatStyle().parse("2026-10-18T05:52:00Z")
        let november = try Date.ISO8601FormatStyle().parse("2026-11-08T09:20:00Z")

        XCTAssertEqual(UsageFormatting.resetLabel(sunday, now: now, locale: locale, timeZone: utc), "Resets Sun 05:52")
        XCTAssertEqual(UsageFormatting.resetLabel(november, now: now, locale: locale, timeZone: utc), "Resets 8 Nov 09:20")
    }

    func testAmountsUseThreeSignificantFigures() {
        XCTAssertEqual(UsageFormatting.tokens(946_491, locale: locale), "946K")
        XCTAssertEqual(UsageFormatting.tokens(34_138_059, locale: locale), "34.1M")
        XCTAssertEqual(UsageFormatting.tokens(6_883_276_314, locale: locale), "6.88B")
        XCTAssertEqual(UsageFormatting.tokens(999_600, locale: locale), "1.00M")
        XCTAssertEqual(UsageFormatting.tokens(512, locale: locale), "512")
        XCTAssertEqual(UsageFormatting.cost(4_990_000_000, locale: locale), "$4.99")
        XCTAssertEqual(UsageFormatting.cost(45_110_000_000, locale: locale), "$45.1")
        XCTAssertEqual(UsageFormatting.cost(647_960_000_000, locale: locale), "$648")
        XCTAssertEqual(UsageFormatting.cost(1_767_190_000_000, locale: locale), "$1,767")
        XCTAssertEqual(UsageFormatting.cost(0, locale: locale), "$0")
    }

    func testFormattingUsesSuppliedNumberSymbols() {
        XCTAssertEqual(UsageFormatting.tokens(2_090_000_000, locale: Locale(identifier: "nl_NL")), "2,09B")
    }

    @MainActor
    func testMenuBarShowsEachAccountNumberAndRoundedAllowance() {
        let layout = StatusStripRenderer.layout(for: [
            MenuBarAccountValue(provider: .chatgpt, number: 1, freePercent: 74.6),
            MenuBarAccountValue(provider: .chatgpt, number: 2, freePercent: 25.4),
        ])

        XCTAssertEqual(layout.badges, ["1", "2"])
        XCTAssertEqual(layout.values, ["75%", "25%"])
    }
}
