import Foundation
import SwiftProtobuf

protocol SnapshotLoading: Sendable {
    /// The proxy's current menu snapshot as protobuf bytes.
    func loadSnapshot() async throws -> Data
}

enum SnapshotClientError: Error, Equatable {
    case unreadableURLFile
    case unreachable(URLError.Code)
    case status(Int)
    case invalidSnapshot
}

enum LaunchConfigurationError: Error, Equatable {
    case unexpectedArguments
    case relativeDataDirectory
}

/// Production reads the proxy in `~/Library/Application Support/codex-switch`.
/// A staged app is launched with `--data-dir ABSOLUTE_PATH` to read an isolated proxy.
struct LaunchConfiguration: Equatable, Sendable {
    let dataDirectory: URL
    let isProduction: Bool

    init(arguments: [String]) throws {
        if arguments.isEmpty {
            dataDirectory = URL.applicationSupportDirectory.appending(path: "codex-switch", directoryHint: .isDirectory)
            isProduction = true
            return
        }
        guard arguments.count == 2, arguments[0] == "--data-dir" else {
            throw LaunchConfigurationError.unexpectedArguments
        }
        guard arguments[1].hasPrefix("/") else { throw LaunchConfigurationError.relativeDataDirectory }
        dataDirectory = URL(filePath: arguments[1], directoryHint: .isDirectory)
        isProduction = false
    }
}

/// Fetches the snapshot the proxy composes and serves at the URL it writes to
/// `menu-snapshot-url`. The file is read on every fetch because the port
/// changes when the proxy is redeployed.
struct SnapshotClient: SnapshotLoading {
    let urlFile: URL

    init(dataDirectory: URL) {
        urlFile = dataDirectory.appending(path: "menu-snapshot-url")
    }

    private static let session: URLSession = {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.timeoutIntervalForRequest = 5
        configuration.timeoutIntervalForResource = 5
        configuration.urlCache = nil
        return URLSession(configuration: configuration)
    }()

    func loadSnapshot() async throws -> Data {
        let url = try snapshotURL()
        let data: Data
        let response: URLResponse
        do {
            (data, response) = try await Self.session.data(from: url)
        } catch let error as URLError {
            if error.code == .cancelled { throw CancellationError() }
            throw SnapshotClientError.unreachable(error.code)
        }
        let status = (response as? HTTPURLResponse)?.statusCode ?? 0
        guard status == 200 else { throw SnapshotClientError.status(status) }
        return data
    }

    func snapshotURL() throws -> URL {
        guard let contents = try? String(contentsOf: urlFile, encoding: .utf8),
              let url = URL(string: contents.trimmingCharacters(in: .whitespacesAndNewlines)),
              url.scheme == "http" else {
            throw SnapshotClientError.unreadableURLFile
        }
        return url
    }
}

enum MenuSnapshotDecoding {
    /// Decodes a snapshot and rejects one whose facts the panel cannot render.
    static func decode(_ data: Data) throws -> CSSwitch_MenuSnapshot {
        guard let snapshot = try? CSSwitch_MenuSnapshot(serializedBytes: data),
              snapshot.hasGeneratedAt,
              snapshot.accounts.allSatisfy(isValid) else {
            throw SnapshotClientError.invalidSnapshot
        }
        return snapshot
    }

    private static func isValid(_ account: CSSwitch_Account) -> Bool {
        guard !account.key.isEmpty, account.accountNumber > 0 else { return false }
        guard account.hasAllowance else { return true }
        return account.allowance.hasObservedAt && account.allowance.windows.allSatisfy { window in
            window.periodSeconds > 0 && window.usedPercent.isFinite &&
                (0...100).contains(window.usedPercent) && window.hasResetAt
        }
    }
}
