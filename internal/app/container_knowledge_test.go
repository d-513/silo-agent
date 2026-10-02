package app_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/apptest"
	"silo.agent/internal/ids"
)

// seedDocsScript builds, inside the bot image, a folder with a born-digital
// markdown note, a Word document, a text PDF, a scanned (image-only) PDF, and
// a PNG of text: every extractor the knowledge index uses.
const seedDocsScript = `
set -e
mkdir -p /workspace/docs && cd /workspace/docs
printf '# Fleet notes\n\nThe harbour crane inspection happens every March.\n' > notes.md
printf '# Warranty\n\nThe turbine warranty lasts seven years from delivery.\n' > /tmp/w.md
pandoc /tmp/w.md -o warranty.docx
python3 - <<'PY'
from PIL import Image, ImageDraw, ImageFont
from reportlab.pdfgen import canvas
font = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf", 44)
def render(text, size=(1700, 500)):
    img = Image.new("RGB", size, "white")
    ImageDraw.Draw(img).text((70, 70), text, font=font, fill="black")
    return img
render("Replacement valve ships from the Rotterdam warehouse").save("scan.pdf", "PDF", resolution=200.0)
render("Invoice number 4471 covers the Antwerp pallet").save("receipt.png")
c = canvas.Canvas("digital.pdf"); c.setFont("Helvetica", 14)
c.drawString(72, 720, "Digital report: the compressor overhaul finished on schedule"); c.save()
PY
`

func runInBox(t *testing.T, h *apptest.H, botID, script string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	out, err := h.App.Hub.Exec(ctx, botID, &v1.Cmd{Id: ids.New(), Body: &v1.Cmd_Terminal{Terminal: &v1.TerminalCmd{Command: script}}})
	if err != nil {
		t.Fatalf("seed docs: %v\n%s", err, out)
	}
}

// TestContainerKnowledgeAllFormats indexes real documents in a real Bot image:
// the whole path from worker Walk/Extract (pdftotext, pandoc, tesseract) to a
// hybrid search that cites a page.
func TestContainerKnowledgeAllFormats(t *testing.T) {
	if !apptest.ContainersEnabled(t) {
		return
	}
	h := newContainerHarness(t)
	id := h.SeedBot("Archivist")
	h.StartSeededBot(id)
	runInBox(t, h, id, seedDocsScript)

	added, err := h.Client.AddKnowledgeFolder(h.Ctx(), connect.NewRequest(&v1.AddKnowledgeFolderRequest{BotId: id, Path: "docs"}))
	if err != nil {
		t.Fatal(err)
	}
	if err := h.App.SyncKnowledgeFolder(h.Ctx(), added.Msg.GetId()); err != nil {
		t.Fatalf("sync: %v", err)
	}
	list, err := h.Client.ListKnowledge(h.Ctx(), connect.NewRequest(&v1.ListKnowledgeRequest{BotId: id}))
	if err != nil {
		t.Fatal(err)
	}
	fo := list.Msg.GetFolders()[0]
	if fo.GetStatus() != "idle" || fo.GetFiles() != 5 || fo.GetSkipped() != 0 {
		t.Fatalf("folder = %+v (issues %+v)", fo, fo.GetIssues())
	}

	cases := []struct{ query, path, locator string }{
		{"when is the harbour crane inspection", "docs/notes.md", ""},
		{"how long does the turbine warranty last", "docs/warranty.docx", ""},
		{"compressor overhaul schedule", "docs/digital.pdf", "p. 1"},
		{"replacement valve Rotterdam warehouse", "docs/scan.pdf", "p. 1"}, // OCR'd page
		{"invoice 4471 Antwerp pallet", "docs/receipt.png", ""},            // OCR'd image
	}
	for _, c := range cases {
		res, err := h.Client.SearchKnowledge(h.Ctx(), connect.NewRequest(&v1.SearchKnowledgeRequest{BotId: id, Query: c.query}))
		if err != nil {
			t.Fatalf("%q: %v", c.query, err)
		}
		hits := res.Msg.GetHits()
		if len(hits) == 0 || hits[0].GetPath() != c.path {
			t.Fatalf("%q: top hit = %+v, want %s", c.query, hits, c.path)
		}
		if c.locator != "" && !strings.HasPrefix(hits[0].GetLocator(), c.locator) {
			t.Fatalf("%q: locator %q, want %q", c.query, hits[0].GetLocator(), c.locator)
		}
	}
}
