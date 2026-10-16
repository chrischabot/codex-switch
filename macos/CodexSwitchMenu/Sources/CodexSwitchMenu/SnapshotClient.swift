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
    case invalidInstallationConfiguration
}

/// Bundled installation settings survive Sparkle relaunches. `--data-dir`
/// selects an existing proxy; only the production installation opens at login.
struct LaunchConfiguration: Equatable, Sendable {
    let dataDirectory: URL
    let isProduction: Bool
    let usesManagedDataDirectory: Bool
    let installationEnvironment: [String: String]

    init(arguments: [String]) throws {
        let productionDataDirectory = URL.applicationSupportDirectory.appending(path: "codex-switch", directoryHint: .isDirectory)
        let root = Bundle.main.object(forInfoDictionaryKey: "CSInstallationRoot") as? String
        let label = Bundle.main.object(forInfoDictionaryKey: "CSLaunchdLabel") as? String
        let listen = Bundle.main.object(forInfoDictionaryKey: "CSListenAddress") as? String
        let managedDataDirectory: URL
        if let root, let label, let listen {
            guard root.hasPrefix("/"), !label.isEmpty, !listen.isEmpty else {
                throw LaunchConfigurationError.invalidInstallationConfiguration
            }
            managedDataDirectory = URL(filePath: root, directoryHint: .isDirectory)
                .appending(path: "Library/Application Support/codex-switch", directoryHint: .isDirectory)
            installationEnvironment = [
                "HOME": root,
                "CODEX_SWITCH_LAUNCHD_LABEL": label,
                "CODEX_SWITCH_LISTEN": listen,
            ]
        } else {
            guard root == nil, label == nil, listen == nil else {
                throw LaunchConfigurationError.invalidInstallationConfiguration
            }
            managedDataDirectory = productionDataDirectory
            installationEnvironment = [:]
        }
        if arguments.isEmpty {
            dataDirectory = managedDataDirectory
        } else {
            guard arguments.count == 2, arguments[0] == "--data-dir" else {
                throw LaunchConfigurationError.unexpectedArguments
            }
            guard arguments[1].hasPrefix("/") else { throw LaunchConfigurationError.relativeDataDirectory }
            dataDirectory = URL(filePath: arguments[1], directoryHint: .isDirectory)
        }
        let readsManagedDataDirectory = dataDirectory.standardizedFileURL == managedDataDirectory.standardizedFileURL
        usesManagedDataDirectory = readsManagedDataDirectory
        isProduction = root == nil && readsManagedDataDirectory
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
