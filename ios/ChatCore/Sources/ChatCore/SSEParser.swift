import Foundation

/// Splits a byte stream into lines. Handles `\n` and `\r\n` endings and keeps
/// empty lines, which matter in SSE because a blank line ends an event.
/// (`URLSession.AsyncBytes.lines` drops empty lines, so we don't use it.)
public struct LineSplitter: Sendable {
    private var buffer: [UInt8] = []

    public init() {}

    /// Feeds one byte. Returns a complete line when `byte` ends one.
    public mutating func append(_ byte: UInt8) -> String? {
        guard byte == UInt8(ascii: "\n") else {
            buffer.append(byte)
            return nil
        }
        if buffer.last == UInt8(ascii: "\r") {
            buffer.removeLast()
        }
        let line = String(decoding: buffer, as: UTF8.self)
        buffer.removeAll(keepingCapacity: true)
        return line
    }

    /// Returns whatever is left once the stream ends without a final newline.
    public mutating func finish() -> String? {
        guard !buffer.isEmpty else { return nil }
        let line = String(decoding: buffer, as: UTF8.self)
        buffer.removeAll()
        return line
    }
}

/// A minimal Server-Sent Events parser. Feed it lines; it hands back the
/// joined `data:` payload each time a blank line ends an event.
///
/// It follows the parts of the SSE spec this app needs: `data:` lines are
/// joined with newlines, comment lines (starting with `:`) are skipped, and
/// other fields (`event:`, `id:`, `retry:`) are ignored.
public struct SSEParser: Sendable {
    private var dataLines: [String] = []

    public init() {}

    public mutating func feed(line: String) -> String? {
        if line.isEmpty {
            return flush()
        }
        if line.hasPrefix(":") {
            return nil
        }
        let field: Substring
        var value: Substring
        if let colon = line.firstIndex(of: ":") {
            field = line[..<colon]
            value = line[line.index(after: colon)...]
            if value.hasPrefix(" ") {
                value = value.dropFirst()
            }
        } else {
            field = Substring(line)
            value = ""
        }
        if field == "data" {
            dataLines.append(String(value))
        }
        return nil
    }

    /// Ends the current event, for example when the stream closes.
    public mutating func flush() -> String? {
        guard !dataLines.isEmpty else { return nil }
        defer { dataLines.removeAll() }
        return dataLines.joined(separator: "\n")
    }
}
