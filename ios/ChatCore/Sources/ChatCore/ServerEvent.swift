import Foundation

/// One event from the proxy's SSE stream.
public enum ServerEvent: Equatable, Sendable {
    /// A chunk of reply text to append.
    case delta(String)
    /// The reply is complete.
    case done
    /// Something went wrong. The message is safe to show the user.
    case error(String)
    /// An event type this version of the app doesn't know. Ignore it.
    case unknown(String)

    private struct Wire: Decodable {
        let type: String
        let text: String?
        let message: String?
    }

    /// Decodes the JSON payload of a `data:` line.
    public init(json: String) throws {
        let wire = try JSONDecoder().decode(Wire.self, from: Data(json.utf8))
        switch wire.type {
        case "delta": self = .delta(wire.text ?? "")
        case "done": self = .done
        case "error": self = .error(wire.message ?? "Something went wrong.")
        default: self = .unknown(wire.type)
        }
    }
}
