import ChatCore
import SwiftUI

struct SettingsView: View {
    @Environment(\.dismiss) private var dismiss
    @AppStorage(AppSettings.serverURLKey) private var savedURL = AppSettings.defaultServerURL
    @State private var draftURL = ""

    private var isValid: Bool { AppSettings.validated(draftURL) != nil }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    TextField("Server URL", text: $draftURL)
                        .keyboardType(.URL)
                        .textContentType(.URL)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .accessibilityLabel("Server URL")
                    Button("Use default (\(AppSettings.defaultServerURL))") {
                        draftURL = AppSettings.defaultServerURL
                    }
                } header: {
                    Text("Server")
                } footer: {
                    Text(isValid
                         ? "The app sends your messages here. Your API key lives on that server, never in the app."
                         : "Enter a full http:// or https:// URL.")
                }

                Section {
                    LabeledContent("Device ID") {
                        Text(DeviceID.current())
                            .font(.footnote.monospaced())
                            .textSelection(.enabled)
                    }
                } header: {
                    Text("This install")
                } footer: {
                    Text("A random ID the server uses for rate limiting. Deleting the app resets it.")
                }
            }
            .navigationTitle("Settings")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Save") {
                        if let url = AppSettings.validated(draftURL) {
                            savedURL = url.absoluteString
                            dismiss()
                        }
                    }
                    .disabled(!isValid)
                }
            }
            .onAppear { draftURL = savedURL }
        }
    }
}
