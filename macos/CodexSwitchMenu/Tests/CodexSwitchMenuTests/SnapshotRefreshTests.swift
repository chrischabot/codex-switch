import Foundation
import Network
import SwiftProtobuf
import XCTest
@testable import CodexSwitchMenu

final class SnapshotRefreshTests: XCTestCase {
    @MainActor
    func testOlderProxyUsesPollingInsteadOfRepeatedImmediateFetches() async throws {
        var snapshot = CSSwitch_MenuSnapshot()
        snapshot.generatedAt = .init(date: .now)
        let server = try LegacySnapshotServer(snapshot: Data(snapshot.serializedBytes()))
        defer { server.stop() }
        await fulfillment(of: [server.ready], timeout: 3)
        let port = try XCTUnwrap(server.port)
        let directory = FileManager.default.temporaryDirectory.appending(path: UUID().uuidString)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: directory) }
        try "http://127.0.0.1:\(port)/snapshot".write(
            to: directory.appending(path: "menu-snapshot-url"), atomically: true, encoding: .utf8
        )
        let store = OverviewStore(client: SnapshotClient(dataDirectory: directory), cacheFile: directory.appending(path: "cache.pb"))
        await store.refresh()
        XCTAssertTrue(store.currentCheckSucceeded)
        XCTAssertEqual(server.requestCount, 1)

        let waiting = Task { await store.waitForChange() }
        try await Task.sleep(for: .milliseconds(150))
        waiting.cancel()
        _ = await waiting.value
        XCTAssertEqual(server.requestCount, 1, "An older proxy must not turn the change-wait loop into immediate HTTP polling")

        // Opening the menu still asks for fresh data immediately.
        await store.refresh()
        XCTAssertEqual(server.requestCount, 2)
        XCTAssertTrue(store.currentCheckSucceeded)
    }
}

/// A real loopback HTTP endpoint using the pre-versioned snapshot protocol.
private final class LegacySnapshotServer: @unchecked Sendable {
    let ready = XCTestExpectation(description: "snapshot server listening")
    private let listener: NWListener
    private let queue = DispatchQueue(label: "snapshot-refresh-test")
    private let lock = NSLock()
    private var requests = 0
    private let response: Data

    var port: UInt16? { listener.port?.rawValue }
    var requestCount: Int { lock.withLock { requests } }

    init(snapshot: Data) throws {
        let parameters = NWParameters.tcp
        parameters.requiredLocalEndpoint = .hostPort(host: "127.0.0.1", port: .any)
        listener = try NWListener(using: parameters)
        response = Data("HTTP/1.1 200 OK\r\nContent-Type: application/x-protobuf\r\nContent-Length: \(snapshot.count)\r\nConnection: close\r\n\r\n".utf8) + snapshot
        listener.stateUpdateHandler = { [weak self] state in
            if case .ready = state { self?.ready.fulfill() }
        }
        listener.newConnectionHandler = { [weak self] connection in
            guard let self else { connection.cancel(); return }
            connection.start(queue: self.queue)
            connection.receive(minimumIncompleteLength: 1, maximumLength: 65_536) { [weak self] _, _, _, error in
                guard let self, error == nil else { connection.cancel(); return }
                self.lock.withLock { self.requests += 1 }
                connection.send(content: self.response, completion: .contentProcessed { _ in connection.cancel() })
            }
        }
        listener.start(queue: queue)
    }

    func stop() { listener.cancel() }
}
