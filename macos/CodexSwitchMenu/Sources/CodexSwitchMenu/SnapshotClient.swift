import Darwin
import Foundation
import OSLog
import SwiftProtobuf

private let snapshotDecoderLogger = Logger(
    subsystem: Bundle.main.bundleIdentifier ?? "CodexSwitchMenu",
    category: "SnapshotDecoder"
)

protocol SnapshotLoading: Sendable {
    func loadCachedSnapshot() async throws -> CSSwitch_MenuSnapshot
    func loadSnapshot() async throws -> CSSwitch_MenuSnapshot
}

protocol BinaryLocating: Sendable {
    func locate() throws -> URL
}

protocol CommandRunning: Sendable {
    func run(executable: URL, arguments: [String]) async throws -> CommandResult
}

struct CommandResult: Equatable, Sendable {
    let exitCode: Int32
    let standardOutput: Data
    let standardError: Data

    init(exitCode: Int32, standardOutput: Data, standardError: Data = Data()) {
        self.exitCode = exitCode
        self.standardOutput = standardOutput
        self.standardError = standardError
    }
}

enum SnapshotClientError: Error, Equatable, LocalizedError {
    case binaryNotFound
    case launchFailed
    case commandTimedOut
    case outputTooLarge
    case commandFailed(Int32, String)
    case invalidSnapshot

    var errorDescription: String? {
        switch self {
        case .binaryNotFound:
            "codex-switch is not installed"
        case .launchFailed:
            "Could not start codex-switch"
        case .commandTimedOut:
            "codex-switch did not respond in time"
        case .outputTooLarge:
            "codex-switch returned too much data"
        case let .commandFailed(status, detail):
            detail.isEmpty
                ? "codex-switch failed (status \(status))"
                : "codex-switch failed (status \(status)): \(detail)"
        case .invalidSnapshot:
            "codex-switch returned an invalid snapshot"
        }
    }

    var sanitizedLogDescription: String {
        switch self {
        case .binaryNotFound:
            "helper executable was not found"
        case .launchFailed:
            "helper process could not start"
        case .commandTimedOut:
            "helper process timed out"
        case .outputTooLarge:
            "helper output exceeded its size limit"
        case let .commandFailed(status, _):
            "helper process exited with status \(status)"
        case .invalidSnapshot:
            "helper returned an invalid menu snapshot"
        }
    }
}

enum SnapshotLaunchConfigurationError: Error, Equatable {
    case unknownArgument(String)
    case missingValue(String)
    case duplicateArgument(String)
    case incompleteStagedConfiguration
    case invalidProxyAddress
    case relativeDirectory(String)
}

struct SnapshotLaunchConfiguration: Equatable, Sendable {
    static let production = SnapshotLaunchConfiguration(
        proxyAddress: nil,
        helperEnvironment: [:]
    )

    let proxyAddress: String?
    let helperEnvironment: [String: String]

    init(arguments: [String]) throws {
        let supportedArguments = ["--proxy-address", "--data-dir", "--codex-home"]
        var values: [String: String] = [:]
        var index = 0
        while index < arguments.count {
            let argument = arguments[index]
            guard supportedArguments.contains(argument) else {
                throw SnapshotLaunchConfigurationError.unknownArgument(argument)
            }
            guard values[argument] == nil else {
                throw SnapshotLaunchConfigurationError.duplicateArgument(argument)
            }
            index += 1
            guard index < arguments.count else {
                throw SnapshotLaunchConfigurationError.missingValue(argument)
            }
            let value = arguments[index].trimmingCharacters(in: .whitespacesAndNewlines)
            guard !value.isEmpty, !supportedArguments.contains(value) else {
                throw SnapshotLaunchConfigurationError.missingValue(argument)
            }
            values[argument] = value
            index += 1
        }

        guard !values.isEmpty else {
            self = .production
            return
        }
        guard let proxyAddress = values["--proxy-address"],
              let dataDirectory = values["--data-dir"],
              let codexHome = values["--codex-home"] else {
            throw SnapshotLaunchConfigurationError.incompleteStagedConfiguration
        }
        guard Self.isNumericLoopbackAddress(proxyAddress) else {
            throw SnapshotLaunchConfigurationError.invalidProxyAddress
        }
        for directory in [dataDirectory, codexHome] where !directory.hasPrefix("/") {
            throw SnapshotLaunchConfigurationError.relativeDirectory(directory)
        }

        self.init(
            proxyAddress: proxyAddress,
            helperEnvironment: [
                "CODEX_SWITCH_DATA_DIR": dataDirectory,
                "CODEX_HOME": codexHome,
            ]
        )
    }

