import Foundation
import Observation
import OSLog

private let snapshotLogger = Logger(
    subsystem: Bundle.main.bundleIdentifier ?? "CodexSwitchMenu",
    category: "Snapshot"
)

/// Holds the latest menu snapshot. The last good snapshot is kept in the app's
/// caches directory so the menu renders at launch before the first fetch.
@MainActor
@Observable
final class OverviewStore {
    private(set) var latestOverview: OverviewSnapshot?
    var latestSnapshot: CSSwitch_MenuSnapshot? { latestOverview?.menuSnapshot }
    /// When the proxy last confirmed the snapshot the menu shows.
    var lastUpdatedAt: Date? {
        guard let composedAt = latestSnapshot?.generatedAt.date else { return nil }
        return max(composedAt, confirmedAt ?? composedAt)
    }
    private var confirmedAt: Date?
    private(set) var isRefreshing = false
    private(set) var currentCheckSucceeded = false

    private var activeRefreshTask: Task<Void, Never>?
    /// The version of the snapshot shown, which the proxy is asked to improve on.
    private var version: String?
    private let client: any SnapshotLoading
    private let cacheFile: URL

    init(client: any SnapshotLoading, cacheFile: URL) {
        self.client = client
        self.cacheFile = cacheFile
        if let cached = try? MenuSnapshotDecoding.decode(Data(contentsOf: cacheFile)) {
            latestOverview = OverviewSnapshot(menuSnapshot: cached)
        }
    }

    /// Fetches the latest snapshot at once.
    func refresh() async {
        if let activeRefreshTask {
            await activeRefreshTask.value
            return
        }

        isRefreshing = true
        let refreshTask = Task { _ = await self.fetch(after: nil) }
        activeRefreshTask = refreshTask
        await refreshTask.value
        activeRefreshTask = nil
        isRefreshing = false
    }

    func cancelRefresh() {
        activeRefreshTask?.cancel()
    }

    /// Waits until the proxy has a newer snapshot, or confirms the one shown is
    /// current, and takes it. Returns false when the proxy could not be reached.
    func waitForChange() async -> Bool {
        // Older proxy generations answer immediately without a version.
        // Keep their original polling interval during an update or rollback.
        if version == nil, currentCheckSucceeded {
            do {
                try await Task.sleep(for: .seconds(30))
            } catch {
                return true
            }
        }
        return await fetch(after: version)
    }

    private func fetch(after shownVersion: String?) async -> Bool {
        let data: Data
        let latestVersion: String?
        let snapshot: CSSwitch_MenuSnapshot
        do {
            switch try await client.loadSnapshot(after: shownVersion) {
            case let .current(confirmedAt):
                if !currentCheckSucceeded { currentCheckSucceeded = true }
                self.confirmedAt = confirmedAt
                return true
            case let .changed(changedData, changedVersion):
                data = changedData
                latestVersion = changedVersion
            }
            snapshot = try MenuSnapshotDecoding.decode(data)
        } catch is CancellationError {
            return true
        } catch {
            currentCheckSucceeded = false
            snapshotLogger.error("Menu snapshot fetch failed: \(String(describing: error), privacy: .public)")
            return false
        }

        if !currentCheckSucceeded { currentCheckSucceeded = true }
        version = latestVersion
        confirmedAt = nil
        latestOverview = OverviewSnapshot(menuSnapshot: snapshot)
        do {
            try FileManager.default.createDirectory(
                at: cacheFile.deletingLastPathComponent(),
                withIntermediateDirectories: true
            )
            try data.write(to: cacheFile, options: .atomic)
        } catch {
            snapshotLogger.error("Menu snapshot cache write failed: \(error.localizedDescription, privacy: .public)")
        }
        return true
    }
}
