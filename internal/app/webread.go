package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/tool"
	"silo.agent/internal/egress"
	"silo.agent/internal/hub"
	"silo.agent/internal/netguard"
	"silo.agent/internal/reader"
)

// blockedHost is what a read of a host the operator or the owner blocked
// answers.
const blockedHost = "%s is on this Silo's block list (Rules → Network), so it is not read"

// pageReader is the one page reader every Bot shares: its cache and its
// manners towards a site are the server's. The operator's settings are read
// each time they are needed, so a change takes no restart.
func (a *App) pageReader() *reader.Reader {
	a.readerOnce.Do(func() {
		a.reader = reader.New(reader.Options{
			Policy: func() netguard.Policy { return a.cfg().ReaderPolicy() },
			Refused: func(host string) string {
				if egress.Covers(a.cfg().Egress.Block, host) {
					return fmt.Sprintf(blockedHost, host)
				}
				return ""
			},
			GitHubToken: func() string { return a.cfg().Reader.GitHubToken },
			RenderOn:    func() bool { return a.cfg().Reader.Render },
		})
	})
	return a.reader
}

// readerBox is a Bot's machine as the page reader uses it: the headless browser
// that runs a page's scripts, and the tools that open a document.
type readerBox struct {
	a      *App
	botID  string
	locale string
}

func (b readerBox) call(ctx context.Context, cmd *v1.Cmd) (string, error) {
	raw, err := b.a.Workspace.Call(ctx, b.botID, cmd)
	if errors.Is(err, hub.ErrNoWorker) {
		return "", reader.ErrNoBox
	}
	return raw, err
}

func (b readerBox) Render(ctx context.Context, link string) (reader.Rendered, error) {
	raw, err := b.call(ctx, &v1.Cmd{Body: &v1.Cmd_Render{Render: &v1.RenderCmd{
		Url: link, BlockPrivate: b.a.cfg().Reader.BlockPrivate, Locale: b.locale,
	}}})
	if err != nil {
		return reader.Rendered{}, err
	}
	var out struct {
		URL    string `json:"url"`
		Status int    `json:"status"`
		HTML   string `json:"html"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return reader.Rendered{}, errors.New("the machine's answer could not be read")
	}
	return reader.Rendered{HTML: out.HTML, Status: out.Status, URL: out.URL}, nil
}

func (b readerBox) Document(ctx context.Context, name string, data []byte) (string, error) {
	raw, err := b.call(ctx, &v1.Cmd{Body: &v1.Cmd_ReadDoc{ReadDoc: &v1.ReadDocCmd{
		Name: name, Data: data, Ocr: b.a.cfg().Knowledge.OCR,
	}}})
	if err != nil {
		return "", err
	}
	var out struct {
		Text   string `json:"text"`
		Kind   string `json:"kind"`
		Detail string `json:"detail"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return "", errors.New("the machine's answer could not be read")
	}
	if out.Kind == "skipped" {
		if out.Detail == "" {
			out.Detail = "not a document it can read"
		}
		return "", errors.New(out.Detail)
	}
	return out.Text, nil
}

// runWebRead answers one `url` with its page, or a list of `urls` with
// {"reads": [...]} in the order asked.
func (a *App) runWebRead(ctx context.Context, c *tool.Call) (string, error) {
	var args struct {
		URL      string          `json:"url"`
		URLs     []string        `json:"urls"`
		Query    string          `json:"query"`
		Offset   int             `json:"offset"`
		MaxChars int             `json:"max_chars"`
		FullPage bool            `json:"full_page"`
		Links    bool            `json:"links"`
		Render   json.RawMessage `json:"render"`
		Fresh    bool            `json:"fresh"`
		Language string          `json:"language"`
	}
	if raw := strings.TrimSpace(c.ArgsJSON); raw != "" {
		if err := json.Unmarshal([]byte(raw), &args); err != nil {
			return "", fmt.Errorf("invalid args")
		}
	}
	mode, err := renderMode(args.Render)
	if err != nil {
		return "", err
	}
	one := strings.TrimSpace(args.URL) != ""
	var links []string
	for _, u := range args.URLs {
		if u = strings.TrimSpace(u); u != "" {
			links = append(links, u)
		}
	}
	switch {
	case one && len(links) > 0:
		return "", fmt.Errorf("give url or urls, not both")
	case one:
		links = []string{strings.TrimSpace(args.URL)}
	case len(links) == 0:
		return "", fmt.Errorf("url required")
	case len(links) > reader.MaxURLs:
		return "", fmt.Errorf("at most %d urls in one call", reader.MaxURLs)
	}
	botID := ""
	if c.Bot != nil {
		botID = c.Bot.ID
	}
	// The owner's own block list is checked here: the reader's copy of a
	// page is shared by every Bot, and this list is one Bot's.
	refused := func(link string) error {
		if !strings.Contains(link, "://") {
			link = "https://" + link
		}
		if u, err := url.Parse(link); err == nil && u.Hostname() != "" && a.Egress.Blocked(botID, u.Hostname()) {
			return fmt.Errorf(blockedHost, u.Hostname())
		}
		return nil
	}
	rqs := make([]reader.Request, len(links))
	for i, link := range links {
		rqs[i] = reader.Request{
			URL: link, Query: args.Query, MaxChars: args.MaxChars, FullPage: args.FullPage,
			Links: args.Links, Render: mode, Fresh: args.Fresh, Language: args.Language,
		}
	}
	var box reader.Box
	if botID != "" {
		box = readerBox{a: a, botID: botID, locale: args.Language}
	}
	rd := a.pageReader()
	var out any
	if one {
		if err := refused(links[0]); err != nil {
			return "", err
		}
		rqs[0].Offset = args.Offset
		page, err := rd.Read(ctx, rqs[0], box)
		if err != nil {
			return "", err
		}
		out = page
	} else {
		pages := make([]reader.Page, len(rqs))
		var ask []reader.Request
		var at []int
		for i, rq := range rqs {
			if err := refused(rq.URL); err != nil {
				pages[i] = reader.Page{URL: rq.URL, Problem: "error", Note: err.Error()}
				continue
			}
			ask, at = append(ask, rq), append(at, i)
		}
		for j, page := range rd.Many(ctx, ask, box) {
			pages[at[j]] = page
		}
		out = map[string]any{"reads": pages}
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// renderMode reads `render`: absent or null lets the reader decide, true
// always renders, false never does.
func renderMode(raw json.RawMessage) (string, error) {
	switch s := strings.Trim(strings.TrimSpace(string(raw)), `"`); s {
	case "", "null", reader.RenderAuto:
		return reader.RenderAuto, nil
	case "true", reader.RenderAlways:
		return reader.RenderAlways, nil
	case "false", reader.RenderNever:
		return reader.RenderNever, nil
	}
	return "", fmt.Errorf("render is true, false or left out")
}
