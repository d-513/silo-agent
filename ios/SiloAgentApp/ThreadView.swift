import Observation
import SiloClient
import SwiftUI

struct ThreadView: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        ScrollViewReader { proxy in
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 14) {
                    let blocks = foldEvents(model.events)
                    if blocks.isEmpty && !model.isRunning {
                        Text("Send a message to start.")
                            .font(.callout)
                            .foregroundStyle(.secondary)
                            .frame(maxWidth: .infinity)
                            .padding(.top, 48)
                    }
                    ForEach(blocks) { block in
                        BlockView(block: block).id(block.id)
                    }
                }
                .padding(.horizontal, 16)
                .padding(.vertical, 12)
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            .scrollDismissesKeyboard(.interactively)
            .onChange(of: model.events.count) { _, _ in
                guard let last = foldEvents(model.events).last else { return }
                withAnimation(.easeOut(duration: 0.15)) {
                    proxy.scrollTo(last.id, anchor: .bottom)
                }
            }
            .onAppear {
                guard let last = foldEvents(model.events).last else { return }
                proxy.scrollTo(last.id, anchor: .bottom)
            }
        }
    }
}

struct BlockView: View {
    let block: Block

    var body: some View {
        switch block.kind {
        case .user:
            userBubble
        case .assistant:
            ProseView(text: block.text)
        case .thinking:
            thinkingRow
        case .tool:
            toolRow(title: block.name)
        case .call:
            toolRow(title: block.text.isEmpty ? block.name : block.text)
        case .error:
            Text(block.text)
                .font(.callout)
                .foregroundStyle(.red)
                .textSelection(.enabled)
        }
    }

    private var userBubble: some View {
        HStack(spacing: 0) {
            Spacer(minLength: 40)
            Text(block.text)
                .padding(.horizontal, 14)
                .padding(.vertical, 10)
                .background(Color.accentColor.opacity(0.15), in: RoundedRectangle(cornerRadius: 16))
                .textSelection(.enabled)
        }
        .frame(maxWidth: .infinity, alignment: .trailing)
    }

    private var thinkingRow: some View {
        DisclosureGroup {
            Text(block.text)
                .font(.callout)
                .foregroundStyle(.secondary)
                .textSelection(.enabled)
                .padding(.top, 4)
        } label: {
            HStack(spacing: 6) {
                if block.streaming { ProgressView().controlSize(.mini) }
                Text(block.streaming ? "Thinking…" : "Thought")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        }
    }

    private func toolRow(title: String) -> some View {
        DisclosureGroup {
            VStack(alignment: .leading, spacing: 8) {
                if !block.args.isEmpty {
                    CodeWell(code: prettyJSON(block.args), language: nil)
                }
                if let result = block.result, !result.isEmpty {
                    CodeWell(code: result, language: nil)
                }
            }
            .padding(.top, 6)
        } label: {
            HStack(spacing: 6) {
                if block.running {
                    ProgressView().controlSize(.mini)
                } else {
                    Image(systemName: "checkmark.circle")
                        .foregroundStyle(.secondary)
                }
                Text(title)
                    .font(.system(.caption, design: .monospaced).weight(.medium))
            }
        }
    }
}

struct ProseView: View {
    let text: String

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            ForEach(Prose.parse(text)) { part in
                switch part {
                case .text(let value):
                    Text(attributed(value))
                        .textSelection(.enabled)
                case .code(let value, let language):
                    CodeWell(code: value, language: language)
                }
            }
        }
    }

    private func attributed(_ value: String) -> AttributedString {
        (try? AttributedString(markdown: value)) ?? AttributedString(value)
    }
}

struct CodeWell: View {
    let code: String
    let language: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            if let language, !language.isEmpty {
                Text(language.uppercased())
                    .font(.caption2.weight(.medium))
                    .foregroundStyle(.secondary)
                    .padding(.horizontal, 10)
                    .padding(.top, 6)
            }
            ScrollView(.horizontal, showsIndicators: false) {
                Text(code)
                    .font(.system(.footnote, design: .monospaced))
                    .textSelection(.enabled)
                    .padding(10)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Color(.secondarySystemBackground), in: RoundedRectangle(cornerRadius: 10))
    }
}
