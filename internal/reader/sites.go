package reader

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// A site with an API is asked through it: the answer is the content itself,
// with no markup to take it out of and no scripts to run. A handler that
// cannot answer returns nil and the address is read as a page.
type site struct {
	name  string
	match func(u *url.URL) bool
	read  func(ctx context.Context, r *Reader, u *url.URL, rq Request, box Box) (*doc, error)
}

var sites = []site{
	{"github", onHost("github.com"), readGitHub},
	{"youtube", func(u *url.URL) bool { return videoID(u) != "" }, readYouTube},
	{"arxiv", onHost("arxiv.org"), readArxiv},
	{"podcast", onHost("podcasts.apple.com"), readApplePodcast},
}

func onHost(host string) func(*url.URL) bool {
	return func(u *url.URL) bool {
		return strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.") == host
	}
}

// api asks a public API under our own name.
func (r *Reader) api(ctx context.Context, link string, headers map[string]string) (*response, error) {
	return r.send(ctx, &fetchReq{url: link, headers: headers, api: true})
}

// apiJSON asks an API and reads a 200's JSON into v; ok is false for
// anything else.
func (r *Reader) apiJSON(ctx context.Context, link string, headers map[string]string, v any) bool {
	resp, err := r.api(ctx, link, headers)
	if err != nil || resp.status != http.StatusOK {
		return false
	}
	return json.Unmarshal(resp.body, v) == nil
}