    private init(proxyAddress: String?, helperEnvironment: [String: String]) {
        self.proxyAddress = proxyAddress
        self.helperEnvironment = helperEnvironment
    }

    private static func isNumericLoopbackAddress(_ address: String) -> Bool {
        guard let components = URLComponents(string: "http://\(address)"),
              let host = components.host,
              components.port != nil else {
            return false
        }

        var ipv4 = in_addr()
        if host.withCString({ inet_pton(AF_INET, $0, &ipv4) }) == 1 {
            return withUnsafeBytes(of: &ipv4) { $0.first == 127 }
        }

        var ipv6 = in6_addr()
        if host.withCString({ inet_pton(AF_INET6, $0, &ipv6) }) == 1 {
            return withUnsafeBytes(of: &ipv6) { bytes in
                bytes.dropLast().allSatisfy { $0 == 0 } && bytes.last == 1
            }
        }
        return false
    }
}

struct DefaultBinaryLocator: BinaryLocating {
    private let current: URL
    private let bundled: URL

    init(
        current: URL = FileManager.default.homeDirectoryForCurrentUser
            .appending(path: ".local/bin/codex-switch"),
        bundled: URL = Bundle.main.bundleURL
            .appending(path: "Contents/Helpers/codex-switch")
    ) {
        self.current = current
        self.bundled = bundled
    }

    func locate() throws -> URL {
        if FileManager.default.isExecutableFile(atPath: bundled.path) {
            return bundled
        }

        guard FileManager.default.isExecutableFile(atPath: current.path) else {
            throw SnapshotClientError.binaryNotFound
        }
        return current
    }
}

struct FoundationCommandRunner: CommandRunning {
    private let timeout: TimeInterval?
    private let terminationGrace: TimeInterval
    private let maximumOutputBytes: Int
    private let maximumErrorBytes: Int
    private let environment: [String: String]

    init(
        timeout: TimeInterval? = 10,
        terminationGrace: TimeInterval = 0.1,
        maximumOutputBytes: Int = 8 * 1_024 * 1_024,
        maximumErrorBytes: Int = 64 * 1_024,
        environment: [String: String] = [:]
    ) {
        self.timeout = timeout
        self.terminationGrace = terminationGrace
        self.maximumOutputBytes = maximumOutputBytes
        self.maximumErrorBytes = maximumErrorBytes
        self.environment = environment
    }

    func run(executable: URL, arguments: [String]) async throws -> CommandResult {
        let command = RunningCommand(
            executable: executable,
            arguments: arguments,
            timeout: timeout,
            terminationGrace: terminationGrace,
            maximumOutputBytes: maximumOutputBytes,
            maximumErrorBytes: maximumErrorBytes,
            environment: environment
        )
        return try await withTaskCancellationHandler {
            try Task.checkCancellation()
            try command.start()
            return try await command.waitForResult()
        } onCancel: {
            command.cancel()
        }
    }
}

private final class RunningCommand: @unchecked Sendable {
    private let lock = NSLock()
    private let process: Process
    private let output: Pipe
    private let errorOutput: Pipe
    private let terminationEvents: AsyncStream<Int32>
    private let timeout: TimeInterval?
    private let terminationGrace: TimeInterval
    private let maximumOutputBytes: Int
    private let maximumErrorBytes: Int

    private var launchedPID: pid_t?
    private var stopRequested = false
    private var terminationSent = false

