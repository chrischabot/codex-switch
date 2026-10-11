import SwiftUI

struct CodexSwitchPanel: View {
    let store: OverviewStore
    let maximumHeight: CGFloat
    @State private var selectedPeriod: OverviewSnapshot.ReportingPeriodKind
    @State private var contentHeight: CGFloat = 0
    @AppStorage("privacyModeEnabled") private var privacyModeEnabled = false
    @AppStorage("usageMetric") private var selectedMetric: UsageMetric = .cost

    init(
        store: OverviewStore,
        maximumHeight: CGFloat,
        initialPeriod: OverviewSnapshot.ReportingPeriodKind = .today
    ) {
        self.store = store
        self.maximumHeight = maximumHeight
        _selectedPeriod = State(initialValue: initialPeriod)
    }

    var body: some View {
        let snapshot = store.latestSnapshot
        let overview = store.latestOverview
        let selectedPresentation = overview.flatMap {
            UsagePresentation(snapshot: $0, selectedPeriod: selectedPeriod)
        }
        VStack(spacing: 0) {
            ScrollViewReader { scrollProxy in
                ScrollView {
                    VStack(alignment: .leading, spacing: UsageTheme.sectionGap) {
                        if let snapshot {
                            PanelStatusHeader(
                                snapshot: snapshot,
                                currentCheckSucceeded: store.currentCheckSucceeded,
                                privacyModeEnabled: $privacyModeEnabled
                            )
                            if let overview, let selectedPresentation {
                                UsageSummaryCard(
                                    presentation: selectedPresentation,
                                    selectedPeriod: selectedPeriod,
                                    selectedMetric: selectedMetric,
                                    enabledPeriods: Set(OverviewSnapshot.ReportingPeriodKind.allCases.filter {
                                        overview.supports(selectedMetric, period: $0)
                                    }),
                                    enabledMetrics: Set(UsageMetric.allCases.filter {
                                        overview.supports($0, period: selectedPeriod)
                                    }),
                                    selectPeriod: { selectedPeriod = $0 },
                                    selectMetric: { selectedMetric = $0 }
                                )
                            }
                            ForEach(snapshot.accounts, id: \.key) { account in
                                AccountSection(account: account, hideIdentity: privacyModeEnabled)
                            }
                            RoutedTasksSection(
                                snapshot: snapshot,
                                period: overview?.reportingPeriod(selectedPeriod),
                                selectedPeriod: selectedPeriod,
                                selectedMetric: selectedMetric
                            )
                        } else {
                            EmptyPanelState()
                        }
                    }
                    .padding(.horizontal, UsageTheme.outerInset)
                    .padding(.top, 14)
                    .padding(.bottom, 12)
                    .background {
                        GeometryReader { proxy in
                            Color.clear.preference(
                                key: PanelContentHeightKey.self,
                                value: proxy.size.height
                            )
                        }
                    }
                    .id(PanelScrollAnchor.top)
                }
                .scrollIndicators(
                    PanelSizing.contentRequiresScrolling(
                        contentHeight: contentHeight,
                        maximumPanelHeight: maximumHeight
                    ) ? .automatic : .hidden
                )
                .scrollBounceBehavior(.basedOnSize)
                .frame(height: PanelSizing.scrollHeight(
                    contentHeight: contentHeight,
                    maximumPanelHeight: maximumHeight
                ))
                .onPreferenceChange(PanelContentHeightKey.self) { contentHeight = $0 }
                .onChange(of: selectedPeriod) { _, _ in
                    scrollProxy.scrollTo(PanelScrollAnchor.top, anchor: .top)
                }
                .onChange(of: selectedMetric) { _, _ in
                    scrollProxy.scrollTo(PanelScrollAnchor.top, anchor: .top)
                }
            }

            UsageFooter(store: store)
        }
        .frame(width: UsageTheme.panelWidth)
        .background(UsageTheme.traySurface)
        .onChange(of: store.latestSnapshot, initial: true) { _, snapshot in
            guard let snapshot else { return }
            let overview = OverviewSnapshot(menuSnapshot: snapshot)
            if !overview.supports(selectedMetric, period: selectedPeriod),
               overview.supports(.tokens, period: selectedPeriod) {
                selectedMetric = .tokens
            }
        }
    }
}

enum PanelSizing {
    static let footerHeight: CGFloat = 36

    static func scrollHeight(contentHeight: CGFloat, maximumPanelHeight: CGFloat) -> CGFloat {
        let maximumContentHeight = max(maximumPanelHeight - footerHeight, 120)
        return min(max(contentHeight, 1), maximumContentHeight)
    }

    static func contentRequiresScrolling(
        contentHeight: CGFloat,
        maximumPanelHeight: CGFloat
    ) -> Bool {
        contentHeight > scrollHeight(
            contentHeight: contentHeight,
            maximumPanelHeight: maximumPanelHeight
        )
    }
}

private enum PanelScrollAnchor {
    static let top = "panel-top"
}

private struct PanelContentHeightKey: PreferenceKey {
    static let defaultValue: CGFloat = 0
    static func reduce(value: inout CGFloat, nextValue: () -> CGFloat) {
        let next = nextValue()
        if next > 0 { value = next }
    }
}

private struct EmptyPanelState: View {
    var body: some View {
        ProgressView()
            .controlSize(.small)
            .frame(maxWidth: .infinity, minHeight: 80)
    }
}

private struct PanelStatusHeader: View {
    let snapshot: CSSwitch_MenuSnapshot
    let currentCheckSucceeded: Bool
    @Binding var privacyModeEnabled: Bool

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Text("Balancing \(snapshot.accounts.count) accounts")
                .font(.headline)
            Spacer(minLength: 8)
            if currentCheckSucceeded && snapshot.connection.newTasksConfigured {
                let usable = snapshot.accounts.contains(where: \.usable)
                Label(
                    usable ? "Working" : "Waiting for capacity",
                    systemImage: usable ? "checkmark.circle.fill" : "pause.circle.fill"
                )
                .font(.caption.weight(.medium))
                .foregroundStyle(usable ? Color.green : Color.secondary)
            }
            Button {
                privacyModeEnabled.toggle()
            } label: {
                Image(systemName: privacyModeEnabled ? "lock.fill" : "lock.open")
                    .imageScale(.small)
                    .foregroundStyle(.secondary)
            }
            .buttonStyle(.plain)
            .help(privacyModeEnabled ? "Show account emails" : "Hide account emails")
            .accessibilityLabel(privacyModeEnabled ? "Show account emails" : "Hide account emails")
        }
        .padding(.horizontal, 4)
    }
}
