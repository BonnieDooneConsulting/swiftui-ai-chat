import Foundation

/// One bubble in the conversation.
public struct ChatMessage: Identifiable, Equatable, Sendable, Codable {
    public enum Role: String, Codable, Sendable {
        case user
        case assistant
    }

    public let id: UUID
    public let role: Role
    public var content: String

    public init(id: UUID = UUID(), role: Role, content: String) {
        self.id = id
        self.role = role
        self.content = content
    }
}

/// The JSON body the proxy expects: `{"messages":[{"role":"user","content":"..."}]}`.
struct ChatRequestBody: Encodable {
    struct Turn: Encodable {
        let role: String
        let content: String
    }

    let messages: [Turn]

    init(_ messages: [ChatMessage]) {
        self.messages = messages.map { Turn(role: $0.role.rawValue, content: $0.content) }
    }
}