    init(
        executable: URL,
        arguments: [String],
        timeout: TimeInterval?,
        terminationGrace: TimeInterval,
        maximumOutputBytes: Int,
        maximumErrorBytes: Int,
        environment: [String: String]
    ) {
        let process = Process()
        let output = Pipe()
        let errorOutput = Pipe()
        let (terminationEvents, terminationContinuation) = AsyncStream<Int32>.makeStream()
        process.executableURL = executable
        process.arguments = arguments
        process.standardInput = FileHandle.nullDevice
        process.standardOutput = output
        process.standardError = errorOutput
        process.environment = ProcessInfo.processInfo.environment.merging(environment) {
            _, stagedValue in stagedValue
        }
        process.terminationHandler = { process in
            terminationContinuation.yield(process.terminationStatus)
            terminationContinuation.finish()
        }

        self.process = process
        self.output = output
        self.errorOutput = errorOutput
        self.terminationEvents = terminationEvents
        self.timeout = timeout
        self.terminationGrace = terminationGrace
        self.maximumOutputBytes = maximumOutputBytes
        self.maximumErrorBytes = maximumErrorBytes
    }

    func start() throws {
        guard lock.withLock({ !stopRequested }) else {
            throw CancellationError()
        }
        do {
            try process.run()
        } catch {
            throw SnapshotClientError.launchFailed
        }

        let shouldStop = lock.withLock {
            launchedPID = process.processIdentifier
            return stopRequested
        }
        if shouldStop {
            cancel()
        }
    }

    func waitForResult() async throws -> CommandResult {
        try await withThrowingTaskGroup(of: CommandEvent.self) { group in
            group.addTask { [self] in
                .output(try read(pipe: output, maximumBytes: maximumOutputBytes))
            }
            group.addTask { [self] in
                .errorOutput(try read(pipe: errorOutput, maximumBytes: maximumErrorBytes))
            }
            group.addTask { [self] in
                for await status in terminationEvents {
                    return .exit(status)
                }
                try Task.checkCancellation()
                throw SnapshotClientError.launchFailed
            }
            if let timeout {
                group.addTask {
                    try await Task.sleep(for: .seconds(timeout))
                    throw SnapshotClientError.commandTimedOut
                }
            }

            var output: Data?
            var standardError: Data?
            var exitCode: Int32?
            do {
                while let event = try await group.next() {
                    switch event {
                    case let .output(data):
                        output = data
                    case let .errorOutput(data):
                        standardError = data
                    case let .exit(status):
                        exitCode = status
                    }
                    if let output, let standardError, let exitCode {
                        try Task.checkCancellation()
                        group.cancelAll()
                        return CommandResult(
                            exitCode: exitCode,
                            standardOutput: output,
                            standardError: standardError
                        )
                    }
                }
            } catch {
                group.cancelAll()
                stop()
                if error is CancellationError || error is SnapshotClientError {
                    throw error
                }
                throw SnapshotClientError.launchFailed
            }
            throw SnapshotClientError.launchFailed
        }
    }

    private func read(pipe: Pipe, maximumBytes: Int) throws -> Data {
        var data = Data()
        while true {
            let remaining = maximumBytes - data.count
            let readSize = remaining > 0 ? min(64 * 1_024, remaining) : 1
            let chunk = try pipe.fileHandleForReading.read(
                upToCount: readSize
            ) ?? Data()
            guard !chunk.isEmpty else { return data }
            data.append(chunk)
            guard data.count <= maximumBytes else {
                data.removeAll(keepingCapacity: false)
                throw SnapshotClientError.outputTooLarge
            }
        }
    }

    func stop() {
        let pid = processIdentifierToStop()
        guard let pid else { return }
        process.terminate()
        DispatchQueue.global(qos: .utility).asyncAfter(deadline: .now() + terminationGrace) {
            self.forceKill(pid: pid)
        }
    }

    func cancel() {
        let pid = processIdentifierToStop()
        guard let pid else { return }
        process.terminate()
        forceKill(pid: pid)
    }

