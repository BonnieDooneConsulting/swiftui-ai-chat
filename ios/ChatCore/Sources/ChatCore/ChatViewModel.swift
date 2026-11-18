import Foundation
import Observation

/// Drives the chat screen: holds the conversation, starts and stops streams,
/// and keeps track of errors so the UI can offer a retry.
@MainActor
@Observable
public final class ChatViewModel {
    /// Produces a reply stream for a conversation. The app passes a closure
    /// that calls `ChatClient`; tests pass a fake.
    public typealias StreamProvider = @Sendable ([ChatMessage]) -> AsyncThrowingStream<String, Error>

    public private(set) var messages: [ChatMessage] = []
    public var draft: String = ""
    public private(set) var isStreaming = false
    public private(set) var errorMessage: String?

    @ObservationIgnored private let streamProvider: StreamProvider
    @ObservationIgnored private var streamTask: Task<Void, Never>?
    /// Identifies the stream that's allowed to update state. A stream that was
    /// stopped can still deliver a late chunk or error; this lets us ignore it.
    @ObservationIgnored private var activeStream: UUID?

    public init(messages: [ChatMessage] = [], streamProvider: @escaping StreamProvider) {
        self.messages = messages
        self.streamProvider = streamProvider
    }

    public var canSend: Bool {
        !isStreaming && !draft.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }

    /// Sends the current draft as a new user message and streams the reply.
    public func send() {
        guard canSend else { return }
        let text = draft.trimmingCharacters(in: .whitespacesAndNewlines)
        draft = ""
        messages.append(ChatMessage(role: .user, content: text))
        startStreaming()
    }

    /// Stops the reply in progress. Whatever text has arrived stays on screen.
    public func stop() {
        guard isStreaming else { return }
        activeStream = nil
        streamTask?.cancel()
        streamTask = nil
        finishStreaming()
    }

    /// Tries the last request again after an error.
    public func retry() {
        guard !isStreaming, messages.last != nil else { return }
        if messages.last?.role == .assistant {
            messages.removeLast() // drop the partial reply and ask again
        }
        guard messages.last?.role == .user else { return }
        startStreaming()
    }

    public func dismissError() {
        errorMessage = nil
    }

    /// Clears the conversation.
    public func reset() {
        stop()
        messages.removeAll()
        errorMessage = nil
    }

    /// Waits for the current stream to finish. Handy in tests.
    public func waitForStream() async {
        await streamTask?.value
    }

    private func startStreaming() {
        errorMessage = nil
        let history = messages
        let reply = ChatMessage(role: .assistant, content: "")
        messages.append(reply)
        isStreaming = true

        let token = UUID()
        activeStream = token
        let stream = streamProvider(history)
        streamTask = Task { [weak self] in
            do {
                for try await chunk in stream {
                    guard let self, self.activeStream == token else { return }
                    self.append(chunk, to: reply.id)
                }
                self?.complete(token, error: nil)
            } catch {
                self?.complete(token, error: error)
            }
        }
    }

    private func complete(_ token: UUID, error: Error?) {
        guard activeStream == token else { return } // stopped or replaced
        activeStream = nil
        streamTask = nil
        if let error, !(error is CancellationError) {
            errorMessage = error.localizedDescription
        }
        finishStreaming()
    }

    private func append(_ chunk: String, to id: UUID) {
        guard let index = messages.lastIndex(where: { $0.id == id }) else { return }
        messages[index].content += chunk
    }

    private func finishStreaming() {
        isStreaming = false
        // An empty bubble is just noise, and the server rejects empty turns.
        if let last = messages.last, last.role == .assistant, last.content.isEmpty {
            messages.removeLast()
        }
    }
}
