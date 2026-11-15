package reader

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// A video is read as its description and its captions. YouTube's player API
// is asked the way its Android app asks it: that answer carries caption
// addresses that can be fetched as they are, which the web player's no longer
// can. It is an unofficial door and YouTube refuses it from addresses it does
// not like; then the page's own data and oEmbed still give the title and the
// description, and the answer says the captions could not be had.
const (
	androidVersion = "20.10.38"
	androidAgent   = "com.google.android.youtube/" + androidVersion + " (Linux; U; Android 14)"
	// captionStep is about how many seconds of speech one paragraph of a
	// transcript holds.
	captionStep = 45
)

var (
	videoIDForm  = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)
	innertubeKey = regexp.MustCompile(`"INNERTUBE_API_KEY":"([^"]+)"`)
)

// videoID is the video an address names, or "".
func videoID(u *url.URL) string {
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	seg := segments(u)
	id := ""
	switch host {
	case "youtu.be":
		if len(seg) > 0 {
			id = seg[0]
		}
	case "youtube.com", "m.youtube.com", "music.youtube.com":
		switch {
		case len(seg) == 1 && seg[0] == "watch":
			id = u.Query().Get("v")
		case len(seg) >= 2 && (seg[0] == "shorts" || seg[0] == "live" || seg[0] == "embed"):
			id = seg[1]
		}
	}
	if !videoIDForm.MatchString(id) {
		return ""
	}
	return id
}

type ytPlayer struct {
	Playability struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	} `json:"playabilityStatus"`
	Details struct {
		Title   string `json:"title"`
		Author  string `json:"author"`
		Seconds string `json:"lengthSeconds"`
		About   string `json:"shortDescription"`
		Views   string `json:"viewCount"`
		Live    bool   `json:"isLiveContent"`
	} `json:"videoDetails"`
	Captions struct {
		List struct {
			Tracks []ytTrack `json:"captionTracks"`
		} `json:"playerCaptionsTracklistRenderer"`
	} `json:"captions"`
	Micro struct {
		R struct {
			Published string `json:"publishDate"`
		} `json:"playerMicroformatRenderer"`
	} `json:"microformat"`
}

type ytTrack struct {
	URL  string `json:"baseUrl"`
	Lang string `json:"languageCode"`
	// Kind is "asr" for captions YouTube made by itself.
	Kind string `json:"kind"`
	Name struct {
		Text string `json:"simpleText"`
		Runs []struct {
			Text string `json:"text"`
		} `json:"runs"`
	} `json:"name"`
}

func (t ytTrack) label() string {
	name := t.Name.Text
	for _, r := range t.Name.Runs {
		name += r.Text
	}
	return first(name, t.Lang)
}