func segments(u *url.URL) []string {
	var out []string
	for _, s := range strings.Split(u.Path, "/") {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// --- GitHub ---

// githubPages are first path segments that are GitHub's own pages, not an
// owner.
var githubPages = map[string]bool{
	"orgs": true, "settings": true, "marketplace": true, "features": true, "topics": true, "search": true,
	"sponsors": true, "about": true, "login": true, "join": true, "pricing": true, "explore": true,
	"notifications": true, "collections": true, "trending": true, "enterprise": true, "apps": true,
}

func (r *Reader) githubHeaders(accept string) map[string]string {
	h := map[string]string{"Accept": accept, "X-GitHub-Api-Version": "2022-11-28"}
	if r.opts.GitHubToken != nil {
		if t := strings.TrimSpace(r.opts.GitHubToken()); t != "" {
			h["Authorization"] = "Bearer " + t
		}
	}
	return h
}

func readGitHub(ctx context.Context, r *Reader, u *url.URL, rq Request, _ Box) (*doc, error) {
	seg := segments(u)
	if len(seg) < 2 || githubPages[seg[0]] {
		return nil, nil
	}
	repo := "https://api.github.com/repos/" + url.PathEscape(seg[0]) + "/" + url.PathEscape(strings.TrimSuffix(seg[1], ".git"))
	switch {
	case len(seg) == 2:
		return githubRepo(ctx, r, repo)
	case len(seg) >= 4 && (seg[2] == "issues" || seg[2] == "pull"):
		if n, err := strconv.Atoi(seg[3]); err == nil && n > 0 {
			return githubIssue(ctx, r, repo, n)
		}
	case len(seg) >= 5 && seg[2] == "blob":
		return githubFile(ctx, r, seg)
	}
	return nil, nil
}

func githubRepo(ctx context.Context, r *Reader, repo string) (*doc, error) {
	var v struct {
		FullName    string   `json:"full_name"`
		Description string   `json:"description"`
		Homepage    string   `json:"homepage"`
		Stars       int      `json:"stargazers_count"`
		Forks       int      `json:"forks_count"`
		Issues      int      `json:"open_issues_count"`
		Language    string   `json:"language"`
		Topics      []string `json:"topics"`
		Branch      string   `json:"default_branch"`
		Pushed      string   `json:"pushed_at"`
		Archived    bool     `json:"archived"`
		Fork        bool     `json:"fork"`
		HTMLURL     string   `json:"html_url"`
		License     *struct {
			ID string `json:"spdx_id"`
		} `json:"license"`
	}
	if !r.apiJSON(ctx, repo, r.githubHeaders("application/vnd.github+json"), &v) || v.FullName == "" {
		return nil, nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", v.FullName)
	if v.Description != "" {
		b.WriteString(v.Description + "\n\n")
	}
	facts := []string{fmt.Sprintf("Stars: %d", v.Stars), fmt.Sprintf("Forks: %d", v.Forks), fmt.Sprintf("Open issues and pull requests: %d", v.Issues)}
	if v.Language != "" {
		facts = append(facts, "Language: "+v.Language)
	}
	if v.License != nil && v.License.ID != "" && v.License.ID != "NOASSERTION" {
		facts = append(facts, "License: "+v.License.ID)
	}
	facts = append(facts, "Default branch: "+v.Branch)
	if len(v.Pushed) >= 10 {
		facts = append(facts, "Last push: "+v.Pushed[:10])
	}
	if v.Homepage != "" {
		facts = append(facts, "Homepage: "+v.Homepage)
	}
	if len(v.Topics) > 0 {
		facts = append(facts, "Topics: "+strings.Join(v.Topics, ", "))
	}
	if v.Archived {
		facts = append(facts, "Archived (read-only)")
	}
	if v.Fork {
		facts = append(facts, "A fork")
	}
	b.WriteString("- " + strings.Join(facts, "\n- ") + "\n")
	if resp, err := r.api(ctx, repo+"/readme", r.githubHeaders("application/vnd.github.raw+json")); err == nil && resp.status == http.StatusOK {
		b.WriteString("\n## README\n\n" + string(decode(resp.body, "")))
	}
	return &doc{url: v.HTMLURL, title: v.FullName, site: "GitHub", text: tidy(b.String())}, nil
}

type githubUser struct {
	Login string `json:"login"`
}

func githubIssue(ctx context.Context, r *Reader, repo string, n int) (*doc, error) {
	var v struct {
		Title    string     `json:"title"`
		State    string     `json:"state"`
		Reason   string     `json:"state_reason"`
		User     githubUser `json:"user"`
		Created  string     `json:"created_at"`
		Body     string     `json:"body"`
		Comments int        `json:"comments"`
		HTMLURL  string     `json:"html_url"`
		PR       *struct {
			Merged string `json:"merged_at"`
		} `json:"pull_request"`
		Labels []struct {
			Name string `json:"name"`
		} `json:"labels"`
	}
	h := r.githubHeaders("application/vnd.github+json")
	if !r.apiJSON(ctx, fmt.Sprintf("%s/issues/%d", repo, n), h, &v) || v.Title == "" {
		return nil, nil
	}
	what, state := "Issue", v.State
	if v.PR != nil {
		what = "Pull request"
		if v.PR.Merged != "" {
			state = "merged"
		}
	}
	if v.Reason != "" && v.Reason != "completed" && state == "closed" {
		state += " (" + strings.ReplaceAll(v.Reason, "_", " ") + ")"
	}
	facts := []string{what, state, "by " + v.User.Login}
	if len(v.Created) >= 10 {
		facts = append(facts, v.Created[:10])
	}
	var labels []string
	for _, l := range v.Labels {
		labels = append(labels, l.Name)
	}
	if len(labels) > 0 {
		facts = append(facts, "labels: "+strings.Join(labels, ", "))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s (#%d)\n\n%s\n\n%s\n", v.Title, n, strings.Join(facts, " · "), strings.TrimSpace(v.Body))
	if v.Comments > 0 {
		var cs []struct {
			User    githubUser `json:"user"`
			Created string     `json:"created_at"`
			Body    string     `json:"body"`
		}
		if r.apiJSON(ctx, fmt.Sprintf("%s/issues/%d/comments?per_page=100", repo, n), h, &cs) {
			fmt.Fprintf(&b, "\n## Comments (%d)\n", v.Comments)
			for _, c := range cs {
				when := c.Created
				if len(when) >= 10 {
					when = when[:10]
				}
				fmt.Fprintf(&b, "\n### %s · %s\n\n%s\n", c.User.Login, when, strings.TrimSpace(c.Body))
			}
			if v.Comments > len(cs) {
				fmt.Fprintf(&b, "\n(the first %d of %d comments)\n", len(cs), v.Comments)
			}
		}
	}
	return &doc{url: v.HTMLURL, title: v.Title, site: "GitHub", author: v.User.Login, date: day10(v.Created), text: tidy(b.String())}, nil
}

// githubFile reads a file's own bytes instead of the page that shows it.
func githubFile(ctx context.Context, r *Reader, seg []string) (*doc, error) {
	raw := "https://raw.githubusercontent.com/" + seg[0] + "/" + seg[1] + "/" + strings.Join(seg[3:], "/")
	resp, err := r.api(ctx, raw, nil)
	if err != nil || resp.status != http.StatusOK {
		return nil, nil
	}
	name := seg[len(seg)-1]
	kind := sniff(resp.contentType(), resp.body, name)
	if kind != "text" && kind != "markdown" && kind != "json" {
		return nil, nil
	}
	text := string(decode(resp.body, resp.header.Get("Content-Type")))
	if kind != "markdown" {
		ext := ""
		if i := strings.LastIndexByte(name, '.'); i >= 0 {
			ext = name[i+1:]
		}
		text = "```" + ext + "\n" + strings.TrimRight(text, "\n") + "\n```"
	}
	return &doc{title: strings.Join(seg[4:], "/"), site: "GitHub", kind: "github", text: tidy(text)}, nil
}

func day10(s string) string {
	if len(s) >= 10 {
		return s[:10]
	}
	return s
}

// --- arXiv ---

func readArxiv(ctx context.Context, r *Reader, u *url.URL, _ Request, _ Box) (*doc, error) {
	seg := segments(u)
	// The PDF itself is read as a document; only the abstract page has an
	// API worth asking.
	if len(seg) < 2 || seg[0] != "abs" {
		return nil, nil
	}
	id := strings.Join(seg[1:], "/")
	resp, err := r.api(ctx, "https://export.arxiv.org/api/query?id_list="+url.QueryEscape(id), nil)
	if err != nil || resp.status != http.StatusOK {
		return nil, nil
	}
	f, err := parseFeed(resp.body)
	if err != nil || len(f.Entries) == 0 {
		return nil, nil
	}
	e := f.Entries[0]
	if strings.TrimSpace(e.Summary) == "" || strings.Contains(e.ID, "api/errors") {
		return nil, nil
	}
	var authors, cats []string
	for _, a := range e.Authors {
		authors = append(authors, oneLine(a.Name))
	}
	for _, c := range e.Categories {
		cats = append(cats, c.Term)
	}
	pdf := "https://arxiv.org/pdf/" + id
	var b strings.Builder
	title := oneLine(e.Title)
	fmt.Fprintf(&b, "# %s\n\nAuthors: %s\n\n", title, strings.Join(authors, ", "))
	facts := []string{"Published: " + day(e.Published)}
	if day(e.Updated) != day(e.Published) {
		facts = append(facts, "Updated: "+day(e.Updated))
	}
	if len(cats) > 0 {
		facts = append(facts, "Categories: "+strings.Join(cats, ", "))
	}
	b.WriteString(strings.Join(facts, " · ") + "\n")
	if c := oneLine(e.Comment); c != "" {
		b.WriteString("\n" + c + "\n")
	}
	fmt.Fprintf(&b, "\n## Abstract\n\n%s\n\nThis is the abstract only. The whole paper: %s (read that address for its text).\n", oneLine(e.Summary), pdf)
	return &doc{
		url: "https://arxiv.org/abs/" + id, title: title, site: "arXiv", author: strings.Join(authors, ", "),
		date: day(e.Published), text: tidy(b.String()), data: map[string]any{"pdf_url": pdf},
	}, nil
}

// --- Apple Podcasts ---

var appleShow = regexp.MustCompile(`/id(\d+)`)

func readApplePodcast(ctx context.Context, r *Reader, u *url.URL, _ Request, _ Box) (*doc, error) {
	m := appleShow.FindStringSubmatch(u.Path)
	if m == nil || !strings.Contains(u.Path, "/podcast/") {
		return nil, nil
	}
	var v struct {
		Results []struct {
			Kind        string `json:"kind"`
			TrackID     int64  `json:"trackId"`
			Name        string `json:"trackName"`
			Show        string `json:"collectionName"`
			Artist      string `json:"artistName"`
			Feed        string `json:"feedUrl"`
			Description string `json:"description"`
			Released    string `json:"releaseDate"`
			Audio       string `json:"episodeUrl"`
			Millis      int64  `json:"trackTimeMillis"`
			Page        string `json:"trackViewUrl"`
		} `json:"results"`
	}
	lookup := "https://itunes.apple.com/lookup?id=" + m[1] + "&media=podcast&entity=podcastEpisode&limit=200"
	if !r.apiJSON(ctx, lookup, nil, &v) || len(v.Results) == 0 {
		return nil, nil
	}
	show := v.Results[0]
	d := &doc{site: "Apple Podcasts", author: show.Artist, data: map[string]any{}}
	if show.Feed != "" {
		d.data["feed_url"] = show.Feed
	}
	var b strings.Builder
	want, _ := strconv.ParseInt(u.Query().Get("i"), 10, 64)
	for _, e := range v.Results[1:] {
		if want == 0 || e.TrackID != want {
			continue
		}
		d.title, d.date = e.Name, day10(e.Released)
		facts := []string{"Podcast: " + first(e.Show, show.Show, show.Name), "Published: " + day10(e.Released)}
		if e.Millis > 0 {
			facts = append(facts, "Length: "+clock(time.Duration(e.Millis)*time.Millisecond))
		}
		fmt.Fprintf(&b, "# %s\n\n%s\n\n## Show notes\n\n%s\n", e.Name, strings.Join(facts, " · "), strings.TrimSpace(e.Description))
		if e.Audio != "" {
			fmt.Fprintf(&b, "\nAudio: %s\n", e.Audio)
			d.data["audio_url"] = e.Audio
			d.note = "These are the episode's notes, not what is said in it. For the words, download `data.audio_url` into the workspace in exec_python and run silo_runtime.transcribe on it (25 MB a call: cut a longer file into pieces with ffmpeg first)."
		}
		d.text = tidy(b.String())
		return d, nil
	}
	// The show, or an episode too old to be among the newest listed.
	d.title = first(show.Show, show.Name)
	fmt.Fprintf(&b, "# %s\n\nBy %s\n\n## Episodes\n\n", d.title, show.Artist)
	for i, e := range v.Results[1:] {
		if i >= 30 {
			break
		}
		fmt.Fprintf(&b, "- **%s** (%s) %s\n", oneLine(e.Name), day10(e.Released), e.Page)
	}
	if want != 0 {
		d.note = "That episode is not among the show's newest 200; this is the show. Its feed (`data.feed_url`) lists every episode with its audio."
	}
	d.text = tidy(b.String())
	return d, nil
}

// clock is a duration as h:mm:ss or m:ss.
func clock(d time.Duration) string {
	s := int(d.Seconds())
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s%3600/60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}
