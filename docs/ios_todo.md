# iOS TODO — Step 2 (Artifacts, Quoting, Content Host, Previewers)

The web and backend changes for versioned artifacts, live apps, public shares, structured quotes, and rich file previewers landed first and intentionally left the iOS client behind. Here is what Step 2 needs to update in `ios/`.

## 1. Regenerate Swift Protobuf & Connect stubs

`proto/silo/v1/ui.proto` and `proto/silo/v1/worker.proto` changed:
- **`SaveSkillRequest`**: replaced `path` with `artifact_id` (`string`) and `version` (`int32`).
- **`Settings`**: `tunnels_host` (`10`) became `content_host` (`10`).
- **`SendRequest` & `EditMessageRequest`**: added `repeated Quote quotes = 6` (`Quote{source, ref, text, locator, path, version, label}`).
- **New `UI` RPCs**: `GetArtifact`, `ReadArtifactFile`, `OpenArtifactApp`, `ShareArtifact`, `ListArtifactShares`, `DeleteArtifactShare`.

Regenerate `ios/SiloClient/Sources/SiloClient/Generated/silo/v1/*` once Swift proto generation is run.

## 2. Fold (`ios/SiloClient/Sources/SiloClient/Logic/Fold.swift` & `ios/SiloAgentApp/Fold.swift`)

Mirror the web fold changes in `web/src/fold/`:
- **`artifact` events**:
  - Parse the new fields on `artifact.Info`: `artifact_id`, `version`, `kind` (`file` | `tree`), `pinned`, `files`, `network`, `entry`, `call_id`, and `preview` (`kind`, `text`, `rows`, `image`, `mime`).
  - Merge repeated `artifact` emits for the same `artifact_id` (or `path` within a chat) into a single `artifact` block that accumulates `versions: [ArtifactVersion]` at its first position in the thread, and emit a compact `artifact_update` block (`Updated <title> to version vN`) where later versions were emitted.
  - Pair the `artifact` tool call row to its card using `call_id` (falling back to the run heuristic only when `call_id` is absent) and drop the obsolete `approval_id` field.
- **`user` events**:
  - Parse `quotes` from `RunEvent.meta` (`{"files":[...],"quotes":[...]}`) alongside `attachments` so user bubbles render attached quotes and editing a user message preserves its quotes.
- Update `ios/SiloClient/Tests/SiloClientTests/FoldTests.swift` to match `web/src/fold/golden.json`.

## 3. Artifact URLs & `SaveSkill` (`ArtifactURL.swift`, `ThreadView.swift`, `ChatScreen.swift`)

- **`ios/SiloClient/Sources/SiloClient/Logic/ArtifactURL.swift`** (and `ArtifactURLTests.swift`):
  - Update `/artifacts/file` and `/artifacts/skill.zip` query building to pass `artifact_id` and `version` (falling back to `path` only for unversioned cards).
  - Add support for tree downloads (`/artifacts/tree.zip` or `/artifacts/file` for `app` / `skill` bundles).
- **`SaveSkill` call sites** (`ios/SiloAgentApp/ChatScreen.swift`, `ThreadView.swift`):
  - Pass `artifactId` and `version` instead of `path` to `SaveSkillRequest`.

## 4. Artifact Cards & Overlay (`ThreadView.swift`, `FileBrowser.swift`)

- Render emit-time **card previews** on `ArtifactCard` (text excerpt, CSV header/rows, or inline base64 JPEG thumbnail for images/PDFs) so cards show their content even when the Bot is stopped.
- Add the **version switcher** (`v1`, `v2`, …) and **Changes since vN** diff view in the artifact inspector overlay using `GetArtifact` and `ReadArtifactFile`.
- Support **`app` artifacts**:
  - Inline or full-screen `WKWebView` pointed at the URL returned by `OpenArtifactApp`, with a toggle to inspect the pinned `Files` tree.
  - Show the `Allows network` badge when `network == true` (and in `ApprovalText.swift` for `artifact.app_network`).
- Support **Share** (`ShareArtifact`, `ListArtifactShares`, `DeleteArtifactShare`) on file and app cards, plus a **Shared links** section on the Tunnels screen if/when Tunnels is added to iOS.

## 5. Structured Quoting (`ThreadView.swift`, `FileBrowser.swift`, `ComposerView.swift`)

- Allow selecting text in assistant/user bubbles and file/artifact previews to add a `Silo_V1_Quote` chip to the composer draft (up to 8 quotes per message).
- Support dragging a crop rectangle on rendered images (`PutFile` to `/workspace/tmp` + `Quote` with `source: "file"` / `"artifact"` and `locator: "x,y,w,h"`).
- Render quote chips above the composer (`ComposerView.swift`) and quoted blocks inside user message bubbles (`ThreadView.swift`), and pass `quotes` in `SendRequest` and `EditMessageRequest`.

## 6. Richer File Previewers (`FileBrowser.swift`)

Extend the iOS file/artifact previewer to handle the new kinds (`sheet` `.xlsx`, `slides` `.pptx`, `chart` `.vl.json`, `mermaid` `.mmd`, `notebook` `.ipynb`, `geojson`, `model` `.glb`, and sortable/filterable CSV), using native equivalents (`QLPreviewController` / `SceneKit` for `.xlsx`/`.pptx`/`.glb`, or lightweight web/Swift renderers for Vega-Lite, Mermaid, `.ipynb`, and GeoJSON).
