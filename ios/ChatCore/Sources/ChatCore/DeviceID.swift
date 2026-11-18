import Foundation

/// A random ID created on first launch and kept in UserDefaults. It survives
/// app updates and is reset when the app is deleted, which is what we want for
/// per-install rate limiting. It isn't tied to the person or the hardware.
public enum DeviceID {
    static let key = "chat.deviceID"

    public static func current(in defaults: UserDefaults = .standard) -> String {
        if let existing = defaults.string(forKey: key), !existing.isEmpty {
            return existing
        }
        let new = UUID().uuidString
        defaults.set(new, forKey: key)
        return new
    }
}
