package reader

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode"
)

// A page is cut for one answer in one of two ways: a window from an offset,
// with the outline of the rest, or only the passages that answer a query.

const (
	// passageSize is about how long one passage is, in characters.
	passageSize = 1200
	// outlineMax is how many headings an outline lists.
	outlineMax = 80
)

// view cuts a page for one request.
func view(d *doc, rq Request) Page {
	p := Page{
		URL: d.url, Title: d.title, Site: d.site, Author: d.author, Date: d.date, Language: d.language,
		Kind: d.kind, Via: d.via, Status: d.status, Problem: d.problem, Note: d.note, Data: d.data,
	}
	text := []rune(d.text)
	p.TotalChars = len(text)
	max := rq.MaxChars
	switch {
	case max <= 0:
		max = DefaultMaxChars
	case max > MaxCharsCap:
		max = MaxCharsCap
	}
	if q := strings.TrimSpace(rq.Query); q != "" && len(text) > max {
		if out, n, of := passages(d.text, q, max); n > 0 {
			p.Text, p.Passages = out, fmt.Sprintf("%d of %d", n, of)
			return p
		}
		p.Note = join(p.Note, "Nothing on the page matched the query; this is the page from its start.")
		rq.Offset = 0
	}
	start := rq.Offset
	if start > len(text) {
		start = len(text)
	}
	end := start + max
	if end >= len(text) {
		end = len(text)
	} else {
		end = cut(text, start, end)
		p.NextOffset = end
	}
	p.Text, p.Offset = strings.TrimSpace(string(text[start:end])), start
	if start > 0 || end < len(text) {
		p.Outline = outline(text)
	}
	return p
}

// cut moves the end of a window back to the end of a paragraph, or of a line,
// when one is in its last quarter.
func cut(text []rune, start, end int) int {
	floor := end - (end-start)/4
	line := -1
	for i := end; i > floor; i-- {
		if text[i-1] != '\n' {
			continue
		}
		if i >= 2 && text[i-2] == '\n' {
			return i
		}
		if line < 0 {
			line = i
		}
	}
	if line > 0 {
		return line
	}
	return end
}

// outline lists a page's headings (three levels) with where each starts.
func outline(text []rune) []Heading {
	var out []Heading
	fenced, lineStart := false, 0
	for i := 0; i <= len(text); i++ {
		if i < len(text) && text[i] != '\n' {
			continue
		}
		line := string(text[lineStart:i])
		switch level := headingLevel(line); {
		case strings.HasPrefix(strings.TrimSpace(line), "```"):
			fenced = !fenced
		case !fenced && level >= 1 && level <= 3 && len(out) < outlineMax:
			out = append(out, Heading{Heading: strings.TrimSpace(line), Offset: lineStart})
		}
		lineStart = i + 1
	}
	return out
}

// headingLevel is 1 to 6 for an ATX heading line, else 0.
func headingLevel(line string) int {
	n := 0
	for n < len(line) && line[n] == '#' {
		n++
	}
	if n == 0 || n > 6 || n >= len(line) || line[n] != ' ' {
		return 0
	}
	return n
}

// passage is one piece of a page, under the headings it sits below.
type passage struct {
	path string
	text string
	n    int
}

// split cuts a page into passages: a heading starts a new one, and a long
// section is cut at its paragraphs.
func split(text string) []passage {
	var out []passage
	var heads [6]string
	var cur strings.Builder
	path := ""
	flush := func() {
		if t := strings.TrimSpace(cur.String()); t != "" {
			out = append(out, passage{path: path, text: t, n: len(out)})
		}
		cur.Reset()
	}
	fenced := false
	for _, para := range strings.Split(text, "\n\n") {
		trimmed := strings.TrimSpace(para)
		if strings.Count(trimmed, "```")%2 == 1 {
			fenced = !fenced
		}
		if level := headingLevel(trimmed); level > 0 && !fenced && !strings.Contains(trimmed, "\n") {
			flush()
			heads[level-1] = strings.TrimSpace(trimmed[level:])
			for i := level; i < len(heads); i++ {
				heads[i] = ""
			}
			var parts []string
			for _, h := range heads {
				if h != "" {
					parts = append(parts, h)
				}
			}
			path = strings.Join(parts, " › ")
			continue
		}
		if cur.Len() > 0 && !fenced && cur.Len()+len(para) > passageSize*2 {
			flush()
		}
		if cur.Len() > 0 {
			cur.WriteString("\n\n")
		}
		cur.WriteString(para)
		if !fenced && cur.Len() >= passageSize {
			flush()
		}
	}
	flush()
	return out
}

// tokens are a text's words, lower-cased; a one-letter word says nothing.
func tokens(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// passages picks the pieces of a page that answer a query, by BM25 over the
// passages with each one's headings counted as its own words, and returns
// them in the page's order. n is how many were returned, of how many.
func passages(text, query string, max int) (out string, n, of int) {
	all := split(text)
	of = len(all)
	terms := map[string]bool{}
	for _, t := range tokens(query) {
		if len([]rune(t)) > 1 {
			terms[t] = true
		}
	}
	if of == 0 || len(terms) == 0 {
		return "", 0, of
	}
	const k1, b = 1.5, 0.75
	freq := make([]map[string]int, of)
	length := make([]int, of)
	seen := map[string]int{}
	total := 0
	for i, p := range all {
		words := append(tokens(p.text), tokens(p.path)...)
		words = append(words, tokens(p.path)...)
		freq[i] = map[string]int{}
		for _, w := range words {
			if terms[w] {
				freq[i][w]++
			}
		}
		for w := range freq[i] {
			seen[w]++
		}
		length[i] = len(words)
		total += len(words)
	}
	avg := float64(total) / float64(of)
	type scored struct {
		i     int
		score float64
	}
	var ranked []scored
	for i := range all {
		score := 0.0
		for w, f := range freq[i] {
			idf := math.Log(1 + (float64(of)-float64(seen[w])+0.5)/(float64(seen[w])+0.5))
			score += idf * float64(f) * (k1 + 1) / (float64(f) + k1*(1-b+b*float64(length[i])/avg))
		}
		if score > 0 {
			ranked = append(ranked, scored{i, score})
		}
	}
	if len(ranked) == 0 {
		return "", 0, of
	}
	sort.SliceStable(ranked, func(x, y int) bool { return ranked[x].score > ranked[y].score })
	var picked []int
	used := 0
	for _, r := range ranked {
		size := runes(all[r.i].text) + runes(all[r.i].path) + 8
		if used+size > max && len(picked) > 0 {
			continue
		}
		picked = append(picked, r.i)
		used += size
	}
	sort.Ints(picked)
	var sb strings.Builder
	last, lastPath := -2, "\x00"
	for _, i := range picked {
		p := all[i]
		if sb.Len() > 0 {
			if i != last+1 {
				sb.WriteString("\n\n[…]")
			}
			sb.WriteString("\n\n")
		}
		if p.path != lastPath && p.path != "" {
			sb.WriteString("[" + p.path + "]\n\n")
		}
		sb.WriteString(p.text)
		last, lastPath = i, p.path
	}
	out = sb.String()
	if r := []rune(out); len(r) > max {
		out = string(r[:max])
	}
	return out, len(picked), of
}
