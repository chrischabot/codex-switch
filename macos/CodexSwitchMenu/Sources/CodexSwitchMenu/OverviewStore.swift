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
    static let refreshInterval: Duration = .seconds(30)

    private(set) var latestOverview: OverviewSnapshot?
    var latestSnapshot: CSSwitch_MenuSnapshot? { latestOverview?.menuSnapshot }
    var lastUpdatedAt: Date? { latestSnapshot?.generatedAt.date }
    private(set) var isRefreshing = false
    private(set) var currentCheckSucceeded = false

    private var activeRefreshTask: Task<Void, Never>?
    /// The bytes of the snapshot last shown. The proxy composes once a minute
    /// and the menu fetches twice as often, so half the fetches return the
    /// same snapshot; those change nothing and redraw nothing.
    private var latestData: Data?
    private let client: any SnapshotLoading
    private let cacheFile: URL

    init(client: any SnapshotLoading, cacheFile: URL) {
        self.client = client
        self.cacheFile = cacheFile
        if let cached = try? MenuSnapshotDecoding.decode(Data(contentsOf: cacheFile)) {
            latestOverview = OverviewSnapshot(menuSnapshot: cached)
        }
    }

    func refresh() async {
        if let activeRefreshTask {
            await activeRefreshTask.value
            return
        }

        isRefreshing = true
        let refreshTask = Task { await self.fetch() }
        activeRefreshTask = refreshTask
        await refreshTask.value
        activeRefreshTask = nil
        isRefreshing = false
    }

    func cancelRefresh() {
        activeRefreshTask?.cancel()
    }

    private func fetch() async {
        let data: Data
        let snapshot: CSSwitch_MenuSnapshot
        do {
            data = try await client.loadSnapshot()
            snapshot = try MenuSnapshotDecoding.decode(data)
        } catch is CancellationError {
            return
        } catch {
            currentCheckSucceeded = false
            snapshotLogger.error("Menu snapshot fetch failed: \(String(describing: error), privacy: .public)")
            return
        }

        if !currentCheckSucceeded { currentCheckSucceeded = true }
        guard data != latestData else { return }
        latestData = data
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
    }
}
