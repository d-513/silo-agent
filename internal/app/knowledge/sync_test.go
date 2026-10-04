package knowledge

import (
	"testing"
	"time"

	"silo.agent/internal/db"
)

func TestSourceUnchangedRevisitsScansOnceOCRIsOn(t *testing.T) {
	now := time.Now()
	wf := walkEntry{Path: "docs/scan.pdf", Size: 10, Mtime: 5}
	scan := &db.KnowledgeSource{Size: 10, Mtime: 5, Status: "skipped", Detail: "scanned (OCR is off)", EmbedModel: "m", IndexedAt: now}

	if !sourceUnchanged(scan, wf, "m", false, now) {
		t.Fatal("with OCR still off, an untouched scan is left alone")
	}
	if sourceUnchanged(scan, wf, "m", true, now) {
		t.Fatal("turning OCR on must re-read files that were skipped for it")
	}

	partial := &db.KnowledgeSource{Size: 10, Mtime: 5, Status: "ok", Detail: "3 of 10 pages are scans (OCR is off)", EmbedModel: "m", IndexedAt: now}
	if sourceUnchanged(partial, wf, "m", true, now) {
		t.Fatal("a partly scanned file indexed without OCR must be re-read once OCR is on")
	}

	// A file that OCR already looked at is not retried every sweep.
	read := &db.KnowledgeSource{Size: 10, Mtime: 5, Status: "skipped", Detail: "no text found in the image", EmbedModel: "m", IndexedAt: now}
	if !sourceUnchanged(read, wf, "m", true, now) {
		t.Fatal("an image with no text stays skipped")
	}
}

func TestSourceUnchangedBasics(t *testing.T) {
	now := time.Now()
	wf := walkEntry{Size: 10, Mtime: 5}
	ok := &db.KnowledgeSource{Size: 10, Mtime: 5, Status: "ok", EmbedModel: "m", IndexedAt: now}
	if !sourceUnchanged(ok, wf, "m", false, now) {
		t.Fatal("same size, mtime and model is unchanged")
	}
	if sourceUnchanged(ok, walkEntry{Size: 11, Mtime: 5}, "m", false, now) || sourceUnchanged(ok, walkEntry{Size: 10, Mtime: 6}, "m", false, now) {
		t.Fatal("a different size or mtime is a candidate")
	}
	if sourceUnchanged(ok, wf, "other", false, now) {
		t.Fatal("a different embedding model re-embeds")
	}
	bad := &db.KnowledgeSource{Size: 10, Mtime: 5, Status: "error", IndexedAt: now.Add(-2 * time.Hour)}
	if sourceUnchanged(bad, wf, "m", false, now) {
		t.Fatal("an error source is retried after an hour")
	}
	if !sourceUnchanged(&db.KnowledgeSource{Size: 10, Mtime: 5, Status: "error", IndexedAt: now}, wf, "m", false, now) {
		t.Fatal("a fresh error is not retried every sweep")
	}
}
