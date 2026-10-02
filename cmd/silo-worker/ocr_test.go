package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSparsePages(t *testing.T) {
	pages := []string{"A real page with plenty of words on it.", "  \n ", "12", "Another full page of body text here."}
	got := sparsePages(pages)
	if len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("sparsePages = %v, want [1 2]", got)
	}
	if len(sparsePages(nil)) != 0 {
		t.Fatal("no pages, no sparse pages")
	}
}

func TestLooksLikeText(t *testing.T) {
	if !looksLikeText("Quarterly revenue grew by twelve percent over the last year.") {
		t.Fatal("prose is text")
	}
	if looksLikeText("") || looksLikeText("ab") {
		t.Fatal("too short to be text")
	}
	// Tesseract on a photo emits symbol soup; that must not be indexed.
	if looksLikeText("«|;~ ¬/\\ ^^ |‘ ~~ _ .. ;; -- ;| \\\\ // °° ~~ || ;; ,,") {
		t.Fatal("symbol noise is not text")
	}
	if !looksLikeText("Zażółć gęślą jaźń — zapytanie o fakturę numer 4471") {
		t.Fatal("non-ASCII prose is text")
	}
}

func TestApplyOCRReplacesOnlySparsePages(t *testing.T) {
	pages := []string{"born digital page text goes here and on", "", "third page with real text as well ok", ""}
	got := applyOCR(pages, map[int]string{1: "scanned words recovered by ocr", 3: "ignored"}, 3)
	want := []string{"born digital page text goes here and on", "scanned words recovered by ocr", "third page with real text as well ok", ""}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("applyOCR = %q", got)
	}
}

func TestExtractTinyImageIsSkippedWithoutOCR(t *testing.T) {
	ws := t.TempDir()
	writeTree(t, ws, map[string]string{"icon.png": "\x89PNG tiny"})
	w := &worker{workspace: ws}
	t.Setenv("PATH", t.TempDir()) // tesseract absent: the size check comes first
	out := runExtractOCR(t, w, "icon.png", 0, true)
	if out.Kind != "skipped" || !strings.Contains(out.Detail, "too small") || out.Sha256 != "" {
		t.Fatalf("out = %+v", out)
	}
}

func TestExtractImageWithOCROffIsReportedNotRead(t *testing.T) {
	ws := t.TempDir()
	writeTree(t, ws, map[string]string{"shot.png": strings.Repeat("x", 20<<10)})
	w := &worker{workspace: ws}
	out := runExtractOCR(t, w, "shot.png", 0, false)
	if out.Kind != "skipped" || !strings.Contains(out.Detail, "OCR is off") {
		t.Fatalf("out = %+v", out)
	}
}

func TestExtractImageMissingTesseractIsAnError(t *testing.T) {
	ws := t.TempDir()
	writeTree(t, ws, map[string]string{"shot.png": strings.Repeat("x", 20<<10)})
	w := &worker{workspace: ws}
	t.Setenv("PATH", t.TempDir())
	if _, err := w.extract(context.Background(), "shot.png", 0, true); err == nil || !strings.Contains(err.Error(), "tesseract") {
		t.Fatalf("want a tesseract-missing error, got %v", err)
	}
}

// The tests below need the bot image's tools (tesseract, poppler, python3 with
// PIL and reportlab). They skip on a host without them and run for real inside
// the image: see TESTING.md, "Worker tests in the bot image".

func needOCRTools(t *testing.T) {
	t.Helper()
	for _, tool := range []string{"tesseract", "pdftoppm", "pdftotext", "python3"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not installed", tool)
		}
	}
	if err := exec.Command("python3", "-c", "import PIL, reportlab").Run(); err != nil {
		t.Skip("python3 PIL/reportlab not installed")
	}
}

const scanScript = `
import sys
from PIL import Image, ImageDraw, ImageFont
out, kind = sys.argv[1], sys.argv[2]
lines = sys.argv[3:]
font = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf", 44)
img = Image.new("RGB", (1700, 700), "white")
d = ImageDraw.Draw(img)
for i, line in enumerate(lines):
    d.text((70, 70 + i * 90), line, font=font, fill="black")
if kind == "pdf":
    img.save(out, "PDF", resolution=200.0)
else:
    img.save(out)
`

const mixedScript = `
import sys
from PIL import Image, ImageDraw, ImageFont
from reportlab.pdfgen import canvas
out, digital, scanned = sys.argv[1], sys.argv[2], sys.argv[3]
font = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf", 44)
img = Image.new("RGB", (1700, 500), "white")
ImageDraw.Draw(img).text((70, 70), scanned, font=font, fill="black")
img.save(out + ".png")
c = canvas.Canvas(out)
c.setFont("Helvetica", 14)
c.drawString(72, 720, digital)
c.showPage()
c.drawImage(out + ".png", 36, 400, width=540, height=159)
c.showPage()
c.save()
`

func renderScan(t *testing.T, path, kind string, lines ...string) {
	t.Helper()
	args := append([]string{"-c", scanScript, path, kind}, lines...)
	if b, err := exec.Command("python3", args...).CombinedOutput(); err != nil {
		t.Fatalf("render %s: %v\n%s", path, err, b)
	}
}

