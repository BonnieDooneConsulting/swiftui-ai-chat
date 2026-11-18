import Foundation

/// Errors the chat client can surface. Every case has a message that's fine
/// to show in the UI.
public enum ChatError: LocalizedError, Equatable, Sendable {
    /// The proxy answered with a non-200 status before streaming.
    case http(status: Int, message: String?)
    /// The proxy sent an `error` event mid-stream.
    case server(String)
    /// The connection closed before the proxy said it was done.
    case incomplete
    /// The response wasn't HTTP at all.
    case invalidResponse

    public var errorDescription: String? {
        switch self {
        case let .http(status, message):
            if let message, !message.isEmpty { return message }
            if status == 429 { return "Slow down a little. Try again in a few seconds." }
            return "The server returned an error (\(status))."
        case let .server(message):
            return message
        case .incomplete:
            return "The reply was cut off. Check your connection and try again."
        case .invalidResponse:
            return "The server sent something unexpected."
        }
    }
}

/// Talks to the proxy's `POST /v1/chat` and streams the reply.
public struct ChatClient: Sendable {
    public var baseURL: URL
    public var deviceID: String
    private let session: URLSession

    public init(baseURL: URL, deviceID: String, session: URLSession = .shared) {
        self.baseURL = baseURL
        self.deviceID = deviceID
        self.session = session
    }

    /// Builds the HTTP request for a conversation.
    public func makeRequest(for messages: [ChatMessage]) throws -> URLRequest {
        var request = URLRequest(url: baseURL.appending(path: "v1/chat"))
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.setValue("text/event-stream", forHTTPHeaderField: "Accept")
        request.setValue(deviceID, forHTTPHeaderField: "X-Device-ID")
        request.timeoutInterval = 60 // time allowed between bytes, not total
        request.httpBody = try JSONEncoder().encode(ChatRequestBody(messages))
        return request
    }

    /// Streams the assistant's reply as text chunks, in order.
    ///
    /// Cancelling the consuming task (or breaking out of the loop) cancels the
    /// underlying request, which closes the connection and lets the proxy stop
    /// paying for tokens nobody will read.
    public func streamReply(to messages: [ChatMessage]) -> AsyncThrowingStream<String, Error> {
        AsyncThrowingStream { continuation in
            let task = Task {
                do {
                    let request = try makeRequest(for: messages)
                    let (bytes, response) = try await session.bytes(for: request)
                    guard let http = response as? HTTPURLResponse else {
                        throw ChatError.invalidResponse
                    }
                    guard http.statusCode == 200 else {
                        throw ChatError.http(status: http.statusCode, message: try await Self.errorMessage(from: bytes))
                    }

                    var lines = LineSplitter()
                    var parser = SSEParser()
                    for try await byte in bytes {
                        guard let line = lines.append(byte), let payload = parser.feed(line: line) else { continue }
                        if try Self.handle(payload, continuation) { return }
                    }
                    if let line = lines.finish() { _ = parser.feed(line: line) }
                    if let payload = parser.flush(), try Self.handle(payload, continuation) { return }
                    throw ChatError.incomplete
                } catch {
                    continuation.finish(throwing: error)
                }
            }
            continuation.onTermination = { _ in task.cancel() }
        }
    }

    /// Applies one event. Returns true once the stream is finished.
    private static func handle(
        _ payload: String,
        _ continuation: AsyncThrowingStream<String, Error>.Continuation
    ) throws -> Bool {
        switch try ServerEvent(json: payload) {
        case let .delta(text):
            continuation.yield(text)
            return false
        case .done:
            continuation.finish()
            return true
        case let .error(message):
            throw ChatError.server(message)
        case .unknown:
            return false
        }
    }

    /// Reads a small JSON error body like `{"type":"error","message":"..."}`.
    private static func errorMessage(from bytes: URLSession.AsyncBytes) async throws -> String? {
        var data = Data()
        for try await byte in bytes {
            data.append(byte)
            if data.count > 16 * 1024 { break }
        }
        struct Body: Decodable { let message: String? }
        return try? JSONDecoder().decode(Body.self, from: data).message
    }
}
