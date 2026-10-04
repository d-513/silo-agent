package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

// OCR for the knowledge index: scanned PDF pages and image files go through
// tesseract (in the bot image, with eng + pol). A PDF is read with pdftotext
// first; only the pages with next to no text are rasterized (pdftoppm) and
// recognized, so a born-digital PDF costs nothing extra.
const (
	// ocrMinChars is how little text a PDF page may carry before it counts as
	// a scan.
	ocrMinChars = 20
	// ocrMinImageBytes skips icons and thumbnails: nothing readable fits.
	ocrMinImageBytes = 8 << 10
	// ocrDPI is the resolution pages are rasterized at; 200 reads body text
	// reliably and keeps a page to a second or two.
	ocrDPI = "200"
	// ocrOffNote is what a scan reports when OCR is off. The CP looks for it
	// to re-read such files once OCR is turned on.
	ocrOffNote = "OCR is off"
	// ocrTextRatio is the share of non-space characters that must be letters
	// or digits for recognized text to count as text and not photo noise.
	ocrTextRatio = 0.6
)

var (
	// ocrMaxPages caps the pages OCR'd per file; a longer scan is partial.
	ocrMaxPages = 100
	// ocrBudget bounds one file's OCR so the command's own 5 minute limit does
	// not turn a long scan into an error.
	ocrBudget = 4 * time.Minute
)

var imageExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".tif": true, ".tiff": true,
	".bmp": true, ".gif": true, ".webp": true,
}

func nonSpace(s string) int {
	n := 0
	for _, r := range s {
		if !unicode.IsSpace(r) {
			n++
		}
	}
	return n
}

// sparsePages returns the indexes of pages with too little text to be real.
func sparsePages(pages []string) []int {
	var out []int
	for i, p := range pages {
		if nonSpace(p) < ocrMinChars {
			out = append(out, i)
		}
	}
	return out
}

// looksLikeText reports whether recognized text is prose-like. Tesseract on a
// photo answers with symbol soup, which must not reach the index.
func looksLikeText(s string) bool {
	total, alnum := 0, 0
	for _, r := range s {
		if unicode.IsSpace(r) {
			continue
		}
		total++
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			alnum++
		}
	}
	return total >= ocrMinChars && float64(alnum)/float64(total) >= ocrTextRatio
}

// applyOCR puts recognized text back on its pages. Only pages named in
// sparse and up to limit are replaced; the rest keep what pdftotext found.
func applyOCR(pages []string, got map[int]string, limit int) []string {
	out := append([]string(nil), pages...)
	for i, txt := range got {
		if i < limit && i < len(out) {
			out[i] = txt
		}
	}
	return out
}

var (
	langsOnce sync.Once
	langs     string
)

// ocrLangs is every installed tesseract language but osd, English first
// ("eng+pol"), so a Polish scan is not read as English.
func ocrLangs() string {
	langsOnce.Do(func() {
		langs = "eng"
		out, err := exec.Command("tesseract", "--list-langs").Output()
		if err != nil {
			return
		}
		var have []string
		for _, l := range strings.Fields(string(out)) {
			if l == "osd" || strings.HasPrefix(l, "List") || strings.HasSuffix(l, ":") || strings.Contains(l, "/") {
				continue
			}
			have = append(have, l)
		}
		sort.Slice(have, func(i, j int) bool {
			if (have[i] == "eng") != (have[j] == "eng") {
				return have[i] == "eng"
			}
			return have[i] < have[j]
		})
		if len(have) > 0 {
			langs = strings.Join(have, "+")
		}
	})
	return langs
}

// ocrEnv limits tesseract to one thread: pages run in parallel instead.
func ocrEnv() []string { return append(os.Environ(), "OMP_THREAD_LIMIT=1") }

// runOCRTool runs tesseract (or pdftoppm) without a shell, one thread each.
func runOCRTool(ctx context.Context, name string, args ...string) (string, error) {
	return runTool(ctx, ocrEnv(), name, args...)
}

// ocrPage rasterizes one page of a PDF and recognizes it.
func ocrPage(ctx context.Context, pdf string, page int) (string, error) {
	dir, err := os.MkdirTemp("", "silo-ocr-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	root := filepath.Join(dir, "pg")
	pn := fmt.Sprint(page)
	if _, err := runOCRTool(ctx, "pdftoppm", "-f", pn, "-l", pn, "-r", ocrDPI, "-gray", "-png", "-singlefile", pdf, root); err != nil {
		return "", err
	}
	return runOCRTool(ctx, "tesseract", root+".png", "stdout", "-l", ocrLangs())
}

// ocrPDF reads the scanned pages of a PDF whose text layer is in text (pages
// split by form feed) and returns the merged text and a note for the owner.
// It never fails the file: a page that cannot be read is left as it was and
// the note says so.
func ocrPDF(ctx context.Context, pdf, text string, ocr bool) (string, string) {
	pages := strings.Split(text, "\f")
	n := len(pages)
	if n > 1 && strings.TrimSpace(pages[n-1]) == "" {
		n-- // pdftotext ends every page with a form feed
	}
	sparse := sparsePages(pages[:n])
	if len(sparse) == 0 {
		return text, ""
	}
	if !ocr {
		if len(sparse) == n {
			return text, "scanned (" + ocrOffNote + ")"
		}
		return text, fmt.Sprintf("%d of %d pages are scans (%s)", len(sparse), n, ocrOffNote)
	}
	for _, tool := range []string{"pdftoppm", "tesseract"} {
		if _, err := exec.LookPath(tool); err != nil {
			return text, fmt.Sprintf("%d scanned pages not read: %s is not installed in this Bot's image", len(sparse), tool)
		}
	}
	todo := sparse
	if len(todo) > ocrMaxPages {
		todo = todo[:ocrMaxPages]
	}
	octx, cancel := context.WithTimeout(ctx, ocrBudget)
	defer cancel()

	workers := min(runtime.NumCPU(), 3)
	var (
		mu   sync.Mutex
		got  = map[int]string{}
		wg   sync.WaitGroup
		jobs = make(chan int)
	)
	for i := 0; i < max(workers, 1); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				if octx.Err() != nil {
					continue
				}
				txt, err := ocrPage(octx, pdf, idx+1)
				if err == nil && looksLikeText(txt) {
					mu.Lock()
					got[idx] = strings.TrimSpace(txt)
					mu.Unlock()
				}
			}
		}()
	}
	for _, idx := range todo {
		jobs <- idx
	}
	close(jobs)
	wg.Wait()

	merged := strings.Join(applyOCR(pages, got, n), "\f")
	note := fmt.Sprintf("OCR read %d of %d scanned pages", len(got), len(sparse))
	switch {
	case len(got) == 0:
		note = "scanned pages had no readable text"
	case len(todo) < len(sparse):
		note = fmt.Sprintf("OCR read %d of %d scanned pages (only the first %d are tried)", len(got), len(sparse), len(todo))
	case octx.Err() != nil:
		note += " (stopped at the time limit)"
	}
	return merged, note
}
