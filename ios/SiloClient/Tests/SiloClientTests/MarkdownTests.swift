import XCTest
@testable import SiloClient

final class MarkdownTests: XCTestCase {
    func testParagraphsSplitOnBlankLinesAndJoinSoftBreaks() {
        XCTAssertEqual(Markdown.parse("one\ntwo\n\nthree"), [.paragraph("one two"), .paragraph("three")])
    }

    func testHardBreaksStay() {
        XCTAssertEqual(Markdown.parse("one  \ntwo\\\nthree"), [.paragraph("one\ntwo\nthree")])
    }

    func testHeadings() {
        XCTAssertEqual(Markdown.parse("# Title ##\ntext\n### Sub"), [
            .heading(level: 1, text: "Title"), .paragraph("text"), .heading(level: 3, text: "Sub"),
        ])
        XCTAssertEqual(Markdown.parse("Title\n===\nBody\n---"), [
            .heading(level: 1, text: "Title"), .heading(level: 2, text: "Body"),
        ])
        XCTAssertEqual(Markdown.parse("#hashtag"), [.paragraph("#hashtag")])
    }

    func testRule() {
        XCTAssertEqual(Markdown.parse("a\n\n***\n\nb"), [.paragraph("a"), .rule, .paragraph("b")])
    }

    func testFencedCode() {
        XCTAssertEqual(Markdown.parse("Run:\n```bash\nls -la\n\necho\n```\nDone"), [
            .paragraph("Run:"), .code(text: "ls -la\n\necho", language: "bash"), .paragraph("Done"),
        ])
        XCTAssertEqual(Markdown.parse("```\nopen"), [.code(text: "open", language: nil)])
    }

    func testBulletListWithNestingAndTasks() {
        let blocks = Markdown.parse("Intro\n- one\n  - nested\n- [x] done\n- [ ] todo\n\nAfter")
        XCTAssertEqual(blocks, [
            .paragraph("Intro"),
            .list(ordered: false, items: [
                MarkdownItem(level: 0, marker: "•", text: "one"),
                MarkdownItem(level: 1, marker: "•", text: "nested"),
                MarkdownItem(level: 0, marker: "•", text: "done", checked: true),
                MarkdownItem(level: 0, marker: "•", text: "todo", checked: false),
            ]),
            .paragraph("After"),
        ])
    }

    func testOrderedListKeepsNumbersAndContinuations() {
        XCTAssertEqual(Markdown.parse("1. first\n   more\n2. second\n\n3. third"), [
            .list(ordered: true, items: [
                MarkdownItem(level: 0, marker: "1.", text: "first more"),
                MarkdownItem(level: 0, marker: "2.", text: "second"),
                MarkdownItem(level: 0, marker: "3.", text: "third"),
            ]),
        ])
    }

    func testANumberMidParagraphIsNotAList() {
        XCTAssertEqual(Markdown.parse("It grew in\n2024. Then it shrank."), [.paragraph("It grew in 2024. Then it shrank.")])
    }

    func testQuote() {
        XCTAssertEqual(Markdown.parse("> quoted\nlazy\n> - item\n\nafter"), [
            .quote([.paragraph("quoted lazy"), .list(ordered: false, items: [MarkdownItem(level: 0, marker: "•", text: "item")])]),
            .paragraph("after"),
        ])
    }

    func testTable() {
        let text = """
        Results:
        | Widget | Kind | Size |
        |:-------|:----:|-----:|
        | Note | `note` | m |
        | Pipe \\| inside | x |
        """
        XCTAssertEqual(Markdown.parse(text), [
            .paragraph("Results:"),
            .table(header: ["Widget", "Kind", "Size"], align: [.leading, .center, .trailing], rows: [
                ["Note", "`note`", "m"],
                ["Pipe | inside", "x", ""],
            ]),
        ])
    }

    func testDashesUnderALineWithoutPipesAreAHeading() {
        XCTAssertEqual(Markdown.parse("Total\n---"), [.heading(level: 2, text: "Total")])
    }

    func testTheCollapsedReplyFromTheDashboardChat() {
        let text = "I set up 12 widgets.\n\n| Widget | Kind |\n|---|---|\n| Note | note |\n\nThe App widget is unconfirmed."
        XCTAssertEqual(Markdown.parse(text).count, 3)
    }
}
