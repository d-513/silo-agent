import Foundation

func query(_ params: [(String, String)]) -> String {
    var allowed = CharacterSet.urlQueryAllowed
    allowed.remove(charactersIn: "&=+#?/")
    return params.filter { !$0.1.isEmpty }
        .map { "\($0.0.addingPercentEncoding(withAllowedCharacters: allowed) ?? $0.0)=\($0.1.addingPercentEncoding(withAllowedCharacters: allowed) ?? $0.1)" }
        .joined(separator: "&")
}

/// The cookie-authed CP route for one version of an artifact (mirrors `downloadURL` in
/// `web/src/Artifact.tsx`): a file's bytes, or a folder zipped.
public func artifactDownloadPath(botID: String, artifactID: String, version: Int) -> String {
    "/artifacts/download?" + query([("bot_id", botID), ("artifact_id", artifactID), ("v", version > 0 ? String(version) : "")])
}

extension ArtifactInfo {
    /// Where this card's bytes come from. A card from before versions has no id the server knows:
    /// a saved skill is still zipped by name; a file is read from the workspace instead (nil).
    public func downloadPath(botID: String) -> String? {
        if !artifactID.isEmpty, !artifactID.hasPrefix("path:") {
            return artifactDownloadPath(botID: botID, artifactID: artifactID, version: version)
        }
        if artifactType == "skill", !name.isEmpty {
            return "/artifacts/skill.zip?" + query([("scope", scope.isEmpty || scope == "workspace" ? "personal" : scope), ("name", name)])
        }
        return nil
    }

    /// The name a saved copy gets: a folder is a zip.
    public var downloadName: String {
        let tree = kind == "tree" || (kind.isEmpty && artifactType != "file")
        let last = path.split(separator: "/").last.map(String.init) ?? ""
        let base = !name.isEmpty ? name : (!last.isEmpty ? last : (artifactType == "skill" ? "skill" : "file"))
        return tree ? base + ".zip" : base
    }

    public var isPending: Bool { artifactType == "skill" && status == "pending" }
    public var isSaved: Bool { artifactType == "skill" && status == "saved" }
}
