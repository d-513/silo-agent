package reader

import (
	"encoding/xml"
	"fmt"
	"strings"
	"time"

	"silo.agent/internal/htmltext"
)

// feedItems is how many entries of a feed are listed.
const feedItems = 50

// feed is an RSS or Atom document, read loosely: both shapes in one struct,
// whichever the file has.
type feed struct {
	Channel struct {
		Title       string     `xml:"title"`
		Description string     `xml:"description"`
		Items       []feedItem `xml:"item"`
	} `xml:"channel"`
	Title   string      `xml:"title"`
	Entries []feedEntry `xml:"entry"`
}

type feedItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	Date        string `xml:"pubDate"`
	Description string `xml:"description"`
	Duration    string `xml:"http://www.itunes.com/dtds/podcast-1.0.dtd duration"`
	Enclosure   struct {
		URL  string `xml:"url,attr"`
		Type string `xml:"type,attr"`
	} `xml:"enclosure"`
}

type feedEntry struct {
	ID        string `xml:"id"`
	Title     string `xml:"title"`
	Updated   string `xml:"updated"`
	Published string `xml:"published"`
	Summary   string `xml:"summary"`
	Content   string `xml:"content"`
	Comment   string `xml:"http://arxiv.org/schemas/atom comment"`
	Links     []struct {
		Href  string `xml:"href,attr"`
		Rel   string `xml:"rel,attr"`
		Title string `xml:"title,attr"`
		Type  string `xml:"type,attr"`
	} `xml:"link"`
	Authors []struct {
		Name string `xml:"name"`
	} `xml:"author"`
	Categories []struct {
		Term string `xml:"term,attr"`
	} `xml:"category"`
}

func parseFeed(body []byte) (*feed, error) {
	var f feed
	dec := xml.NewDecoder(strings.NewReader(string(decode(body, "application/xml"))))
	dec.Strict = false
	dec.CharsetReader = nil
	if err := dec.Decode(&f); err != nil {
		return nil, err
	}
	return &f, nil
}

// feedDoc lists a feed's entries: what each is, when, where, and the audio a
// podcast episode carries. A feed is links, so they are always kept.
func feedDoc(d *doc, body []byte) {
	d.kind = "feed"
	f, err := parseFeed(body)
	if err != nil {
		d.kind, d.text = "text", tidy(string(decode(body, "")))
		return
	}
	var b strings.Builder
	d.title = first(f.Channel.Title, f.Title, d.title)
	if s := brief(f.Channel.Description, 400); s != "" {
		b.WriteString(s + "\n\n")
	}
	n := 0
	for _, it := range f.Channel.Items {
		if n++; n > feedItems {
			break
		}
		fmt.Fprintf(&b, "- **%s**%s %s\n", oneLine(it.Title), dated(it.Date), strings.TrimSpace(it.Link))
		if s := brief(it.Description, 300); s != "" {
			b.WriteString("  " + s + "\n")
		}
		if it.Enclosure.URL != "" {
			fmt.Fprintf(&b, "  audio: %s%s\n", it.Enclosure.URL, lasting(it.Duration))
		}
	}
	for _, e := range f.Entries {
		if n++; n > feedItems {
			break
		}
		link := ""
		for _, l := range e.Links {
			if l.Rel == "" || l.Rel == "alternate" {
				link = l.Href
				break
			}
		}
		fmt.Fprintf(&b, "- **%s**%s %s\n", oneLine(e.Title), dated(first(e.Published, e.Updated)), first(link, e.ID))
		if s := brief(first(e.Summary, e.Content), 300); s != "" {
			b.WriteString("  " + s + "\n")
		}
	}
	if total := len(f.Channel.Items) + len(f.Entries); total > feedItems {
		fmt.Fprintf(&b, "\n(the newest %d of %d entries)\n", feedItems, total)
	}
	d.text = tidy(b.String())
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// brief is an entry's description as one plain line, cut to max characters.
func brief(s string, max int) string {
	if strings.Contains(s, "<") {
		s = htmltext.Flatten(s)
	}
	s = oneLine(s)
	if r := []rune(s); len(r) > max {
		s = string(r[:max]) + "…"
	}
	return s
}

var feedDates = []string{time.RFC1123Z, time.RFC1123, time.RFC3339, "Mon, 2 Jan 2006 15:04:05 -0700", "2006-01-02T15:04:05Z"}

// day is a feed's date as a day, or as written when it cannot be read.
func day(s string) string {
	s = strings.TrimSpace(s)
	for _, layout := range feedDates {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format("2006-01-02")
		}
	}
	return s
}

func dated(s string) string {
	if s = day(s); s == "" {
		return ""
	}
	return " (" + s + ")"
}

func lasting(s string) string {
	if s = strings.TrimSpace(s); s == "" {
		return ""
	}
	return " (" + s + ")"
}