    private func processIdentifierToStop() -> pid_t? {
        lock.withLock {
            stopRequested = true
            guard !terminationSent,
                  let launchedPID,
                  process.isRunning else {
                return nil
            }
            terminationSent = true
            return launchedPID
        }
    }

    private func forceKill(pid: pid_t) {
        let shouldKill = lock.withLock {
            launchedPID == pid
                && process.processIdentifier == pid
                && process.isRunning
        }
        if shouldKill {
            Darwin.kill(pid, SIGKILL)
        }
    }

    private enum CommandEvent: Sendable {
        case output(Data)
        case errorOutput(Data)
        case exit(Int32)
    }
}

struct CodexSwitchClient<Locator: BinaryLocating, Runner: CommandRunning>: SnapshotLoading {
    private static var maximumSnapshotBytes: Int { 8 * 1_024 * 1_024 }

    private let locator: Locator
    private let runner: Runner
    private let proxyAddress: String?

    init(locator: Locator, runner: Runner, proxyAddress: String? = nil) {
        self.locator = locator
        self.runner = runner
        self.proxyAddress = Self.normalized(proxyAddress)
    }

    func loadCachedSnapshot() async throws -> CSSwitch_MenuSnapshot {
        try await load(cachedOnly: true)
    }

    func loadSnapshot() async throws -> CSSwitch_MenuSnapshot {
        try await load(cachedOnly: false)
    }

    private func load(cachedOnly: Bool) async throws -> CSSwitch_MenuSnapshot {
        let executable = try locator.locate()
        var arguments = ["menu-snapshot"]
        if cachedOnly { arguments.append("--cached-only") }
        if let proxyAddress { arguments.append(contentsOf: ["--address", proxyAddress]) }
        return try decode(await runner.run(executable: executable, arguments: arguments))
    }

    private static func normalized(_ value: String?) -> String? {
        guard let value = value?.trimmingCharacters(in: .whitespacesAndNewlines),
              !value.isEmpty else {
            return nil
        }
        return value
    }

    private func decode(_ result: CommandResult) throws -> CSSwitch_MenuSnapshot {
        guard result.exitCode == 0 else {
            throw SnapshotClientError.commandFailed(
                result.exitCode,
                commandFailureDetail(result.standardError)
            )
        }
        guard !result.standardOutput.isEmpty,
              result.standardOutput.count <= Self.maximumSnapshotBytes else {
            throw SnapshotClientError.invalidSnapshot
        }
        do {
            let snapshot = try CSSwitch_MenuSnapshot(serializedBytes: result.standardOutput)
            guard snapshot.hasGeneratedAt,
                  snapshot.accounts.allSatisfy({ account in
                      !account.key.isEmpty && account.accountNumber > 0 &&
                      (!account.hasAllowance || (account.allowance.hasObservedAt &&
                          account.allowance.windows.allSatisfy { window in
                              window.periodSeconds > 0 && window.usedPercent.isFinite &&
                              (0...100).contains(window.usedPercent) && window.hasResetAt
                          }))
                  }) else {
                throw SnapshotClientError.invalidSnapshot
            }
            return snapshot
        } catch {
            snapshotDecoderLogger.error(
                "Menu snapshot protobuf decode failed"
            )
            throw SnapshotClientError.invalidSnapshot
        }
    }

    private func commandFailureDetail(_ data: Data) -> String {
        let raw = String(decoding: data.prefix(4_096), as: UTF8.self)
        let printable = raw.unicodeScalars.map { scalar in
            CharacterSet.controlCharacters.contains(scalar) ? " " : String(scalar)
        }.joined()
        let oneLine = printable.split(whereSeparator: { $0.isWhitespace }).joined(separator: " ")
        return String(oneLine.prefix(240))
    }
}

extension CodexSwitchClient where Locator == DefaultBinaryLocator, Runner == FoundationCommandRunner {
    init(configuration: SnapshotLaunchConfiguration = .production) {
        self.init(
            locator: DefaultBinaryLocator(),
            runner: FoundationCommandRunner(environment: configuration.helperEnvironment),
            proxyAddress: configuration.proxyAddress
        )
    }
}
