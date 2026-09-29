import Foundation

/// What an approval slip shows. Mirrors `describeApproval` in `web/src/Approval.tsx`.
public struct ApprovalDescription: Equatable, Sendable {
    public struct Field: Equatable, Sendable {
        public var label: String
        public var value: String
    }

    public var title: String
    public var summary: String
    public var fields: [Field]
}

/// "web_search" / "secrets.get" → "Web Search" / "Secrets Get".
public func phrase(_ value: String) -> String {
    let parts = value.split(whereSeparator: { "._- ".contains($0) })
    return parts.map { $0.prefix(1).uppercased() + $0.dropFirst() }.joined(separator: " ")
}

/// The security engine usually supplies `title`/`summary`/`fields`; an unknown action falls back to
/// its connector + action names and the flat string args. Never dumps raw JSON (the CP already
/// redacts `desktop.type` text; nested values are shown compact).
public func describeApproval(_ approval: Silo_V1_Approval) -> ApprovalDescription {
    if !approval.title.isEmpty {
        return ApprovalDescription(
            title: approval.title,
            summary: approval.summary,
            fields: approval.fields.map { .init(label: $0.label, value: $0.value) }
        )
    }
    var fields: [ApprovalDescription.Field] = []
    if let data = approval.argsJson.data(using: .utf8),
       let object = try? JSONSerialization.jsonObject(with: data) as? [String: Any] {
        for key in object.keys.sorted() {
            let raw = object[key]
            if raw == nil || raw is NSNull { continue }
            let value: String
            if let string = raw as? String {
                value = string
            } else if let raw, let data = try? JSONSerialization.data(withJSONObject: raw, options: [.fragmentsAllowed]),
                      let string = String(data: data, encoding: .utf8) {
                value = string
            } else {
                continue
            }
            if value.isEmpty { continue }
            fields.append(.init(label: phrase(key), value: value))
        }
    }
    return ApprovalDescription(
        title: "\(phrase(approval.action)) · \(phrase(approval.connector))",
        summary: "This Bot wants to \(phrase(approval.action).lowercased()) (\(phrase(approval.connector))).",
        fields: fields
    )
}
