import Testing
@testable import ChatCore

@Suite struct LineSplitterTests {
    @Test func splitsOnNewlinesAndKeepsBlankLines() {
        var splitter = LineSplitter()
        var lines: [String] = []
        for byte in Array("a\r\nb\n\nc".utf8) {
            if let line = splitter.append(byte) { lines.append(line) }
        }
        #expect(lines == ["a", "b", ""])
        #expect(splitter.finish() == "c")
        #expect(splitter.finish() == nil)
    }

    @Test func keepsMultibyteCharactersIntact() {
        var splitter = LineSplitter()
        var out: String?
        for byte in Array("héllo 👋\n".utf8) {
            if let line = splitter.append(byte) { out = line }
        }
        #expect(out == "héllo 👋")
    }
}

@Suite struct SSEParserTests {
    private func parse(_ lines: [String]) -> [String] {
        var parser = SSEParser()
        var events: [String] = []
        for line in lines {
            if let payload = parser.feed(line: line) { events.append(payload) }
        }
        if let rest = parser.flush() { events.append(rest) }
        return events
    }

    @Test func emitsDataWhenBlankLineEndsEvent() {
        let events = parse([#"data: {"type":"delta","text":"Hi"}"#, "", #"data: {"type":"done"}"#, ""])
        #expect(events == [#"{"type":"delta","text":"Hi"}"#, #"{"type":"done"}"#])
    }

    @Test func joinsMultipleDataLines() {
        #expect(parse(["data: one", "data: two", ""]) == ["one\ntwo"])
    }

    @Test func ignoresCommentsAndOtherFields() {
        #expect(parse([": keep-alive", "event: message", "id: 7", "retry: 1000", "data: x", ""]) == ["x"])
    }

    @Test func handlesDataWithoutSpaceAndEmptyEvents() {
        #expect(parse(["data:x", "", "", ""]) == ["x"])
    }

    @Test func flushesTrailingEventWithoutBlankLine() {
        #expect(parse(["data: tail"]) == ["tail"])
    }
}

@Suite struct ServerEventTests {
    @Test func decodesKnownTypes() throws {
        #expect(try ServerEvent(json: #"{"type":"delta","text":"Hello"}"#) == .delta("Hello"))
        #expect(try ServerEvent(json: #"{"type":"done"}"#) == .done)
        #expect(try ServerEvent(json: #"{"type":"error","message":"nope"}"#) == .error("nope"))
        #expect(try ServerEvent(json: #"{"type":"usage"}"#) == .unknown("usage"))
    }

    @Test func rejectsGarbage() {
        #expect(throws: (any Error).self) { try ServerEvent(json: "not json") }
    }
}
