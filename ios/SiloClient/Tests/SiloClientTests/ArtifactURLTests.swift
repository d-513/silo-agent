import XCTest
@testable import SiloClient

final class ArtifactURLTests: XCTestCase {
    func testPendingSkillZipsWorkspacePath() {
        var a = ArtifactInfo()
        a.status = "pending"
        a.path = "skills/my skill"
        XCTAssertEqual(a.downloadPath(botID: "b1"), "/artifacts/skill.zip?bot_id=b1&path=skills%2Fmy%20skill")
        XCTAssertTrue(a.isPending)
    }

    func testSavedSkillByName() {
        var a = ArtifactInfo()
        a.status = "saved"
        a.name = "pdf"
        XCTAssertEqual(a.downloadPath(botID: "b1"), "/artifacts/skill.zip?scope=personal&name=pdf")
        XCTAssertEqual(a.downloadName, "pdf.zip")
    }

    func testFile() {
        var a = ArtifactInfo()
        a.artifactType = "file"
        a.path = "reports/q3 summary.pdf"
        XCTAssertEqual(a.downloadPath(botID: "b1"), "/artifacts/file?bot_id=b1&path=reports%2Fq3%20summary.pdf")
        XCTAssertEqual(a.downloadName, "q3 summary.pdf")
    }
}
