import Foundation

/// The last turn's token usage (`usage` run event) and the model's context window.
public struct Usage: Equatable, Sendable {
    public var input = 0
    public var output = 0
    public var cacheRead = 0
    public var cacheWrite = 0
    public var window = 0

    public var used: Int { input + output }
    /// 0...1, how full the window was.
    public var fraction: Double { window > 0 ? min(1, Double(used) / Double(window)) : 0 }

    public init(input: Int = 0, output: Int = 0, cacheRead: Int = 0, cacheWrite: Int = 0, window: Int = 0) {
        self.input = input
        self.output = output
        self.cacheRead = cacheRead
        self.cacheWrite = cacheWrite
        self.window = window
    }

    /// nil for a malformed or empty body.
    public init?(eventBody: String) {
        guard let data = eventBody.data(using: .utf8),
              let object = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else { return nil }
        func number(_ key: String) -> Int { (object[key] as? NSNumber)?.intValue ?? 0 }
        self.init(input: number("input"), output: number("output"), cacheRead: number("cache_read"),
                  cacheWrite: number("cache_write"), window: number("window"))
    }
}

/// "850", "1.2k", "48k".
public func formatTokens(_ count: Int) -> String {
    guard count >= 1000 else { return String(count) }
    let thousands = Double(count) / 1000
    return count >= 10000 ? String(format: "%.0fk", thousands) : String(format: "%.1fk", thousands)
}
