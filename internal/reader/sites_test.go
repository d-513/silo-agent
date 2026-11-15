package reader

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// answers is a fake network: what each address answers, by URL. Every
// request is recorded, and an address with no answer is a 404.
type answers struct {
	t     *testing.T
	pages map[string]string
	types map[string]string
	asked []*fetchReq
}

func (a *answers) send(_ context.Context, rq *fetchReq) (*response, error) {
	a.asked = append(a.asked, rq)
	u, err := url.Parse(rq.url)
	if err != nil {
		return nil, err
	}
	body, ok := a.pages[rq.url]
	h := http.Header{}
	ct := a.types[rq.url]
	if ct == "" {
		ct = "application/json"
	}
	h.Set("Content-Type", ct)
	if !ok {
		return &response{status: 404, url: u, header: h, body: []byte(`{"message":"Not Found"}`)}, nil
	}
	return &response{status: 200, url: u, header: h, body: []byte(body)}, nil
}

func (a *answers) header(link, name string) string {
	for _, rq := range a.asked {
		if rq.url == link {
			return rq.headers[name]
		}
	}
	a.t.Fatalf("%s was never asked", link)
	return ""
}

func faked(t *testing.T, opts Options, pages map[string]string) (*Reader, *answers) {
	a := &answers{t: t, pages: pages, types: map[string]string{}}
	r := New(opts)
	r.send = a.send
	return r, a
}

