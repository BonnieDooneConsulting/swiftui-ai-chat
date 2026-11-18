import ChatCore
import SwiftUI

@main
struct ChatApp: App {
    @State private var model = ChatApp.makeModel()

    var body: some Scene {
        WindowGroup {
            ChatView(model: model)
        }
    }

    /// Wires the view model to the real network client. The server URL is
    /// read on every send, so changing it in Settings takes effect right away.
    @MainActor
    static func makeModel() -> ChatViewModel {
        let deviceID = DeviceID.current()
        return ChatViewModel { messages in
            ChatClient(baseURL: AppSettings.serverURL, deviceID: deviceID)
                .streamReply(to: messages)
        }
    }
}
