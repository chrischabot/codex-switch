import Darwin
import Foundation
import SwiftProtobuf
import XCTest
@testable import CodexSwitchMenu

final class ClientTests: XCTestCase {
    func testStagedLaunchConfigurationRequiresOneCompleteIsolatedTarget() throws {
        let configuration = try SnapshotLaunchConfiguration(arguments: [
            "--proxy-address", "127.0.0.1:43127",
            "--data-dir", "/tmp/codex-switch-stage/data",
            "--codex-home", "/tmp/codex-switch-stage/codex-home",
        ])

        XCTAssertEqual(configuration.proxyAddress, "127.0.0.1:43127")
        XCTAssertEqual(configuration.helperEnvironment, [
            "CODEX_SWITCH_DATA_DIR": "/tmp/codex-switch-stage/data",
            "CODEX_HOME": "/tmp/codex-switch-stage/codex-home",
        ])
        XCTAssertEqual(try SnapshotLaunchConfiguration(arguments: []), .production)

        XCTAssertThrowsError(try SnapshotLaunchConfiguration(arguments: [
            "--proxy-address", "127.0.0.1:43127",
            "--data-dir", "/tmp/codex-switch-stage/data",
        ])) { error in
            XCTAssertEqual(
                error as? SnapshotLaunchConfigurationError,
                .incompleteStagedConfiguration
            )
        }
        XCTAssertThrowsError(try SnapshotLaunchConfiguration(arguments: [
            "--proxy-address", "localhost:43127",
            "--data-dir", "/tmp/codex-switch-stage/data",
            "--codex-home", "/tmp/codex-switch-stage/codex-home",
        ])) { error in
            XCTAssertEqual(
                error as? SnapshotLaunchConfigurationError,
                .invalidProxyAddress
            )
        }
    }

    func testRunnerPassesStagedDirectoriesToTheHelperChild() async throws {
        let runner = FoundationCommandRunner(
            timeout: 1,
            environment: [
                "CODEX_SWITCH_DATA_DIR": "/tmp/staged-data",
                "CODEX_HOME": "/tmp/staged-codex-home",
            ]
        )

        let dataDirectory = try await runner.run(
            executable: URL(fileURLWithPath: "/usr/bin/printenv"),
            arguments: ["CODEX_SWITCH_DATA_DIR"]
        )
        let codexHome = try await runner.run(
            executable: URL(fileURLWithPath: "/usr/bin/printenv"),
            arguments: ["CODEX_HOME"]
        )

        XCTAssertEqual(String(decoding: dataDirectory.standardOutput, as: UTF8.self), "/tmp/staged-data\n")
        XCTAssertEqual(String(decoding: codexHome.standardOutput, as: UTF8.self), "/tmp/staged-codex-home\n")
    }

    func testSnapshotErrorsExposeOnlySanitizedLogDetails() {
        XCTAssertEqual(
            SnapshotClientError.commandFailed(9, "secret path and token").sanitizedLogDescription,
            "helper process exited with status 9"
        )
    }

    func testRunnerDrainsStandardOutputWhileChildRuns() async throws {
        let runner = FoundationCommandRunner(
            timeout: 1,
            terminationGrace: 0.02,
            maximumOutputBytes: 1_048_576
        )

        let result = try await runner.run(
            executable: URL(fileURLWithPath: "/usr/bin/head"),
            arguments: ["-c", "1048576", "/dev/zero"]
        )

        XCTAssertEqual(result.exitCode, 0)
        XCTAssertEqual(result.standardOutput.count, 1_048_576)
    }

    func testRunnerDrainsStandardErrorAndReturnsItToTheClient() async throws {
        let runner = FoundationCommandRunner(
            timeout: 1,
            terminationGrace: 0.02,
            maximumErrorBytes: 128 * 1_024
        )

        let result = try await runner.run(
            executable: URL(fileURLWithPath: "/bin/sh"),
            arguments: ["-c", "/usr/bin/head -c 65536 /dev/zero >&2"]
        )

        XCTAssertEqual(result.exitCode, 0)
        XCTAssertEqual(result.standardError.count, 65_536)
    }

