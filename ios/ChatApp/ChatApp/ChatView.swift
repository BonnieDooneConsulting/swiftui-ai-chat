import ChatCore
import SwiftUI

struct ChatView: View {
    @Bindable var model: ChatViewModel
    @State private var showingSettings = false
    @FocusState private var inputFocused: Bool

    var body: some View {
        NavigationStack {
            VStack(spacing: 0) {
                if let error = model.errorMessage {
                    ErrorBanner(message: error, retry: model.retry, dismiss: model.dismissError)
                        .transition(.move(edge: .top).combined(with: .opacity))
                }
                messageList
                Divider()
                inputBar
            }
            .animation(.default, value: model.errorMessage)
            .navigationTitle("AI Chat")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .topBarLeading) {
                    Button {
                        showingSettings = true
                    } label: {
                        Image(systemName: "gearshape")
                    }
                    .accessibilityLabel("Settings")
                }
                ToolbarItem(placement: .topBarTrailing) {
                    Button {
                        model.reset()
                    } label: {
                        Image(systemName: "square.and.pencil")
                    }
                    .accessibilityLabel("New chat")
                    .disabled(model.messages.isEmpty)
                }
            }
            .sheet(isPresented: $showingSettings) {
                SettingsView()
            }
        }
        .onChange(of: model.errorMessage) { _, message in
            if let message {
                AccessibilityNotification.Announcement(message).post()
            }
        }
        #if DEBUG
        .task { autoSendFromLaunchArguments() }
        #endif
    }

    private var messageList: some View {
        ScrollViewReader { proxy in
            ScrollView {
                LazyVStack(spacing: 12) {
                    if model.messages.isEmpty {
                        EmptyStateView()
                            .padding(.top, 80)
                    }
                    ForEach(model.messages) { message in
                        MessageBubble(
                            message: message,
                            isStreaming: model.isStreaming && message.id == model.messages.last?.id
                        )
                        .id(message.id)
                    }
                }
                .padding()
            }
            .defaultScrollAnchor(.bottom)
            .scrollDismissesKeyboard(.interactively)
            .onChange(of: model.messages.last?.content) {
                if let last = model.messages.last {
                    proxy.scrollTo(last.id, anchor: .bottom)
                }
            }
        }
    }

    private var inputBar: some View {
        HStack(alignment: .bottom, spacing: 8) {
            TextField("Message", text: $model.draft, axis: .vertical)
                .lineLimit(1...6)
                .padding(.horizontal, 14)
                .padding(.vertical, 10)
                .background(Color(.secondarySystemBackground), in: RoundedRectangle(cornerRadius: 20))
                .focused($inputFocused)
                .accessibilityLabel("Message")
                .accessibilityHint("Type a message to send")

            if model.isStreaming {
                Button(action: model.stop) {
                    Image(systemName: "stop.circle.fill")
                        .font(.title)
                }
                .accessibilityLabel("Stop generating")
                .transition(.scale.combined(with: .opacity))
            }

            Button(action: model.send) {
                Image(systemName: "arrow.up.circle.fill")
                    .font(.title)
            }
            .disabled(!model.canSend)
            .accessibilityLabel("Send message")
            .accessibilityHint(model.isStreaming ? "Available when the reply finishes" : "")
        }
        .animation(.default, value: model.isStreaming)
        .padding(.horizontal)
        .padding(.vertical, 8)
        .background(.bar)
    }

    #if DEBUG
    /// Lets screenshots and manual testing start a conversation from the
    /// command line: `xcrun simctl launch <device> <bundle id> -autoSend "Hi"`.
    private func autoSendFromLaunchArguments() {
        let args = ProcessInfo.processInfo.arguments
        guard model.messages.isEmpty,
              let index = args.firstIndex(of: "-autoSend"),
              args.indices.contains(index + 1)
        else { return }
        model.draft = args[index + 1]
        model.send()
    }
    #endif
}

private struct EmptyStateView: View {
    var body: some View {
        VStack(spacing: 12) {
            Image(systemName: "bubble.left.and.text.bubble.right")
                .font(.largeTitle)
                .foregroundStyle(.secondary)
                .accessibilityHidden(true)
            Text("Ask me anything")
                .font(.headline)
            Text("Replies stream in from your server as they're written.")
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
        }
        .padding()
        .accessibilityElement(children: .combine)
    }
}

private struct ErrorBanner: View {
    let message: String
    let retry: () -> Void
    let dismiss: () -> Void

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 12) {
            Image(systemName: "exclamationmark.triangle.fill")
                .foregroundStyle(.orange)
                .accessibilityHidden(true)
            Text(message)
                .font(.subheadline)
                .frame(maxWidth: .infinity, alignment: .leading)
                .accessibilityLabel("Error: \(message)")
            Button("Retry", action: retry)
                .font(.subheadline.weight(.semibold))
                .accessibilityHint("Sends your last message again")
            Button(action: dismiss) {
                Image(systemName: "xmark")
                    .font(.subheadline)
            }
            .accessibilityLabel("Dismiss error")
        }
        .padding(12)
        .background(Color.orange.opacity(0.15))
    }
}

#Preview("Conversation") {
    ChatView(model: ChatViewModel(
        messages: [
            ChatMessage(role: .user, content: "Why shouldn't I put my API key in the app?"),
            ChatMessage(role: .assistant, content: "Because anyone can pull it out of the app bundle and run up your bill."),
        ],
        streamProvider: { _ in AsyncThrowingStream { $0.finish() } }
    ))
}
