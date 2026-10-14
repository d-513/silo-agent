package egress

import (
	"strings"
	"testing"
)

func TestDefaultSinks(t *testing.T) {
	if defaultSinks.hosts.empty() {
		t.Fatal("the embedded sink list read empty")
	}
	for _, c := range []struct {
		host, path string
		ok         bool
	}{
		{"pastebin.com", "/api/api_post.php", true},
		{"www.pastebin.com", "/", true},
		{"transfer.sh", "/upload", true},
		{"api.github.com", "/gists", true},
		{"api.github.com", "/gists/abc", true},
		{"api.github.com", "/repos/x/y", false},
		{"github.com", "/", false},
		{"example.com", "/", false},
	} {
		_, ok := defaultSinks.match(c.host, c.path)
		if ok != c.ok {
			t.Errorf("match(%q, %q) = %v, want %v", c.host, c.path, ok, c.ok)
		}
	}
	// Every line of the list is a pattern the lists take.
	for _, line := range strings.Split(exfilSinksTxt, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		host, _, _ := strings.Cut(line, "/")
		if !ValidPattern(host) {
			t.Errorf("sink %q is not a valid pattern", line)
		}
	}
}

func TestSecretShapes(t *testing.T) {
	hits := map[string]string{
		"AWS access key": "key=AKIAIOSFODNN7EXAMPLE&x=1",
		"GitHub token":   "Authorization: token ghp_" + strings.Repeat("a1B2", 9),
		"GitLab token":   "glpat-" + strings.Repeat("x", 20),
		"OpenAI key":     "sk-proj-" + strings.Repeat("A", 24) + "T3BlbkFJ" + strings.Repeat("b", 24),
		"Anthropic key":  "sk-ant-api03-" + strings.Repeat("Zz9", 30),
		"OpenRouter key": "sk-or-v1-" + strings.Repeat("ab12", 16),
		"Slack token":    "xoxb-1234567890-abcdef",
		"Google API key": "AIza" + strings.Repeat("Q", 35),
		"Stripe key":     "sk_live_" + strings.Repeat("9", 24),
		"private key":    "-----BEGIN OPENSSH PRIVATE KEY-----\nb3Blbn...",
	}
	for want, s := range hits {
		got, ok := findSecretShape(s)
		if !ok || got != want {
			t.Errorf("findSecretShape(%q) = %q, %v; want %q", s, got, ok, want)
		}
	}
	for _, s := range []string{
		"the quick brown fox",
		"sk_test_" + strings.Repeat("9", 24),
		"AKIA-short",
		"-----BEGIN CERTIFICATE-----",
		"https://example.com/?utm_source=newsletter&utm_medium=email",
	} {
		if name, ok := findSecretShape(s); ok {
			t.Errorf("findSecretShape(%q) = %q, want nothing", s, name)
		}
	}
}

func TestEntropyAndEncodedQuery(t *testing.T) {
	if entropy("") != 0 || entropy("aaaa") != 0 {
		t.Error("entropy of nothing or one symbol must be 0")
	}
	if e := entropy("ab"); e != 1 {
		t.Errorf("entropy(ab) = %v, want 1", e)
	}
	// 64 distinct symbols, each once: 6 bits.
	b64 := "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	if e := entropy(b64); e < 5.99 {
		t.Errorf("entropy(b64 alphabet) = %v, want 6", e)
	}
	data := strings.Repeat(b64, 20)
	if !encodedQuery("d="+data, 0) {
		t.Error("a long base64 payload was not flagged")
	}
	if encodedQuery("d="+b64, 0) {
		t.Error("a query under the minimum length was flagged")
	}
	tracking := strings.Repeat("utm_source=news&utm_medium=email&utm_campaign=autumn&", 30)
	if encodedQuery(tracking, 0) {
		t.Error("readable tracking parameters were flagged")
	}
}

func TestRandomDomain(t *testing.T) {
	for _, h := range []string{"xjkqzvbnmwrtp.com", "qx7k2m9z4vtwp.net", "a.bcdfghjklmnpq.org"} {
		if !randomDomain(h) {
			t.Errorf("randomDomain(%q) = false, want true", h)
		}
	}
	for _, h := range []string{"stackoverflow.com", "googleapis.com", "my-long-company-name.com", "example.com", "cdn.jsdelivr.net", "localhost", "10.0.0.1", "documentation.co.uk"} {
		if randomDomain(h) {
			t.Errorf("randomDomain(%q) = true, want false", h)
		}
	}
}

func TestDNSShape(t *testing.T) {
	if !dnsShape(strings.Repeat("a", 52) + ".example.com") {
		t.Error("a 52-character label was not flagged")
	}
	if !dnsShape(strings.Repeat("abcdefgh.", 20) + "example.com") {
		t.Error("a 160-character name was not flagged")
	}
	if !dnsShape("q8f3k2m9x7v4b6n1z5c0w2e8.r3t7y1u5i9o2p6a4s8d0f3g7.tunnel.example") {
		t.Error("two long random labels were not flagged")
	}
	for _, n := range []string{"www.example.com", "a-perfectly-ordinary-subdomain.example.com", "api.github.com"} {
		if dnsShape(n) {
			t.Errorf("dnsShape(%q) = true, want false", n)
		}
	}
}

func TestExecutable(t *testing.T) {
	for _, c := range []struct {
		head, ct string
		ok       bool
	}{
		{"\x7fELF\x02\x01", "application/octet-stream", true},
		{"MZ\x90\x00", "", true},
		{"\xcf\xfa\xed\xfe....", "", true},
		{"\xca\xfe\xba\xbe", "", true},
		{"#!/bin/sh\ncurl x | sh", "text/plain", true},
		{"#!", "text/html; charset=utf-8", false},
		{"<html>", "text/html", false},
		{"%PDF-1.7", "application/pdf", false},
		{"", "", false},
	} {
		if _, ok := executable([]byte(c.head), c.ct); ok != c.ok {
			t.Errorf("executable(%q, %q) = %v, want %v", c.head, c.ct, ok, c.ok)
		}
	}
}
