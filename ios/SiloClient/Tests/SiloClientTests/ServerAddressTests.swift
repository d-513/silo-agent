import XCTest
@testable import SiloClient

final class ServerAddressTests: XCTestCase {
    func testAddsTheSchemeAPersonLeftOut() {
        XCTAssertEqual(serverAddress("silo.example.com"), "https://silo.example.com")
        XCTAssertEqual(serverAddress(" silo.example.com/ "), "https://silo.example.com")
        XCTAssertEqual(serverAddress("192.168.1.20:8080"), "http://192.168.1.20:8080")
        XCTAssertEqual(serverAddress("localhost:8080"), "http://localhost:8080")
        XCTAssertEqual(serverAddress("mac-mini.local:8080"), "http://mac-mini.local:8080")
        XCTAssertEqual(serverAddress("homebox:8080"), "http://homebox:8080")
    }

    func testKeepsAnExplicitSchemeAndPath() {
        XCTAssertEqual(serverAddress("http://silo.example.com"), "http://silo.example.com")
        XCTAssertEqual(serverAddress("https://example.com/silo/"), "https://example.com/silo")
    }

    func testRefusesWhatCannotBeAServer() {
        XCTAssertNil(serverAddress(""))
        XCTAssertNil(serverAddress("   "))
        XCTAssertNil(serverAddress("silo example"))
        XCTAssertNil(serverAddress("ftp://silo.example.com"))
        XCTAssertNil(serverAddress("https://silo.example.com/?x=1"))
    }

    func testLabel() {
        XCTAssertEqual(serverLabel("https://silo.example.com"), "silo.example.com")
        XCTAssertEqual(serverLabel("http://192.168.1.20:8080"), "192.168.1.20:8080")
        XCTAssertEqual(serverLabel("https://example.com/silo"), "example.com/silo")
    }
}
