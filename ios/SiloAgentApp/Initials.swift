import SiloClient
import SwiftUI

/// A person on a Bot or in a Workspace: their letters in a well circle, as on the web.
struct Initials: View {
    let email: String
    var size: CGFloat = 28

    var body: some View {
        Text(personInitials(email))
            .font(.silo(size >= 28 ? 11 : 9.5, .semibold))
            .tracking(0.2)
            .foregroundStyle(Theme.ink2)
            .frame(width: size, height: size)
            .background(Theme.well, in: Circle())
            .overlay(Circle().stroke(Theme.line))
            .accessibilityHidden(true)
    }
}
