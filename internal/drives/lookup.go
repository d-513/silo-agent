package drives

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// lookupLimit caps a provider response; a drive list is a few KB.
const lookupLimit = 2 << 20

// call runs one lookup with the drive's bearer token and decodes the JSON.
func (t *Template) call(ctx context.Context, hc *http.Client, v Values, l *Lookup) (any, error) {
	tok, err := ParseToken(t.Value(v, KindDynamic, "token"))
	if err != nil {
		return nil, &MissingError{Missing: []Missing{{Kind: KindDynamic, Key: "token"}}}
	}
	method := strings.ToUpper(strings.TrimSpace(l.Method))
	if method == "" {
		method = http.MethodGet
	}
	var body io.Reader
	if l.Body != "" {
		body = strings.NewReader(t.Expand(l.Body, v))
	}
	req, err := http.NewRequestWithContext(ctx, method, t.Expand(l.URL, v), body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, h := range l.Headers {
		req.Header.Set(k, t.Expand(h, v))
	}
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, lookupLimit))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, &MissingError{Missing: []Missing{{Kind: KindDynamic, Key: "token"}}}
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("%s answered %d: %s", t.Title, resp.StatusCode, clip(string(raw), 200))
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("%s answered with something that is not JSON", t.Title)
	}
	return out, nil
}

// Dig reads a dot path ("user.emailAddress", "value.0.id") out of decoded JSON.
func Dig(doc any, path string) (any, bool) {
	cur := doc
	if path == "" || path == "." {
		return cur, true
	}
	for part := range strings.SplitSeq(path, ".") {
		switch n := cur.(type) {
		case map[string]any:
			v, ok := n[part]
			if !ok {
				return nil, false
			}
			cur = v
		case []any:
			i, err := strconv.Atoi(part)
			if err != nil || i < 0 || i >= len(n) {
				return nil, false
			}
			cur = n[i]
		default:
			return nil, false
		}
	}
	return cur, true
}

// DigString is Dig rendered as a string; missing and null are "".
func DigString(doc any, path string) string {
	v, ok := Dig(doc, path)
	if !ok || v == nil {
		return ""
	}
	switch s := v.(type) {
	case string:
		return s
	case float64:
		return strconv.FormatFloat(s, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(s)
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// Resolve runs every lookup-sourced dynamic var (the account label, a default
// drive id) and returns the values it found. A lookup that finds nothing is
// skipped rather than failing the whole sign-in.
func (t *Template) Resolve(ctx context.Context, hc *http.Client, v Values) (map[string]string, error) {
	out := map[string]string{}
	for _, d := range t.VarsOf(KindDynamic) {
		if d.Source != SourceLookup {
			continue
		}
		doc, err := t.call(ctx, hc, v, d.Lookup)
		if err != nil {
			return out, err
		}
		if s := DigString(doc, d.Lookup.Value); s != "" {
			out[d.Key] = s
		}
	}
	return out, nil
}

// Pick lists the options of a pick var from the provider.
func (t *Template) Pick(ctx context.Context, hc *http.Client, v Values, key string) ([]Option, error) {
	d, ok := t.Var(KindUser, key)
	if !ok || d.Type != TypePick {
		return nil, errors.New("no pick field " + key)
	}
	doc, err := t.call(ctx, hc, v, d.Lookup)
	if err != nil {
		return nil, err
	}
	items, ok := Dig(doc, d.Lookup.Items)
	if !ok {
		return nil, nil
	}
	list, ok := items.([]any)
	if !ok {
		list = []any{items}
	}
	out := make([]Option, 0, len(list))
	for _, it := range list {
		o := Option{Value: DigString(it, d.Lookup.Value)}
		if o.Value == "" {
			continue
		}
		o.Label = DigString(it, d.Lookup.Label)
		if o.Label == "" {
			o.Label = o.Value
		}
		o.Detail = DigString(it, d.Lookup.Detail)
		for k, p := range d.Lookup.Extra {
			if o.Extra == nil {
				o.Extra = map[string]string{}
			}
			o.Extra[k] = DigString(it, p)
		}
		out = append(out, o)
	}
	return out, nil
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if len([]rune(s)) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}