func readYouTube(ctx context.Context, r *Reader, u *url.URL, rq Request, _ Box) (*doc, error) {
	id := videoID(u)
	watch := "https://www.youtube.com/watch?v=" + id
	var page, app ytPlayer
	key := ""
	// The consent cookies keep a European address from getting the cookie
	// notice instead of the video's page.
	if resp, err := r.send(ctx, &fetchReq{url: watch + "&hl=en", lang: rq.Language, headers: map[string]string{"cookie": "SOCS=CAI; CONSENT=YES+1"}}); err == nil && resp.status == http.StatusOK {
		if m := innertubeKey.FindSubmatch(resp.body); m != nil {
			key = string(m[1])
		}
		if i := bytes.Index(resp.body, []byte("ytInitialPlayerResponse = ")); i >= 0 {
			_ = json.NewDecoder(bytes.NewReader(resp.body[i+len("ytInitialPlayerResponse = "):])).Decode(&page)
		}
	}
	body, _ := json.Marshal(map[string]any{
		"context": map[string]any{"client": map[string]any{"clientName": "ANDROID", "clientVersion": androidVersion}},
		"videoId": id,
	})
	api := "https://www.youtube.com/youtubei/v1/player?prettyPrint=false"
	if key != "" {
		api += "&key=" + url.QueryEscape(key)
	}
	if resp, err := r.send(ctx, &fetchReq{method: http.MethodPost, url: api, body: body, api: true, headers: map[string]string{
		"Content-Type": "application/json", "User-Agent": androidAgent,
	}}); err == nil && resp.status == http.StatusOK {
		_ = json.Unmarshal(resp.body, &app)
	}

	v := app
	if v.Details.Title == "" {
		v.Details, v.Micro = page.Details, page.Micro
	}
	if v.Micro.R.Published == "" {
		v.Micro = page.Micro
	}
	if v.Details.Title == "" {
		var o struct {
			Title  string `json:"title"`
			Author string `json:"author_name"`
		}
		if !r.apiJSON(ctx, "https://www.youtube.com/oembed?format=json&url="+url.QueryEscape(watch), nil, &o) || o.Title == "" {
			return nil, nil
		}
		v.Details.Title, v.Details.Author = o.Title, o.Author
	}

	d := &doc{url: watch, title: v.Details.Title, site: "YouTube", author: v.Details.Author, date: day10(v.Micro.R.Published), data: map[string]any{"video_id": id}}
	facts := []string{"Channel: " + v.Details.Author}
	if secs, _ := strconv.Atoi(v.Details.Seconds); secs > 0 {
		facts = append(facts, "Length: "+clock(time.Duration(secs)*time.Second))
	}
	if v.Details.Views != "" {
		facts = append(facts, "Views: "+v.Details.Views)
	}
	if d.date != "" {
		facts = append(facts, "Published: "+d.date)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n%s\n", v.Details.Title, strings.Join(facts, " · "))
	if about := strings.TrimSpace(v.Details.About); about != "" {
		b.WriteString("\n## Description\n\n" + about + "\n")
	}

	tracks := app.Captions.List.Tracks
	var langs []string
	for _, t := range tracks {
		langs = append(langs, t.Lang)
	}
	if len(langs) > 0 {
		d.data["caption_languages"] = langs
	}
	track, ok := pickTrack(tracks, rq.Language)
	words := ""
	if ok {
		if resp, err := r.send(ctx, &fetchReq{url: track.URL, api: true, headers: map[string]string{"User-Agent": androidAgent}}); err == nil && resp.status == http.StatusOK {
			words = transcript(resp.body)
		}
	}
	switch {
	case words != "":
		by := ""
		if track.Kind == "asr" {
			by = ", made automatically: names and terms may be misheard"
		}
		fmt.Fprintf(&b, "\n## Transcript (%s%s)\n\n%s\n", track.label(), by, words)
	case ok:
		d.note = "The video has captions, but YouTube would not hand them over to this server. Only its description could be read."
	case app.Playability.Status != "" && app.Playability.Status != "OK":
		d.note = "YouTube would not give this server the video's captions (" + first(app.Playability.Reason, app.Playability.Status) + "). Only its description could be read."
	default:
		d.note = "This video has no captions. Only its description could be read."
	}
	d.text = tidy(b.String())
	return d, nil
}

// pickTrack chooses the captions to read: the language asked for, else
// English, else whatever there is; a person's captions before YouTube's own.
func pickTrack(tracks []ytTrack, lang string) (ytTrack, bool) {
	lang = strings.ToLower(strings.TrimSpace(lang))
	best, score := ytTrack{}, -1
	for _, t := range tracks {
		if t.URL == "" {
			continue
		}
		s := 0
		code := strings.ToLower(t.Lang)
		switch {
		case lang != "" && (code == lang || strings.HasPrefix(code, lang+"-")):
			s = 4
		case code == "en" || strings.HasPrefix(code, "en-"):
			s = 2
		}
		if t.Kind != "asr" {
			s++
		}
		if s > score {
			best, score = t, s
		}
	}
	return best, score >= 0
}

// transcript reads a caption file, in either of the two XML shapes YouTube
// serves (<text start= dur=> in seconds, <p t= d=> in milliseconds), into
// paragraphs that each open with the time they start at.
func transcript(body []byte) string {
	dec := xml.NewDecoder(bytes.NewReader(body))
	dec.Strict = false
	var b strings.Builder
	var line strings.Builder
	paraStart, lineStart := -1.0, 0.0
	depth := 0
	flush := func() {
		text := strings.Join(strings.Fields(html.UnescapeString(line.String())), " ")
		line.Reset()
		if text == "" {
			return
		}
		if paraStart < 0 || lineStart-paraStart >= captionStep {
			if b.Len() > 0 {
				b.WriteString("\n\n")
			}
			b.WriteString("[" + clock(time.Duration(lineStart*float64(time.Second))) + "] ")
			paraStart = lineStart
		} else {
			b.WriteByte(' ')
		}
		b.WriteString(text)
	}
	for {
		tok, err := dec.Token()
		if err == io.EOF || err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if depth > 0 {
				depth++
				continue
			}
			if t.Name.Local != "text" && t.Name.Local != "p" {
				continue
			}
			depth = 1
			for _, a := range t.Attr {
				switch a.Name.Local {
				case "start":
					lineStart, _ = strconv.ParseFloat(a.Value, 64)
				case "t":
					ms, _ := strconv.ParseFloat(a.Value, 64)
					lineStart = ms / 1000
				}
			}
		case xml.CharData:
			if depth > 0 {
				line.Write(t)
			}
		case xml.EndElement:
			if depth > 0 {
				if depth--; depth == 0 {
					flush()
				}
			}
		}
	}
	return b.String()
}