    func testRunnerTimesOutAndKillsStuckChild() async throws {
        let child = try StuckChild()
        defer { child.cleanUp() }
        let runner = FoundationCommandRunner(timeout: 0.05, terminationGrace: 0.02)

        do {
            _ = try await runner.run(executable: child.executable, arguments: child.arguments)
            XCTFail("Expected timeout")
        } catch {
            XCTAssertEqual(error as? SnapshotClientError, .commandTimedOut)
        }

        let pid = try await child.pid()
        await assertProcessIsGone(pid)
    }

    func testRunnerStopsOverLimitProducerWithoutRetainingOutput() async throws {
        let child = try OutputProducer()
        defer { child.cleanUp() }
        let runner = FoundationCommandRunner(
            timeout: 1,
            terminationGrace: 0.02,
            maximumOutputBytes: 64 * 1_024
        )

        do {
            _ = try await runner.run(executable: child.executable, arguments: child.arguments)
            XCTFail("Expected oversized output failure")
        } catch {
            XCTAssertEqual(error as? SnapshotClientError, .outputTooLarge)
            XCTAssertFalse(error.localizedDescription.contains("sensitive-overflow"))
        }

        let pid = try await child.pid()
        await assertProcessIsGone(pid)
    }

    func testDefaultLocatorPrefersTheBundledHelper() throws {
        let directory = FileManager.default.temporaryDirectory
            .appending(path: "CodexSwitchLocatorTests-\(UUID())", directoryHint: .isDirectory)
        defer { try? FileManager.default.removeItem(at: directory) }
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        let current = directory.appending(path: "current")
        let bundled = directory.appending(path: "bundled")
        FileManager.default.createFile(atPath: current.path, contents: Data())
        FileManager.default.createFile(atPath: bundled.path, contents: Data())
        try FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: current.path)
        try FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: bundled.path)

        let located = try DefaultBinaryLocator(current: current, bundled: bundled).locate()

        XCTAssertEqual(located, bundled)
    }

    func testDefaultLocatorFallsBackToTheStableCLI() throws {
        let directory = FileManager.default.temporaryDirectory
            .appending(path: "CodexSwitchLocatorTests-\(UUID())", directoryHint: .isDirectory)
        defer { try? FileManager.default.removeItem(at: directory) }
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        let current = directory.appending(path: "current")
        let bundled = directory.appending(path: "bundled")
        FileManager.default.createFile(atPath: current.path, contents: Data())
        FileManager.default.createFile(atPath: bundled.path, contents: Data())
        try FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: current.path)
        try FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: bundled.path)

        let located = try DefaultBinaryLocator(current: current, bundled: bundled).locate()

        XCTAssertEqual(located, current)
    }

    func testDefaultLocatorReportsNotFoundWhenNeitherBinaryIsExecutable() throws {
        let directory = FileManager.default.temporaryDirectory
            .appending(path: "CodexSwitchLocatorTests-\(UUID())", directoryHint: .isDirectory)
        defer { try? FileManager.default.removeItem(at: directory) }
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        let current = directory.appending(path: "current")
        let bundled = directory.appending(path: "bundled")
        FileManager.default.createFile(atPath: current.path, contents: Data())
        FileManager.default.createFile(atPath: bundled.path, contents: Data())
        try FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: current.path)
        try FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: bundled.path)

        XCTAssertThrowsError(
            try DefaultBinaryLocator(current: current, bundled: bundled).locate()
        ) { error in
            XCTAssertEqual(error as? SnapshotClientError, .binaryNotFound)
        }
    }
    func testClientLoadsProtobufWithExplicitAddressForBothModes() async throws {
        var snapshot = CSSwitch_MenuSnapshot()
        snapshot.generatedAt = .init(date: Date(timeIntervalSince1970: 12345))
        let recorder = InvocationRecorder()
        let client = CodexSwitchClient(locator: StubLocator(url: URL(fileURLWithPath: "/test/helper")), runner: StubRunner(result: .init(exitCode: 0, standardOutput: try snapshot.serializedData()), recorder: recorder), proxyAddress: "127.0.0.1:43127")
        let cached = try await client.loadCachedSnapshot()
        XCTAssertEqual(cached, snapshot)
        let cachedInvocation = await recorder.invocation
        XCTAssertEqual(cachedInvocation?.arguments, ["menu-snapshot", "--cached-only", "--address", "127.0.0.1:43127"])
        let current = try await client.loadSnapshot()
        XCTAssertEqual(current, snapshot)
        let currentInvocation = await recorder.invocation
        XCTAssertEqual(currentInvocation?.arguments, ["menu-snapshot", "--address", "127.0.0.1:43127"])
    }


    func testClientRejectsDefaultOrIncompleteSnapshotFacts() async throws {
        var missingIdentity = CSSwitch_MenuSnapshot()
        missingIdentity.generatedAt = .init(date: Date(timeIntervalSince1970: 12345))
        missingIdentity.accounts = [CSSwitch_Account()]
        for bytes in [Data([0x28, 0x01]), try missingIdentity.serializedData()] {
            let client = CodexSwitchClient(locator: StubLocator(url: URL(fileURLWithPath: "/test/helper")), runner: StubRunner(result: .init(exitCode: 0, standardOutput: bytes), recorder: InvocationRecorder()))
            do {
                _ = try await client.loadSnapshot()
                XCTFail("Incomplete snapshot accepted")
            } catch {
                XCTAssertEqual(error as? SnapshotClientError, .invalidSnapshot)
            }
        }
    }

    func testClientRejectsMalformedProtobuf() async throws {
        let client = CodexSwitchClient(locator: StubLocator(url: URL(fileURLWithPath: "/test/helper")), runner: StubRunner(result: .init(exitCode: 0, standardOutput: Data([0xFF])), recorder: InvocationRecorder()))
        do {
            _ = try await client.loadSnapshot()
            XCTFail("Malformed protobuf accepted")
        } catch {
            XCTAssertEqual(error as? SnapshotClientError, .invalidSnapshot)
        }
    }

    @MainActor
    func testCacheAppearsBeforeRefreshAndSurvivesRefreshFailure() async throws {
        var snapshot = CSSwitch_MenuSnapshot()
        snapshot.generatedAt = .init(date: Date(timeIntervalSince1970: 12345))
        let loader = PausedSnapshotLoader(snapshot: snapshot)
        let store = OverviewStore(client: loader)
        let refresh = Task { await store.refresh() }
        await loader.waitUntilRefreshStarts()
        XCTAssertEqual(store.latestSnapshot, snapshot)
        XCTAssertEqual(store.lastUpdatedAt, snapshot.generatedAt.date)
        XCTAssertTrue(store.isRefreshing)
        XCTAssertFalse(store.currentCheckSucceeded)
        await loader.finishRefresh()
        await refresh.value
        XCTAssertEqual(store.latestSnapshot, snapshot)
        XCTAssertEqual(store.lastUpdatedAt, snapshot.generatedAt.date)
        XCTAssertFalse(store.isRefreshing)
        XCTAssertFalse(store.currentCheckSucceeded)
    }

    @MainActor
    func testRealHelperFailureClearsWorkingStateAndRecoveryRestoresIt() async throws {
        let environment = ProcessInfo.processInfo.environment
        guard let helper = environment["CODEX_SWITCH_TEST_HELPER"],
              let directory = environment["CODEX_SWITCH_TEST_DATA_DIR"],
              let codexHome = environment["CODEX_SWITCH_TEST_CODEX_HOME"],
              let address = environment["CODEX_SWITCH_TEST_PROXY_ADDRESS"] else {
            throw XCTSkip("Requires a packaged helper and an explicitly selected isolated serving proxy")
        }
        let runner = FoundationCommandRunner(environment: ["CODEX_SWITCH_DATA_DIR": directory, "CODEX_HOME": codexHome])
        let locator = StubLocator(url: URL(fileURLWithPath: helper))
        let loader = SelectedEndpointLoader(
            serving: CodexSwitchClient(locator: locator, runner: runner, proxyAddress: address),
            unreachable: CodexSwitchClient(locator: locator, runner: runner, proxyAddress: "127.0.0.1:1")
        )
        let store = OverviewStore(client: loader)
        await store.refresh()
        XCTAssertTrue(store.currentCheckSucceeded)
        let current = try XCTUnwrap(store.latestSnapshot)
        XCTAssertFalse(current.accounts.isEmpty)
        XCTAssertFalse(current.completedTasks.isEmpty)

        await loader.selectUnreachableEndpoint(true)
        await store.refresh()
        XCTAssertFalse(store.currentCheckSucceeded)
        XCTAssertEqual(store.latestSnapshot, current)

        let cachedStore = OverviewStore(client: loader)
        await cachedStore.refresh()
        XCTAssertFalse(cachedStore.currentCheckSucceeded)
        XCTAssertEqual(cachedStore.latestSnapshot, current)

        await loader.selectUnreachableEndpoint(false)
        await store.refresh()
        XCTAssertTrue(store.currentCheckSucceeded)
        XCTAssertEqual(store.latestSnapshot?.completedTasks, current.completedTasks)
    }

    func testRealHelperCachedProtobufWithPrivateDirectories() async throws {
        guard let helperPath = ProcessInfo.processInfo.environment["CODEX_SWITCH_TEST_HELPER"] else {
            throw XCTSkip("Set CODEX_SWITCH_TEST_HELPER to an explicitly built helper")
        }
        let directory = FileManager.default.temporaryDirectory.appending(path: "SnapshotHelper-\(UUID())", directoryHint: .isDirectory)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: directory) }
        var cached = CSSwitch_MenuSnapshot()
        cached.generatedAt = .init(date: Date(timeIntervalSince1970: 12345))
        try cached.serializedData().write(to: directory.appending(path: "menu-snapshot.pb"))
        let runner = FoundationCommandRunner(environment: ["CODEX_SWITCH_DATA_DIR": directory.path, "CODEX_HOME": directory.appending(path: "home").path])
        let client = CodexSwitchClient(locator: StubLocator(url: URL(fileURLWithPath: helperPath)), runner: runner, proxyAddress: "127.0.0.1:1")
        let snapshot = try await client.loadCachedSnapshot()
        XCTAssertEqual(snapshot, cached)
    }
}

