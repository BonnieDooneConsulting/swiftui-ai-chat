import Foundation
import Testing
@testable import ChatCore

/// A reply stream the test controls by hand.
final class ManualStream: @unchecked Sendable {
    let stream: AsyncThrowingStream<String, Error>
    let continuation: AsyncThrowingStream<String, Error>.Continuation
    private(set) var received: [ChatMessage] = []

    init() {
        (stream, continuation) = AsyncThrowingStream<String, Error>.makeStream()
    }

    func provider() -> ChatViewModel.StreamProvider {
        { [self] messages in
            received = messages
            return stream
        }
    }
}

/// Lets the view model's task run until `condition` holds.
@MainActor
private func waitUntil(_ condition: () -> Bool) async {
    for _ in 0..<1000 where !condition() {
        await Task.yield()
    }
}

@MainActor
@Suite struct ChatViewModelTests {
    @Test func sendStreamsReplyIntoAssistantBubble() async {
        let fake = ManualStream()
        let model = ChatViewModel(streamProvider: fake.provider())
        model.draft = "  Hello  "
        #expect(model.canSend)

        model.send()
        #expect(model.isStreaming)
        #expect(!model.canSend)
        #expect(model.draft.isEmpty)
        #expect(fake.received.map(\.content) == ["Hello"])

        fake.continuation.yield("Hi")
        await waitUntil { model.messages.last?.content == "Hi" }
        fake.continuation.yield(" there")
        fake.continuation.finish()
        await model.waitForStream()

        #expect(!model.isStreaming)
        #expect(model.messages.map(\.role) == [.user, .assistant])
        #expect(model.messages.last?.content == "Hi there")
        #expect(model.errorMessage == nil)
    }

    @Test func sendIsIgnoredWhileStreamingOrEmpty() {
        let fake = ManualStream()
        let model = ChatViewModel(streamProvider: fake.provider())
        model.draft = "   "
        model.send()
        #expect(model.messages.isEmpty)

        model.draft = "one"
        model.send()
        model.draft = "two"
        model.send()
        #expect(model.messages.filter { $0.role == .user }.count == 1)
    }

    @Test func stopKeepsPartialText() async {
        let fake = ManualStream()
        let model = ChatViewModel(streamProvider: fake.provider())
        model.draft = "Tell me a story"
        model.send()
        fake.continuation.yield("Once upon")
        await waitUntil { model.messages.last?.content == "Once upon" }

        model.stop()
        #expect(!model.isStreaming)
        // Late chunks from the stopped stream are ignored.
        fake.continuation.yield(" a time")
        fake.continuation.finish()
        await Task.yield()
        #expect(model.messages.last?.content == "Once upon")
        #expect(model.errorMessage == nil)
    }

    @Test func stopBeforeAnyTextRemovesEmptyBubble() {
        let fake = ManualStream()
        let model = ChatViewModel(streamProvider: fake.provider())
        model.draft = "Hi"
        model.send()
        model.stop()
        #expect(model.messages.map(\.role) == [.user])
    }

    @Test func errorShowsBannerAndRetryResends() async {
        let first = ManualStream()
        let second = ManualStream()
        let calls = Counter()
        let model = ChatViewModel { messages in
            calls.increment() == 1 ? first.provider()(messages) : second.provider()(messages)
        }
        model.draft = "Hi"
        model.send()
        first.continuation.yield("par")
        first.continuation.finish(throwing: ChatError.server("The model is busy"))
        await model.waitForStream()

        #expect(model.errorMessage == "The model is busy")
        #expect(!model.isStreaming)

        model.retry()
        #expect(model.errorMessage == nil)
        #expect(second.received.map(\.role) == [.user]) // partial reply dropped
        second.continuation.yield("Hello!")
        second.continuation.finish()
        await model.waitForStream()
        #expect(model.messages.map(\.content) == ["Hi", "Hello!"])
    }
}

final class Counter: @unchecked Sendable {
    private let lock = NSLock()
    private var value = 0
    func increment() -> Int { lock.withLock { value += 1; return value } }
}