func TestExtractScannedPDFIsReadWithOCR(t *testing.T) {
	needOCRTools(t)
	ws := t.TempDir()
	renderScan(t, filepath.Join(ws, "scan.pdf"), "pdf", "Quarterly revenue grew", "across the Rotterdam warehouse")
	w := &worker{workspace: ws}
	out := runExtractOCR(t, w, "scan.pdf", 0, true)
	low := strings.ToLower(out.Text)
	if out.Kind != "pdf" || !strings.Contains(low, "revenue") || !strings.Contains(low, "rotterdam") {
		t.Fatalf("OCR text = %q (%+v)", out.Text, out)
	}
	if !strings.Contains(out.Detail, "OCR read 1 of 1") {
		t.Fatalf("detail = %q", out.Detail)
	}
}

func TestExtractScannedPDFWithOCROffSaysSo(t *testing.T) {
	needOCRTools(t)
	ws := t.TempDir()
	renderScan(t, filepath.Join(ws, "scan.pdf"), "pdf", "Quarterly revenue grew")
	w := &worker{workspace: ws}
	out := runExtractOCR(t, w, "scan.pdf", 0, false)
	if strings.TrimSpace(out.Text) != "" || !strings.Contains(out.Detail, "OCR is off") {
		t.Fatalf("out = %+v", out)
	}
}

func TestExtractMixedPDFKeepsPageOrder(t *testing.T) {
	needOCRTools(t)
	ws := t.TempDir()
	if b, err := exec.Command("python3", "-c", mixedScript, filepath.Join(ws, "mixed.pdf"),
		"Born digital page about turbines", "Scanned page about harbours").CombinedOutput(); err != nil {
		t.Fatalf("render: %v\n%s", err, b)
	}
	w := &worker{workspace: ws}
	out := runExtractOCR(t, w, "mixed.pdf", 0, true)
	low := strings.ToLower(out.Text)
	ti, hi := strings.Index(low, "turbines"), strings.Index(low, "harbours")
	if ti < 0 || hi < 0 || ti > hi {
		t.Fatalf("want turbines then harbours, got %q", out.Text)
	}
	if !strings.Contains(out.Detail, "OCR read 1 of 1") {
		t.Fatalf("only the scanned page is OCR'd: %q", out.Detail)
	}
}

func TestExtractPDFOCRPageCap(t *testing.T) {
	needOCRTools(t)
	ws := t.TempDir()
	// Two scanned pages: PIL saves a multi-page PDF from a page list.
	script := `
import sys
from PIL import Image, ImageDraw, ImageFont
font = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf", 44)
pages = []
for text in sys.argv[2:]:
    img = Image.new("RGB", (1700, 500), "white")
    ImageDraw.Draw(img).text((70, 70), text, font=font, fill="black")
    pages.append(img)
pages[0].save(sys.argv[1], "PDF", resolution=200.0, save_all=True, append_images=pages[1:])
`
	if b, err := exec.Command("python3", "-c", script, filepath.Join(ws, "two.pdf"), "First scanned page about turbines", "Second scanned page about harbours").CombinedOutput(); err != nil {
		t.Fatalf("render: %v\n%s", err, b)
	}
	old := ocrMaxPages
	ocrMaxPages = 1
	defer func() { ocrMaxPages = old }()
	w := &worker{workspace: ws}
	out := runExtractOCR(t, w, "two.pdf", 0, true)
	low := strings.ToLower(out.Text)
	if !strings.Contains(low, "turbines") || strings.Contains(low, "harbours") {
		t.Fatalf("cap of 1 page: %q", out.Text)
	}
	if !strings.Contains(out.Detail, "1 of 2") {
		t.Fatalf("detail = %q", out.Detail)
	}
}

func TestExtractImageIsReadWithOCR(t *testing.T) {
	needOCRTools(t)
	ws := t.TempDir()
	renderScan(t, filepath.Join(ws, "receipt.png"), "png", "Invoice 4471 for the Rotterdam warehouse")
	w := &worker{workspace: ws}
	out := runExtractOCR(t, w, "receipt.png", 0, true)
	low := strings.ToLower(out.Text)
	if out.Kind != "image" || !strings.Contains(low, "invoice") || !strings.Contains(low, "rotterdam") {
		t.Fatalf("out = %+v", out)
	}
	if out.Sha256 == "" {
		t.Fatal("an OCR'd image is still hashed")
	}
}

func TestExtractPhotoWithoutTextYieldsNothing(t *testing.T) {
	needOCRTools(t)
	ws := t.TempDir()
	// Smooth gradient noise: big enough to be tried, with no text in it.
	script := `
import sys, random
from PIL import Image
random.seed(1)
img = Image.new("RGB", (800, 600))
img.putdata([(random.randint(0, 255), random.randint(0, 255), random.randint(0, 255)) for _ in range(800 * 600)])
img.save(sys.argv[1])
`
	if b, err := exec.Command("python3", "-c", script, filepath.Join(ws, "noise.png")).CombinedOutput(); err != nil {
		t.Fatalf("render: %v\n%s", err, b)
	}
	if st, _ := os.Stat(filepath.Join(ws, "noise.png")); st == nil || st.Size() < ocrMinImageBytes {
		t.Fatal("fixture too small to be tried")
	}
	w := &worker{workspace: ws}
	out := runExtractOCR(t, w, "noise.png", 0, true)
	if strings.TrimSpace(out.Text) != "" || !strings.Contains(out.Detail, "no text") {
		t.Fatalf("a photo must not index OCR noise: %+v", out)
	}
}