private struct StuckChild {
    let directory: URL
    let pidFile: URL
    let fifo: URL

    var executable: URL { URL(fileURLWithPath: "/bin/sh") }

    var arguments: [String] {
        [
            "-c",
            "trap '' TERM; printf '%s' \"$$\" > \"$1\"; read line < \"$2\"",
            "stuck-child",
            pidFile.path,
            fifo.path,
        ]
    }

    init() throws {
        let directory = FileManager.default.temporaryDirectory
            .appending(path: "CodexSwitchMenuTests-\(UUID())", directoryHint: .isDirectory)
        let pidFile = directory.appending(path: "pid")
        let fifo = directory.appending(path: "wait")
        self.directory = directory
        self.pidFile = pidFile
        self.fifo = fifo
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        guard Darwin.mkfifo(fifo.path, S_IRUSR | S_IWUSR) == 0 else {
            throw POSIXError(POSIXErrorCode(rawValue: errno) ?? .EIO)
        }
    }

    func pid() async throws -> pid_t {
        let deadline = ContinuousClock.now + .seconds(1)
        while ContinuousClock.now < deadline {
            if let text = try? String(contentsOf: pidFile, encoding: .utf8),
               let pid = pid_t(text) {
                return pid
            }
            try await Task.sleep(for: .milliseconds(5))
        }
        throw TestError.childDidNotStart
    }

