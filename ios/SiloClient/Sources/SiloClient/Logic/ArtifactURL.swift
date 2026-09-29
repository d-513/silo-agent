import Foundation

private func query(_ params: [(String, String)]) -> String {
    var allowed = CharacterSet.urlQueryAllowed
    allowed.remove(charactersIn: "&=+#?/")
    return params.filter { !$0.1.isEmpty }
        .map { "\($0.0.addingPercentEncoding(withAllowedCharacters: allowed) ?? $0.0)=\($0.1.addingPercentEncoding(withAllowedCharacters: allowed) ?? $0.1)" }
        .joined(separator: "&")
}

extension ArtifactInfo {
    /// The cookie-authed CP route that streams this artifact's bytes (mirrors `artifactURL` in
    /// `web/src/Artifact.tsx`). A pending or workspace skill is zipped from the Bot's workspace.
    public func downloadPath(botID: String) -> String {
        if artifactType == "skill" {
            if status == "pending" || scope == "workspace" {
                return "/artifacts/skill.zip?" + query([("bot_id", botID), ("path", path)])
            }
            return "/artifacts/skill.zip?" + query([("scope", scope.isEmpty ? "personal" : scope), ("name", name)])
        }
        return "/artifacts/file?" + query([("bot_id", botID), ("path", path)])
    }

    /// The name a saved copy gets.
    public var downloadName: String {
        if artifactType == "skill" { return (name.isEmpty ? "skill" : name) + ".zip" }
        let last = path.split(separator: "/").last.map(String.init) ?? ""
        if !last.isEmpty { return last }
        return name.isEmpty ? "file" : name
    }

    public var isPending: Bool { artifactType == "skill" && status == "pending" }
    public var isSaved: Bool { artifactType == "skill" && status == "saved" }
}
