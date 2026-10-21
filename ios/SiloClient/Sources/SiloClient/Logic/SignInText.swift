import Foundation

/// The sentence a refused sign-in shows. `code` is "unauthenticated" for a wrong email,
/// password or two-factor code (the server says which), anything else for the rest
/// ("too many attempts; try again in 30 seconds", a disabled account), whose message is
/// already meant for people and only needs to read as a sentence.
public func signInRefusal(code: String, message: String) -> String {
    let text = message.trimmingCharacters(in: .whitespacesAndNewlines)
    if code == "unauthenticated" {
        if text.lowercased().contains("code") {
            return "That code is not right. Use the one your authenticator shows now, or a recovery code."
        }
        return "That email and password do not match."
    }
    guard !text.isEmpty else { return "Could not sign in." }
    let sentences = text.components(separatedBy: "; ").map { part in
        part.prefix(1).uppercased() + part.dropFirst()
    }
    var sentence = sentences.joined(separator: ". ")
    if !sentence.hasSuffix(".") { sentence += "." }
    return sentence
}
