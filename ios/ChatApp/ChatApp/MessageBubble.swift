import ChatCore
import SwiftUI

struct MessageBubble: View {
    let message: ChatMessage
    let isStreaming: Bool

    private var isUser: Bool { message.role == .user }

    var body: some View {
        HStack {
            if isUser { Spacer(minLength: 48) }
            content
                .padding(.horizontal, 14)
                .padding(.vertical, 10)
                .foregroundStyle(isUser ? Color.white : Color.primary)
                .background(
                    isUser ? Color.accentColor : Color(.secondarySystemBackground),
                    in: RoundedRectangle(cornerRadius: 18, style: .continuous)
                )
            if !isUser { Spacer(minLength: 48) }
        }
        .frame(maxWidth: .infinity, alignment: isUser ? .trailing : .leading)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(accessibilityText)
        .accessibilityValue(isStreaming ? "Still typing" : "")
    }

    @ViewBuilder
    private var content: some View {
        if message.content.isEmpty && isStreaming {
            TypingIndicator()
        } else {
            Text(message.content)
                .font(.body)
                .textSelection(.enabled)
                .fixedSize(horizontal: false, vertical: true)
        }
    }

    private var accessibilityText: String {
        let speaker = isUser ? "You" : "Assistant"
        let text = message.content.isEmpty ? "Thinking" : message.content
        return "\(speaker): \(text)"
    }
}

private struct TypingIndicator: View {
    var body: some View {
        HStack(spacing: 4) {
            ForEach(0..<3, id: \.self) { _ in
                Circle()
                    .frame(width: 7, height: 7)
                    .foregroundStyle(.secondary)
            }
        }
        .phaseAnimator([0.35, 1.0]) { dots, opacity in
            dots.opacity(opacity)
        } animation: { _ in
            .easeInOut(duration: 0.6)
        }
        .padding(.vertical, 6)
    }
}