    func cleanUp() {
        if let text = try? String(contentsOf: pidFile, encoding: .utf8),
           let pid = pid_t(text),
           processExists(pid) {
            Darwin.kill(pid, SIGKILL)
        }
        try? FileManager.default.removeItem(at: directory)
    }
}

private struct OutputProducer {
    let directory: URL
    let pidFile: URL

    var executable: URL { URL(fileURLWithPath: "/bin/sh") }

    var arguments: [String] {
        [
            "-c",
            "printf '%s' \"$$\" > \"$1\"; exec /usr/bin/yes sensitive-overflow",
            "output-producer",
            pidFile.path,
        ]
    }

    init() throws {
        let directory = FileManager.default.temporaryDirectory
            .appending(path: "CodexSwitchMenuTests-\(UUID())", directoryHint: .isDirectory)
        self.directory = directory
        pidFile = directory.appending(path: "pid")
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
    }

    func pid() async throws -> pid_t {
        let deadline = ContinuousClock.now + .seconds(1)
        while ContinuousClock.now < deadline {
            if let text = try? String(contentsOf: pidFile, encoding: .utf8),
               let pid = pid_t(text) {
                return pid
            }
            try await Task.sleep(for: .milliseconds(5))
        }
        throw TestError.childDidNotStart
    }

