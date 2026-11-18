import Foundation

/// The one setting this app has: where the proxy lives.
enum AppSettings {
    static let serverURLKey = "serverURL"
    static let defaultServerURL = "http://localhost:8080"

    /// The configured server, falling back to the default if the saved value
    /// is missing or broken.
    static var serverURL: URL {
        let saved = UserDefaults.standard.string(forKey: serverURLKey) ?? defaultServerURL
        return validated(saved) ?? URL(string: defaultServerURL)!
    }

    /// Returns a URL if `string` is a usable http(s) base URL.
    static func validated(_ string: String) -> URL? {
        let trimmed = string.trimmingCharacters(in: .whitespacesAndNewlines)
        guard let url = URL(string: trimmed),
              let scheme = url.scheme?.lowercased(),
              scheme == "http" || scheme == "https",
              url.host() != nil
        else { return nil }
        return url
    }
}
