import XCTest
@testable import SiloClient

final class SignInTextTests: XCTestCase {
    func testAWrongPasswordIsNotAnExpiredSession() {
        XCTAssertEqual(signInRefusal(code: "unauthenticated", message: "invalid credentials"),
                       "That email and password do not match.")
    }

    func testAWrongCode() {
        XCTAssertTrue(signInRefusal(code: "unauthenticated", message: "that code is not right").hasPrefix("That code is not right."))
    }

    func testOtherRefusalsReadAsSentences() {
        XCTAssertEqual(signInRefusal(code: "", message: "too many attempts; try again in 30 seconds"),
                       "Too many attempts. Try again in 30 seconds.")
        XCTAssertEqual(signInRefusal(code: "", message: "this account is disabled; ask an admin"),
                       "This account is disabled. Ask an admin.")
        XCTAssertEqual(signInRefusal(code: "", message: ""), "Could not sign in.")
    }
}
