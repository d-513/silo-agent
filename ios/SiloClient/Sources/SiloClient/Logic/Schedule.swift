import Foundation

/// Friendly schedules over 5-field cron (minute hour day-of-month month day-of-week). The CP
/// stores only cron; these shapes are what the Automations editor offers and anything else stays
/// "custom". Port of `web/src/schedule.ts`.
public struct Schedule: Equatable, Sendable {
    public enum Mode: String, Sendable { case none, minutes, hours, daily, weekly, monthly, custom }

    public var mode: Mode = .none
    /// Minutes or hours between runs.
    public var every = 30
    public var minute = 0
    public var hour = 9
    /// 0 = Sunday … 6 = Saturday.
    public var days = [1, 2, 3, 4, 5]
    public var dom = 1
    public var cron = ""

    public init() {}

    public static let dayShort = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"]
    public static let minuteSteps = [5, 10, 15, 20, 30]
    public static let hourSteps = [1, 2, 3, 4, 6, 8, 12]

    private static func number(_ text: String, _ low: Int, _ high: Int) -> Int? {
        guard !text.isEmpty, text.allSatisfy(\.isASCII), text.allSatisfy(\.isNumber), let n = Int(text) else { return nil }
        return n >= low && n <= high ? n : nil
    }

    private static func step(_ field: String) -> Int? {
        guard field.hasPrefix("*/") else { return nil }
        return Int(field.dropFirst(2))
    }

    private static func parseDays(_ text: String) -> [Int]? {
        var out = Set<Int>()
        for part in text.split(separator: ",", omittingEmptySubsequences: false) {
            let range = part.split(separator: "-", omittingEmptySubsequences: false).map(String.init)
            if range.count == 2 {
                guard let a = number(range[0], 0, 7), let b = number(range[1], 0, 7), a <= b else { return nil }
                for d in a...b { out.insert(d % 7) }
            } else {
                guard let d = number(String(part), 0, 7) else { return nil }
                out.insert(d % 7)
            }
        }
        return out.sorted()
    }

    /// Reads cron into the friendliest shape that round-trips exactly.
    public init(cron raw: String) {
        self.init()
        let c = raw.split(whereSeparator: \.isWhitespace).joined(separator: " ")
        if c.isEmpty { return }
        var custom = Schedule()
        custom.mode = .custom
        custom.cron = c
        let f = c.split(separator: " ").map(String.init)
        guard f.count == 5, f[3] == "*" else { self = custom; return }
        let (mi, ho, dom, dow) = (f[0], f[1], f[2], f[4])
        if let n = Self.step(mi), ho == "*", dom == "*", dow == "*", Self.minuteSteps.contains(n) {
            mode = .minutes
            every = n
            return
        }
        guard let minute = Self.number(mi, 0, 59) else { self = custom; return }
        if dom == "*", dow == "*" {
            if ho == "*" { mode = .hours; every = 1; self.minute = minute; return }
            if let n = Self.step(ho), Self.hourSteps.contains(n) { mode = .hours; every = n; self.minute = minute; return }
        }
        guard let hour = Self.number(ho, 0, 23) else { self = custom; return }
        self.minute = minute
        self.hour = hour
        if dom == "*", dow == "*" { mode = .daily; return }
        if dom == "*" {
            if let d = Self.parseDays(dow), !d.isEmpty {
                if d.count == 7 { mode = .daily } else { mode = .weekly; days = d }
                return
            }
            self = custom
            return
        }
        if let d = Self.number(dom, 1, 31), dow == "*" { mode = .monthly; self.dom = d; return }
        self = custom
    }

    private var dowField: String {
        let ds = Array(Set(days)).sorted()
        if ds == [1, 2, 3, 4, 5] { return "1-5" }
        if ds == [0, 6] { return "0,6" }
        return ds.map(String.init).joined(separator: ",")
    }

    /// Writes the shape back as cron; `.none` is the empty schedule.
    public var toCron: String {
        switch mode {
        case .none: return ""
        case .minutes: return "*/\(every) * * * *"
        case .hours: return every == 1 ? "\(minute) * * * *" : "\(minute) */\(every) * * *"
        case .daily: return "\(minute) \(hour) * * *"
        case .weekly: return days.isEmpty ? "\(minute) \(hour) * * *" : "\(minute) \(hour) * * \(dowField)"
        case .monthly: return "\(minute) \(hour) \(dom) * *"
        case .custom: return cron.split(whereSeparator: \.isWhitespace).joined(separator: " ")
        }
    }

    private func pad(_ n: Int) -> String { String(format: "%02d", n) }
    private var clock: String { "\(pad(hour)):\(pad(minute))" }

    private func ordinal(_ n: Int) -> String {
        let suffix: String
        if (11...13).contains(n % 100) { suffix = "th" } else {
            suffix = ["th", "st", "nd", "rd"][n % 10 < 4 ? n % 10 : 0]
        }
        return "\(n)\(suffix)"
    }

    /// The one-line human reading of the schedule.
    public var summary: String {
        switch mode {
        case .none: return "No schedule"
        case .minutes: return "Every \(every) minutes"
        case .hours: return every == 1 ? "Every hour at :\(pad(minute))" : "Every \(every) hours at :\(pad(minute))"
        case .daily: return "Every day at \(clock)"
        case .weekly:
            let f = dowField
            let when = f == "1-5" ? "Weekdays" : f == "0,6" ? "Weekends" : days.map { Self.dayShort[$0] }.joined(separator: ", ")
            return "\(when) at \(clock)"
        case .monthly: return "Monthly on the \(ordinal(dom)) at \(clock)"
        case .custom: return "Custom: \(cron)"
        }
    }
}

public func describeSchedule(_ cron: String) -> String { Schedule(cron: cron).summary }