    func cleanUp() {
        if let text = try? String(contentsOf: pidFile, encoding: .utf8),
           let pid = pid_t(text),
           processExists(pid) {
            Darwin.kill(pid, SIGKILL)
        }
        try? FileManager.default.removeItem(at: directory)
    }
}

private enum TestError: Error {
    case childDidNotStart
}

private func assertProcessIsGone(
    _ pid: pid_t,
    file: StaticString = #filePath,
    line: UInt = #line
) async {
    let deadline = ContinuousClock.now + .seconds(1)
    while ContinuousClock.now < deadline {
        if !processExists(pid) {
            return
        }
        try? await Task.sleep(for: .milliseconds(5))
    }
    XCTFail("Child process \(pid) is still running", file: file, line: line)
}

private func processExists(_ pid: pid_t) -> Bool {
    errno = 0
    return Darwin.kill(pid, 0) == 0 || errno != ESRCH
}

private struct StubLocator: BinaryLocating {
    let url: URL

    func locate() throws -> URL { url }
}

private struct StubRunner: CommandRunning {
    let result: CommandResult
    let recorder: InvocationRecorder

    func run(executable: URL, arguments: [String]) async throws -> CommandResult {
        await recorder.record(executable: executable, arguments: arguments)
        return result
    }
}

private actor InvocationRecorder {
    struct Invocation: Equatable {
        let executable: URL
        let arguments: [String]
    }

    private(set) var invocation: Invocation?

    func record(executable: URL, arguments: [String]) {
        invocation = Invocation(executable: executable, arguments: arguments)
    }
}

private actor PausedSnapshotLoader: SnapshotLoading {
    let snapshot: CSSwitch_MenuSnapshot
    private var continuation: CheckedContinuation<Void, Never>?
    init(snapshot: CSSwitch_MenuSnapshot) { self.snapshot = snapshot }
    func loadCachedSnapshot() async throws -> CSSwitch_MenuSnapshot { snapshot }
    func loadSnapshot() async throws -> CSSwitch_MenuSnapshot {
        await withCheckedContinuation { continuation = $0 }
        throw SnapshotClientError.commandFailed(1, "sensitive helper diagnostic")
    }
    func waitUntilRefreshStarts() async {
        while continuation == nil { await Task.yield() }
    }
    func finishRefresh() { continuation?.resume(); continuation = nil }
}

private actor SelectedEndpointLoader: SnapshotLoading {
    let serving: any SnapshotLoading
    let unreachable: any SnapshotLoading
    private var useUnreachableEndpoint = false

    init(serving: any SnapshotLoading, unreachable: any SnapshotLoading) {
        self.serving = serving
        self.unreachable = unreachable
    }
    func selectUnreachableEndpoint(_ selected: Bool) { useUnreachableEndpoint = selected }
    func loadCachedSnapshot() async throws -> CSSwitch_MenuSnapshot {
        try await serving.loadCachedSnapshot()
    }
    func loadSnapshot() async throws -> CSSwitch_MenuSnapshot {
        try await (useUnreachableEndpoint ? unreachable : serving).loadSnapshot()
    }
}
