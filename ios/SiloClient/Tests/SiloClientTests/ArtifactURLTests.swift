import XCTest
@testable import SiloClient

final class ArtifactURLTests: XCTestCase {
    func testVersionedDownload() {
        var a = ArtifactInfo()
        a.artifactType = "file"
        a.artifactID = "f1"
        a.version = 2
        a.kind = "file"
        a.name = "q3 summary.pdf"
        a.path = "reports/q3 summary.pdf"
        XCTAssertEqual(a.downloadPath(botID: "b1"), "/artifacts/download?bot_id=b1&artifact_id=f1&v=2")
        XCTAssertEqual(a.downloadName, "q3 summary.pdf")
    }

    func testSkillTreeIsAZip() {
        var a = ArtifactInfo()
        a.artifactID = "s1"
        a.version = 1
        a.kind = "tree"
        a.name = "pdf"
        a.status = "pending"
        XCTAssertEqual(a.downloadPath(botID: "b1"), "/artifacts/download?bot_id=b1&artifact_id=s1&v=1")
        XCTAssertEqual(a.downloadName, "pdf.zip")
        XCTAssertTrue(a.isPending)
    }

    func testLegacySavedSkillByName() {
        var a = ArtifactInfo()
        a.status = "saved"
        a.name = "pdf"
        XCTAssertEqual(a.downloadPath(botID: "b1"), "/artifacts/skill.zip?scope=personal&name=pdf")
    }

    func testLegacyFileHasNoRoute() {
        var a = ArtifactInfo()
        a.artifactType = "file"
        a.path = "reports/q3.pdf"
        XCTAssertNil(a.downloadPath(botID: "b1"))
        XCTAssertEqual(a.downloadName, "q3.pdf")
    }
}