func mustRead(t *testing.T, r *Reader, rq Request) Page {
	t.Helper()
	if rq.MaxChars == 0 {
		rq.MaxChars = 50000
	}
	p, err := r.Read(context.Background(), rq, nil)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestGitHubRepoIsReadThroughTheAPI(t *testing.T) {
	r, a := faked(t, Options{GitHubToken: func() string { return "ghp_secret" }}, map[string]string{
		"https://api.github.com/repos/acme/anvil": `{"full_name":"acme/anvil","description":"Drops on demand","stargazers_count":1234,"forks_count":56,
			"open_issues_count":7,"language":"Go","topics":["gravity","tools"],"default_branch":"main","pushed_at":"2026-10-30T12:00:00Z",
			"html_url":"https://github.com/acme/anvil","license":{"spdx_id":"MIT"},"archived":true}`,
		"https://api.github.com/repos/acme/anvil/readme": "# Anvil\n\nInstall with `go install`.\n",
	})
	p := mustRead(t, r, Request{URL: "https://github.com/acme/anvil"})
	if p.Kind != "github" || p.Via != "api" || p.Title != "acme/anvil" || p.URL != "https://github.com/acme/anvil" {
		t.Fatalf("page %+v", p)
	}
	for _, want := range []string{"Drops on demand", "Stars: 1234", "License: MIT", "Topics: gravity, tools", "Last push: 2026-10-30", "Archived", "## README\n\n# Anvil", "Install with `go install`."} {
		if !strings.Contains(p.Text, want) {
			t.Fatalf("text lacks %q:\n%s", want, p.Text)
		}
	}
	if got := a.header("https://api.github.com/repos/acme/anvil", "Authorization"); got != "Bearer ghp_secret" {
		t.Fatalf("authorization %q", got)
	}
	if got := a.header("https://api.github.com/repos/acme/anvil/readme", "Accept"); got != "application/vnd.github.raw+json" {
		t.Fatalf("readme accept %q", got)
	}
	for _, rq := range a.asked {
		if !rq.api {
			t.Fatalf("%s was asked as a browser", rq.url)
		}
	}
}

func TestGitHubIssueWithComments(t *testing.T) {
	r, _ := faked(t, Options{}, map[string]string{
		"https://api.github.com/repos/acme/anvil/issues/42": `{"title":"Anvil floats","state":"closed","state_reason":"not_planned","user":{"login":"wile"},
			"created_at":"2026-09-01T08:00:00Z","body":"It does not fall.","comments":2,"html_url":"https://github.com/acme/anvil/pull/42",
			"pull_request":{"merged_at":null},"labels":[{"name":"bug"},{"name":"physics"}]}`,
		"https://api.github.com/repos/acme/anvil/issues/42/comments?per_page=100": `[{"user":{"login":"road"},"created_at":"2026-09-02T09:00:00Z","body":"Meep meep."},
			{"user":{"login":"wile"},"created_at":"2026-09-03T09:00:00Z","body":"Closing."}]`,
	})
	p := mustRead(t, r, Request{URL: "https://github.com/acme/anvil/pull/42/files"})
	want := "# Anvil floats (#42)\n\nPull request · closed (not planned) · by wile · 2026-09-01 · labels: bug, physics\n\nIt does not fall.\n\n## Comments (2)\n\n### road · 2026-09-02\n\nMeep meep.\n\n### wile · 2026-09-03\n\nClosing."
	if p.Text != want || p.Author != "wile" || p.Date != "2026-09-01" {
		t.Fatalf("issue:\n%s\n%+v", p.Text, p)
	}
}

func TestGitHubFileIsItsRawBytes(t *testing.T) {
	r, a := faked(t, Options{}, map[string]string{
		"https://raw.githubusercontent.com/acme/anvil/main/cmd/drop.go": "package main\n",
	})
	a.types["https://raw.githubusercontent.com/acme/anvil/main/cmd/drop.go"] = "text/plain; charset=utf-8"
	p := mustRead(t, r, Request{URL: "https://github.com/acme/anvil/blob/main/cmd/drop.go"})
	if p.Text != "```go\npackage main\n```" || p.Title != "cmd/drop.go" {
		t.Fatalf("file %+v", p)
	}
}

// What the API will not answer (a private repository, a spent limit, a page
// that is not a repository) is read as the page it is.
func TestGitHubFallsBackToThePage(t *testing.T) {
	r, a := faked(t, Options{}, map[string]string{
		"https://github.com/acme/secret":    "<html><head><title>Page not found · GitHub</title></head><body><main><p>" + para + "</p></main></body></html>",
		"https://github.com/topics/gravity": "<html><head><title>gravity · GitHub Topics</title></head><body><main><p>" + para + "</p></main></body></html>",
	})
	a.types["https://github.com/acme/secret"] = "text/html"
	a.types["https://github.com/topics/gravity"] = "text/html"
	if p := mustRead(t, r, Request{URL: "https://github.com/acme/secret"}); p.Via != "static" || p.Title != "Page not found · GitHub" {
		t.Fatalf("private repo %+v", p)
	}
	before := len(a.asked)
	if p := mustRead(t, r, Request{URL: "https://github.com/topics/gravity"}); p.Via != "static" || len(a.asked) != before+1 {
		t.Fatalf("topics page %+v (asked %d)", p, len(a.asked)-before)
	}
}

func TestArxivAbstract(t *testing.T) {
	feed := `<?xml version="1.0" encoding="UTF-8"?><feed xmlns="http://www.w3.org/2005/Atom" xmlns:arxiv="http://arxiv.org/schemas/atom">
<entry><id>http://arxiv.org/abs/2601.01234v2</id><updated>2026-02-01T00:00:00Z</updated><published>2026-01-05T00:00:00Z</published>
<title>On the Falling
  of Anvils</title><summary>  We show that anvils fall.
  Mostly. </summary><author><name>W. E. Coyote</name></author><author><name>R. Runner</name></author>
<arxiv:comment>12 pages</arxiv:comment><category term="physics.pop-ph"/><category term="cs.RO"/></entry></feed>`
	r, a := faked(t, Options{}, map[string]string{"https://export.arxiv.org/api/query?id_list=2601.01234v2": feed})
	a.types["https://export.arxiv.org/api/query?id_list=2601.01234v2"] = "application/atom+xml"
	p := mustRead(t, r, Request{URL: "https://arxiv.org/abs/2601.01234v2"})
	want := "# On the Falling of Anvils\n\nAuthors: W. E. Coyote, R. Runner\n\nPublished: 2026-01-05 · Updated: 2026-02-01 · Categories: physics.pop-ph, cs.RO\n\n12 pages\n\n## Abstract\n\nWe show that anvils fall. Mostly.\n\nThis is the abstract only. The whole paper: https://arxiv.org/pdf/2601.01234v2 (read that address for its text)."
	if p.Text != want || p.Kind != "arxiv" || p.Data["pdf_url"] != "https://arxiv.org/pdf/2601.01234v2" {
		t.Fatalf("abstract:\n%s\n%+v", p.Text, p)
	}
	// The PDF is not the handler's: it is read as the document it is.
	box := &fakeBox{docText: "On the Falling of Anvils\nfull text"}
	a.pages["https://arxiv.org/pdf/2601.01234v2"] = "%PDF-1.5"
	a.types["https://arxiv.org/pdf/2601.01234v2"] = "application/pdf"
	q, err := r.Read(context.Background(), Request{URL: "https://arxiv.org/pdf/2601.01234v2"}, box)
	if err != nil || q.Kind != "pdf" || !strings.Contains(q.Text, "full text") {
		t.Fatalf("pdf: %v %+v", err, q)
	}
}

const ytWatch = "https://www.youtube.com/watch?v=dQw4w9WgXcQ"

func youtubeAnswers() map[string]string {
	return map[string]string{
		ytWatch + "&hl=en": `<html><script>var x = {"INNERTUBE_API_KEY":"KEY123"};var ytInitialPlayerResponse = {"videoDetails":{"title":"Page title"},"microformat":{"playerMicroformatRenderer":{"publishDate":"2009-10-25T06:57:33-07:00"}}};</script></html>`,
		"https://www.youtube.com/youtubei/v1/player?prettyPrint=false&key=KEY123": `{"playabilityStatus":{"status":"OK"},
			"videoDetails":{"title":"Never Gonna Give You Up","author":"Rick Astley","lengthSeconds":"213","shortDescription":"The official video.","viewCount":"1600000000"},
			"captions":{"playerCaptionsTracklistRenderer":{"captionTracks":[
				{"baseUrl":"https://www.youtube.com/api/timedtext?v=x&lang=en&kind=asr","languageCode":"en","kind":"asr","name":{"runs":[{"text":"English (auto-generated)"}]}},
				{"baseUrl":"https://www.youtube.com/api/timedtext?v=x&lang=en","languageCode":"en","name":{"simpleText":"English"}},
				{"baseUrl":"https://www.youtube.com/api/timedtext?v=x&lang=pl","languageCode":"pl","name":{"simpleText":"Polish"}}]}}}`,
		"https://www.youtube.com/api/timedtext?v=x&lang=en": `<?xml version="1.0" encoding="utf-8" ?><transcript>
			<text start="18.6" dur="3.2">We&amp;#39;re no strangers to love</text><text start="22.4" dur="4">You know the rules
and so do I</text><text start="70.1" dur="2">Never gonna give you up</text></transcript>`,
		"https://www.youtube.com/api/timedtext?v=x&lang=pl": `<timedtext format="3"><body><p t="18600" d="3200"><s>Nie jesteśmy</s><s t="400"> obcy</s></p></body></timedtext>`,
	}
}

func TestYouTubeIsItsDescriptionAndCaptions(t *testing.T) {
	r, a := faked(t, Options{}, youtubeAnswers())
	p := mustRead(t, r, Request{URL: "https://youtu.be/dQw4w9WgXcQ?t=10"})
	want := "# Never Gonna Give You Up\n\nChannel: Rick Astley · Length: 3:33 · Views: 1600000000 · Published: 2009-10-25\n\n## Description\n\nThe official video.\n\n" +
		"## Transcript (English)\n\n[0:18] We're no strangers to love You know the rules and so do I\n\n[1:10] Never gonna give you up"
	if p.Text != want || p.Kind != "youtube" || p.URL != ytWatch || p.Note != "" {
		t.Fatalf("video:\n%s\n%+v", p.Text, p)
	}
	if langs, _ := p.Data["caption_languages"].([]string); strings.Join(langs, ",") != "en,en,pl" {
		t.Fatalf("languages %v", p.Data)
	}
	// The player is asked as the Android app, with the page's key.
	if got := a.header("https://www.youtube.com/youtubei/v1/player?prettyPrint=false&key=KEY123", "User-Agent"); !strings.HasPrefix(got, "com.google.android.youtube/") {
		t.Fatalf("player asked as %q", got)
	}

	// The language asked for wins, in the other caption format too.
	p = mustRead(t, r, Request{URL: ytWatch, Language: "pl"})
	if !strings.Contains(p.Text, "## Transcript (Polish)\n\n[0:18] Nie jesteśmy obcy") {
		t.Fatalf("polish captions:\n%s", p.Text)
	}
}

func TestYouTubeWithoutCaptionsSaysSo(t *testing.T) {
	pages := youtubeAnswers()
	pages["https://www.youtube.com/youtubei/v1/player?prettyPrint=false&key=KEY123"] = `{"playabilityStatus":{"status":"LOGIN_REQUIRED","reason":"Sign in to confirm you’re not a bot"}}`
	pages["https://www.youtube.com/oembed?format=json&url=https%3A%2F%2Fwww.youtube.com%2Fwatch%3Fv%3DdQw4w9WgXcQ"] = `{"title":"From oEmbed","author_name":"Rick"}`
	r, _ := faked(t, Options{}, pages)
	p := mustRead(t, r, Request{URL: ytWatch})
	if p.Title != "Page title" || !strings.Contains(p.Note, "Sign in to confirm") || strings.Contains(p.Text, "Transcript") || p.Date != "2009-10-25" {
		t.Fatalf("refused player %+v", p)
	}
	// With neither the page nor the player, oEmbed still names the video.
	delete(pages, ytWatch+"&hl=en")
	r, _ = faked(t, Options{}, pages)
	if p := mustRead(t, r, Request{URL: ytWatch}); p.Title != "From oEmbed" || p.Author != "Rick" {
		t.Fatalf("oembed %+v", p)
	}
	for _, link := range []string{"https://www.youtube.com/@rick", "https://www.youtube.com/watch?v=short", "https://example.com/watch?v=dQw4w9WgXcQ"} {
		u, _ := url.Parse(link)
		if videoID(u) != "" {
			t.Fatalf("%s read as a video", link)
		}
	}
}

func TestApplePodcastEpisode(t *testing.T) {
	lookup := "https://itunes.apple.com/lookup?id=1200361736&media=podcast&entity=podcastEpisode&limit=200"
	r, _ := faked(t, Options{}, map[string]string{lookup: `{"results":[
		{"kind":"podcast","collectionName":"Dock Talk","artistName":"Harbour Radio","feedUrl":"https://feeds.example.com/dock"},
		{"kind":"podcast-episode","trackId":1000,"trackName":"Fog","collectionName":"Dock Talk","description":"On fog signals.","releaseDate":"2026-11-02T08:00:00Z",
		 "episodeUrl":"https://cdn.example.com/9.mp3","trackTimeMillis":3723000,"trackViewUrl":"https://podcasts.apple.com/us/podcast/fog/id1200361736?i=1000"},
		{"kind":"podcast-episode","trackId":999,"trackName":"Tides","releaseDate":"2026-10-26T08:00:00Z","trackViewUrl":"https://podcasts.apple.com/us/podcast/tides/id1200361736?i=999"}]}`})
	p := mustRead(t, r, Request{URL: "https://podcasts.apple.com/us/podcast/fog/id1200361736?i=1000"})
	want := "# Fog\n\nPodcast: Dock Talk · Published: 2026-11-02 · Length: 1:02:03\n\n## Show notes\n\nOn fog signals.\n\nAudio: https://cdn.example.com/9.mp3"
	if p.Text != want || p.Kind != "podcast" || p.Data["audio_url"] != "https://cdn.example.com/9.mp3" || p.Data["feed_url"] != "https://feeds.example.com/dock" || !strings.Contains(p.Note, "transcribe") {
		t.Fatalf("episode:\n%s\n%+v", p.Text, p)
	}
	// The show's own address lists its episodes.
	p = mustRead(t, r, Request{URL: "https://podcasts.apple.com/us/podcast/dock-talk/id1200361736"})
	if p.Title != "Dock Talk" || !strings.Contains(p.Text, "- **Tides** (2026-10-26) https://podcasts.apple.com/us/podcast/tides/id1200361736?i=999") {
		t.Fatalf("show:\n%s", p.Text)
	}
}
