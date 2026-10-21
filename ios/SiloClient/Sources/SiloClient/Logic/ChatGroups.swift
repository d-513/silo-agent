import Foundation

/// The sidebar's date groups, as on the web (`bot/chatGroups.ts`): Today, Yesterday, Previous 7
/// days, Previous 30 days, then one group per month, newest first.
public struct ChatGroup<Item>: Identifiable {
    public let title: String
    public var items: [Item]
    public var id: String { title }
}

public func chatGroups<Item>(_ items: [Item], updatedAt: (Item) -> Date?, now: Date = Date(),
                             calendar: Calendar = .current) -> [ChatGroup<Item>] {
    let today = calendar.startOfDay(for: now)
    func days(_ date: Date) -> Int {
        calendar.dateComponents([.day], from: calendar.startOfDay(for: date), to: today).day ?? 0
    }
    let month = DateFormatter()
    month.calendar = calendar
    month.timeZone = calendar.timeZone
    month.setLocalizedDateFormatFromTemplate("MMMMyyyy")

    var groups: [ChatGroup<Item>] = []
    for item in items {
        let title: String
        if let date = updatedAt(item) {
            switch days(date) {
            case ...0: title = "Today"
            case 1: title = "Yesterday"
            case 2...7: title = "Previous 7 days"
            case 8...30: title = "Previous 30 days"
            default: title = month.string(from: date)
            }
        } else {
            title = "Older"
        }
        if let index = groups.firstIndex(where: { $0.title == title }) {
            groups[index].items.append(item)
        } else {
            groups.append(ChatGroup(title: title, items: [item]))
        }
    }
    return groups
}
