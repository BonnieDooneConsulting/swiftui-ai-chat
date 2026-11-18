import Foundation
import Testing
@testable import ChatCore

/// Serves canned responses to URLSession, keyed by host so tests can run in
/// parallel without stepping on each other.
final class StubProtocol: URLProtocol, @unchecked Sendable {
    struct Stub: Sendable {
        var status: Int
        var headers: [String: String] = ["Content-Type": "text/event-stream"]
        var chunks: [String]
    }

    private static let lock = NSLock()
    nonisolated(unsafe) private static var stubs: [String: Stub] = [:]
    nonisolated(unsafe) private static var requests: [String: URLRequest] = [:]

    static func register(_ stub: Stub) -> URL {
        let host = "stub-\(UUID().uuidString.lowercased()).test"
        lock.withLock { stubs[host] = stub }
        return URL(string: "http://\(host)")!
    }

    static func lastRequest(for url: URL) -> URLRequest? {
        lock.withLock { requests[url.host() ?? ""] }
    }

    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    override func startLoading() {
        let host = request.url?.host() ?? ""
        let stub = Self.lock.withLock { () -> Stub? in
            Self.requests[host] = request
            return Self.stubs[host]
        }
        guard let stub, let url = request.url else {
            client?.urlProtocol(self, didFailWithError: URLError(.badURL))
            return
        }
        let response = HTTPURLResponse(url: url, statusCode: stub.status, httpVersion: "HTTP/1.1", headerFields: stub.headers)!
        client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
        for chunk in stub.chunks {
            client?.urlProtocol(self, didLoad: Data(chunk.utf8))
        }
        client?.urlProtocolDidFinishLoading(self)
    }

    override func stopLoading() {}
}

@Suite struct ChatClientTests {
    private func client(for stub: StubProtocol.Stub) -> (ChatClient, URL) {
        let url = StubProtocol.register(stub)
        let config = URLSessionConfiguration.ephemeral
        config.protocolClasses = [StubProtocol.self]
        return (ChatClient(baseURL: url, deviceID: "device-1234", session: URLSession(configuration: config)), url)
    }

    private func collect(_ client: ChatClient) async throws -> [String] {
        var chunks: [String] = []
        for try await chunk in client.streamReply(to: [ChatMessage(role: .user, content: "Hi")]) {
            chunks.append(chunk)
        }
        return chunks
    }

    @Test func streamsDeltasInOrder() async throws {
        // Split events across chunk boundaries on purpose.
        let (client, url) = client(for: .init(status: 200, chunks: [
            "data: {\"type\":\"delta\",\"text\":\"Hel\"}\n\ndata: {\"type\":\"del",
            "ta\",\"text\":\"lo\"}\n\n",
            ": ping\n\n",
            "data: {\"type\":\"delta\",\"text\":\" there\"}\r\n\r\n",
            "data: {\"type\":\"done\"}\n\n",
        ]))
        #expect(try await collect(client) == ["Hel", "lo", " there"])

        let request = try #require(StubProtocol.lastRequest(for: url))
        #expect(request.httpMethod == "POST")
        #expect(request.url?.path() == "/v1/chat")
        #expect(request.value(forHTTPHeaderField: "X-Device-ID") == "device-1234")
    }

    @Test func requestBodyMatchesServerContract() throws {
        let client = ChatClient(baseURL: URL(string: "http://localhost:8080")!, deviceID: "abc12345")
        let request = try client.makeRequest(for: [
            ChatMessage(role: .user, content: "Hi"),
            ChatMessage(role: .assistant, content: "Hello"),
            ChatMessage(role: .user, content: "How are you?"),
        ])
        let json = try JSONSerialization.jsonObject(with: try #require(request.httpBody)) as? [String: Any]
        let messages = try #require(json?["messages"] as? [[String: String]])
        #expect(messages == [
            ["role": "user", "content": "Hi"],
            ["role": "assistant", "content": "Hello"],
            ["role": "user", "content": "How are you?"],
        ])
        #expect(request.url?.absoluteString == "http://localhost:8080/v1/chat")
    }

    @Test func errorEventThrowsServerError() async {
        let (client, _) = client(for: .init(status: 200, chunks: [
            "data: {\"type\":\"delta\",\"text\":\"partial\"}\n\n",
            "data: {\"type\":\"error\",\"message\":\"The model is busy\"}\n\n",
        ]))
        await #expect(throws: ChatError.server("The model is busy")) {
            _ = try await collect(client)
        }
    }

    @Test func non200UsesMessageFromBody() async {
        let (client, _) = client(for: .init(
            status: 429,
            headers: ["Content-Type": "application/json"],
            chunks: [#"{"type":"error","message":"Slow down"}"#]
        ))
        await #expect(throws: ChatError.http(status: 429, message: "Slow down")) {
            _ = try await collect(client)
        }
    }

    @Test func streamWithoutDoneIsIncomplete() async {
        let (client, _) = client(for: .init(status: 200, chunks: ["data: {\"type\":\"delta\",\"text\":\"a\"}\n\n"]))
        await #expect(throws: ChatError.incomplete) {
            _ = try await collect(client)
        }
    }

    @Test func errorsHaveReadableDescriptions() {
        #expect(ChatError.http(status: 429, message: nil).errorDescription?.contains("Slow down") == true)
        #expect(ChatError.http(status: 500, message: nil).errorDescription?.contains("500") == true)
        #expect(ChatError.incomplete.errorDescription != nil)
    }
}

@Suite struct DeviceIDTests {
    @Test func isStableAcrossCalls() throws {
        let defaults = try #require(UserDefaults(suiteName: "DeviceIDTests-\(UUID().uuidString)"))
        let first = DeviceID.current(in: defaults)
        #expect(!first.isEmpty)
        #expect(DeviceID.current(in: defaults) == first)
    }
}
