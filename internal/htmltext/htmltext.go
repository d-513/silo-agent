// Package htmltext flattens HTML into text a model can read: what the email
// connector and a Bot's mailbox do with an HTML-only message.
package htmltext

import (
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// Flatten turns an HTML body into readable text: scripts and styles
// dropped, block elements on their own lines, links kept as "text (url)".
func Flatten(src string) string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(src))
	if err != nil {
		return src
	}
	doc.Find("script, style, head, noscript").Remove()
	doc.Find("a[href]").Each(func(_ int, s *goquery.Selection) {
		href, _ := s.Attr("href")
		t := strings.TrimSpace(s.Text())
		if strings.HasPrefix(href, "http") && t != "" && t != href {
			s.SetText(t + " (" + href + ")")
		}
	})
	doc.Find("br").ReplaceWithHtml("\n")
	doc.Find("p, div, tr, li, h1, h2, h3, h4, h5, h6, blockquote, table").Each(func(_ int, s *goquery.Selection) {
		s.AppendHtml("\n")
	})
	var lines []string
	blank := false
	for _, l := range strings.Split(doc.Text(), "\n") {
		l = strings.Join(strings.Fields(l), " ")
		if l == "" {
			if !blank && len(lines) > 0 {
				lines = append(lines, "")
			}
			blank = true
			continue
		}
		lines = append(lines, l)
		blank = false
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
