import Foundation
import Observation
import OSLog
import SwiftProtobuf

private let snapshotLogger = Logger(
    subsystem: Bundle.main.bundleIdentifier ?? "CodexSwitchMenu",
    category: "Snapshot"
)

@MainActor
@Observable
final class OverviewStore {
    private static let normalRefreshInterval: TimeInterval = 5
    private static let emptySnapshotRetryInterval: TimeInterval = 5

    private(set) var latestOverview: OverviewSnapshot?
    var latestSnapshot: CSSwitch_MenuSnapshot? { latestOverview?.menuSnapshot }
    var freshReportingOverview: OverviewSnapshot? { latestOverview }
    private(set) var lastUpdatedAt: Date?
    private(set) var isRefreshing = false
    private(set) var currentCheckSucceeded = false

    private var activeRefreshTask: Task<Void, Never>?

    private let client: any SnapshotLoading

    init(client: any SnapshotLoading) {
        self.client = client
    }

    func refresh() async {
        if let activeRefreshTask {
            await activeRefreshTask.value
            return
        }

        isRefreshing = true
        let refreshTask = Task { [weak self] in
            guard let self else { return }
            await self.loadCachedThenCurrentSnapshot()
        }
        activeRefreshTask = refreshTask

        await refreshTask.value
        activeRefreshTask = nil
        isRefreshing = false
    }

    func cancelRefresh() {
        activeRefreshTask?.cancel()
    }

    private func loadCachedThenCurrentSnapshot() async {
        if latestSnapshot == nil {
            do {
                let cached = try await client.loadCachedSnapshot()
                accept(cached)
            } catch is CancellationError {
                return
            } catch {
                snapshotLogger.error(
                    "Cached menu snapshot load failed: \(self.sanitizedSnapshotFailure(error), privacy: .public)"
                )
            }
        }

        do {
            let next = try await client.loadSnapshot()
            accept(next)
            currentCheckSucceeded = true
        } catch is CancellationError {
            currentCheckSucceeded = false
            return
        } catch {
            currentCheckSucceeded = false
            snapshotLogger.error(
                "Menu snapshot refresh failed: \(self.sanitizedSnapshotFailure(error), privacy: .public)"
            )
        }
    }

    func secondsUntilNextRefresh() -> TimeInterval {
        latestSnapshot == nil ? Self.emptySnapshotRetryInterval : Self.normalRefreshInterval
    }

    private func accept(_ next: CSSwitch_MenuSnapshot) {
        latestOverview = OverviewSnapshot(menuSnapshot: next)
        lastUpdatedAt = next.hasGeneratedAt ? next.generatedAt.date : nil
    }

    private func sanitizedSnapshotFailure(_ error: Error) -> String {
        guard let error = error as? SnapshotClientError else {
            return "unexpected helper failure"
        }
        return error.sanitizedLogDescription
    }
}
