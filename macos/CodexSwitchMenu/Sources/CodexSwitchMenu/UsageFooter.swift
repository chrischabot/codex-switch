import AppKit
import SwiftUI

struct UsageFooter: View {
    let store: OverviewStore

    var body: some View {
        HStack {
            if let date = store.lastUpdatedAt {
                TimelineView(.periodic(from: .now, by: 10)) { context in
                    Text(updateLabel(date: date, now: context.date))
                }
                .foregroundStyle(.secondary)
            }
            Spacer()
            Menu("Options") {
                Button("Refresh now", systemImage: "arrow.clockwise") {
                    Task { await store.refresh() }
                }
                .disabled(store.isRefreshing)
                Divider()
                Button("Quit", systemImage: "power") {
                    NSApplication.shared.terminate(nil)
                }
            }
            .menuStyle(.borderlessButton)
            .fixedSize()
        }
        .font(.caption)
        .padding(.horizontal, UsageTheme.outerInset)
        .padding(.vertical, 9)
        .background(UsageTheme.traySurface)
        .overlay(alignment: .top) { Divider() }
    }

    private func updateLabel(date: Date, now: Date) -> String {
        let seconds = max(Int(now.timeIntervalSince(date)), 0)
        switch seconds {
        case 0..<5: return "Updated now"
        case 5..<60: return "Updated \(seconds)s ago"
        case 60..<3_600: return "Updated \(seconds / 60)m ago"
        default: return "Updated \(seconds / 3_600)h ago"
        }
    }
}
