import Foundation
import SiloClient

/// A message with files, from outside the app: the share sheet and "Send files to a Bot" in
/// Shortcuts. Files go to the box's `tmp/` as ordinary uploads, then the message goes to the main
/// chat carrying them, the way the composer's paperclip does.
enum BotDelivery {
    struct File {
        var name: String
        var data: Data
    }

    @discardableResult
    static func send(botID: String, text: String, files: [File], server: String) async throws -> Silo_V1_SendResponse {
        let client = SiloClient(host: server)
        var attachments: [Silo_V1_Attachment] = []
        let stamp = Int(Date().timeIntervalSince1970)
        for (index, file) in files.enumerated() {
            let safe = cleanName(file.name, fallback: "file-\(stamp)-\(index)")
            let path = "tmp/\(safe)"
            try await client.putFile(botID: botID, path: path, data: file.data)
            var item = Silo_V1_Attachment()
            item.name = safe
            item.path = path
            item.size = Int64(file.data.count)
            attachments.append(item)
        }
        return try await client.send(botID: botID, chatID: "", text: text, attachments: attachments)
    }

    /// A file's name as a path segment: no folders, never empty.
    static func cleanName(_ name: String, fallback: String) -> String {
        let flat = name.replacingOccurrences(of: "/", with: "_").replacingOccurrences(of: "\\", with: "_")
            .trimmingCharacters(in: .whitespacesAndNewlines)
        return flat.isEmpty || flat == "." || flat == ".." ? fallback : flat
    }
}
