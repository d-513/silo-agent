import XCTest
@testable import SiloClient

final class ApprovalTextTests: XCTestCase {
    func testPhrase() {
        XCTAssertEqual(phrase("web_search"), "Web Search")
        XCTAssertEqual(phrase("secrets.get"), "Secrets Get")
        XCTAssertEqual(phrase(""), "")
    }

    func testUsesEngineTextWhenPresent() {
        var approval = Silo_V1_Approval()
        approval.title = "Send email"
        approval.summary = "To a@b.c"
        var field = Silo_V1_ApprovalField()
        field.label = "To"
        field.value = "a@b.c"
        approval.fields = [field]
        approval.action = "send"
        approval.connector = "email"
        let d = describeApproval(approval)
        XCTAssertEqual(d.title, "Send email")
        XCTAssertEqual(d.fields, [.init(label: "To", value: "a@b.c")])
    }

    func testFallbackFlattensArgs() {
        var approval = Silo_V1_Approval()
        approval.connector = "github"
        approval.action = "create_issue"
        approval.argsJson = #"{"title":"Bug","labels":["a","b"],"empty":"","none":null}"#
        let d = describeApproval(approval)
        XCTAssertEqual(d.title, "Create Issue · Github")
        XCTAssertEqual(d.summary, "This Bot wants to create issue (Github).")
        XCTAssertEqual(d.fields, [.init(label: "Labels", value: #"["a","b"]"#), .init(label: "Title", value: "Bug")])
    }

    func testFallbackSurvivesBadJSON() {
        var approval = Silo_V1_Approval()
        approval.connector = "x"
        approval.action = "y"
        approval.argsJson = "not json"
        XCTAssertTrue(describeApproval(approval).fields.isEmpty)
    }
}
