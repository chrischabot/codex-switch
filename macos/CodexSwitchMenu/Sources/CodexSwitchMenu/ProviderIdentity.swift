import AppKit
import SwiftUI

extension CSSwitch_Provider {
    var title: String {
        switch self {
        case .claude: "Claude"
        default: "Codex"
        }
    }

    /// The provider's own logo, as a template image.
    var mark: NSImage? {
        let name = self == .claude ? "claude" : "openai"
        guard let url = Bundle.module.url(forResource: name, withExtension: "svg", subdirectory: "Resources"),
              let image = NSImage(contentsOf: url) else { return nil }
        image.isTemplate = true
        return image
    }
}

extension CSSwitch_MenuSnapshot {
    /// The providers with accounts in the pool, in the order the proxy lists them.
    var providers: [CSSwitch_Provider] {
        accounts.reduce(into: []) { providers, account in
            if !providers.contains(account.menuProvider) { providers.append(account.menuProvider) }
        }
    }

    func account(key: String) -> CSSwitch_Account? {
        accounts.first { $0.key == key }
    }

    /// Each account keeps one colour everywhere, unique across providers.
    func colourSlot(of account: CSSwitch_Account) -> Int {
        providers.count == 1 ? Int(account.accountNumber) : (accounts.firstIndex(of: account) ?? 0) + 1
    }

    func colour(of account: CSSwitch_Account) -> Color {
        UsageTheme.accountColour(colourSlot(of: account))
    }
}

extension CSSwitch_Account {
    var menuProvider: CSSwitch_Provider { provider == .unspecified ? .chatgpt : provider }

    /// "Codex 1", "Claude 2": how an account is named outside its provider's section.
    var poolName: String { "\(menuProvider.title) \(accountNumber)" }
}

struct ProviderMark: View {
    let provider: CSSwitch_Provider
    var size: CGFloat = 16

    var body: some View {
        if let mark = provider.mark {
            Image(nsImage: mark)
                .renderingMode(.template)
                .resizable()
                .frame(width: size, height: size)
                .accessibilityLabel(provider.title)
        }
    }
}

/// The provider's logo and name above its accounts, as OpenUsage does.
struct ProviderHeader: View {
    let provider: CSSwitch_Provider
    /// The provider's spend or tokens for the selected period.
    let total: String?

    var body: some View {
        HStack(spacing: 7) {
            ProviderMark(provider: provider, size: 18)
                .foregroundStyle(.secondary)
            Text(provider.title)
                .font(UsageTheme.titleFont)
            Spacer()
            if let total {
                Text(total)
                    .font(UsageTheme.detailEmphasisFont)
                    .foregroundStyle(.secondary)
                    .monospacedDigit()
            }
        }
        .padding(.horizontal, 4)
    }
}
