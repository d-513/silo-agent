package egress

import (
	"bufio"
	_ "embed"
	"math"
	"regexp"
	"strings"

	"golang.org/x/net/publicsuffix"
)

//go:embed lists/exfil_sinks.txt
var exfilSinksTxt string

// sinkSet is the exfil-sink list: host patterns, some with a path prefix
// ("discord.com/api/webhooks"), because only that part of the host takes data
// from strangers.
type sinkSet struct {
	hosts hostSet
	paths map[string][]string
}

func parseSinks(txt string) sinkSet {
	s := sinkSet{hosts: newHostSet(), paths: map[string][]string{}}
	sc := bufio.NewScanner(strings.NewReader(txt))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		host, path, ok := strings.Cut(line, "/")
		if ok {
			s.paths[NormHost(host)] = append(s.paths[NormHost(host)], "/"+path)
			continue
		}
		s.hosts.add(host)
	}
	return s
}

var defaultSinks = parseSinks(exfilSinksTxt)

// match reports whether a request to host and path goes to a sink.
func (s sinkSet) match(host, path string) (string, bool) {
	if p, ok := s.hosts.match(host); ok {
		return p, true
	}
	host = NormHost(host)
	for _, prefix := range s.paths[host] {
		if strings.HasPrefix(path, prefix) {
			return host + prefix, true
		}
	}
	return "", false
}

// secretShape is a pattern that only a credential matches. They are kept to
// formats with a fixed prefix: a pattern that also matches ordinary text
// would bury the owner in flags.
type secretShape struct {
	name string
	re   *regexp.Regexp
}

var secretShapes = []secretShape{
	{"AWS access key", regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`)},
	{"GitHub token", regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9]{36,}|github_pat_[A-Za-z0-9_]{60,})\b`)},
	{"GitLab token", regexp.MustCompile(`\bglpat-[A-Za-z0-9_-]{20,}\b`)},
	{"OpenAI key", regexp.MustCompile(`\bsk-(?:proj-|svcacct-|admin-)?[A-Za-z0-9_-]{20,}T3BlbkFJ[A-Za-z0-9_-]{20,}\b`)},
	{"Anthropic key", regexp.MustCompile(`\bsk-ant-(?:api|admin)\d{2}-[A-Za-z0-9_-]{80,}\b`)},
	{"OpenRouter key", regexp.MustCompile(`\bsk-or-v1-[a-f0-9]{64}\b`)},
	{"Slack token", regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9-]{10,}\b`)},
	{"Google API key", regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}\b`)},
	{"Stripe key", regexp.MustCompile(`\b(?:sk|rk)_live_[A-Za-z0-9]{20,}\b`)},
	{"private key", regexp.MustCompile(`-----BEGIN (?:RSA |EC |DSA |OPENSSH |ENCRYPTED |PGP )?PRIVATE KEY(?: BLOCK)?-----`)},
}

// findSecretShape names the first credential-shaped string in s.
func findSecretShape(s string) (string, bool) {
	for _, p := range secretShapes {
		if p.re.MatchString(s) {
			return p.name, true
		}
	}
	return "", false
}

// entropy is the Shannon entropy of s in bits per byte.
func entropy(s string) float64 {
	if s == "" {
		return 0
	}
	var counts [256]int
	for i := 0; i < len(s); i++ {
		counts[s[i]]++
	}
	var h float64
	n := float64(len(s))
	for _, c := range counts {
		if c > 0 {
			p := float64(c) / n
			h -= p * math.Log2(p)
		}
	}
	return h
}

// encodedQuery reports whether a query string looks like data carried out:
// long and close to random. Ordinary tracking parameters are long too, but
// readable names and repeated values keep their entropy lower.
func encodedQuery(q string, min int) bool {
	if min <= 0 {
		min = DefaultQueryMin
	}
	return len(q) >= min && entropy(q) >= 5.0
}

// registrable returns the part of host a person registered (example in
// mail.example.co.uk) and its public suffix.
func registrable(host string) (label string, ok bool) {
	host = NormHost(host)
	etld1, err := publicsuffix.EffectiveTLDPlusOne(host)
	if err != nil {
		return "", false
	}
	label, _, _ = strings.Cut(etld1, ".")
	return label, label != ""
}

// randomDomain reports whether the registered name looks machine-made, the
// way domain generation algorithms make them: long, no dashes, few vowels,
// high entropy. It flags; it never blocks, because people register odd
// names too.
func randomDomain(host string) bool {
	label, ok := registrable(host)
	if !ok || len(label) < 12 || strings.Contains(label, "-") {
		return false
	}
	vowels, digits := 0, 0
	for _, r := range label {
		switch {
		case strings.ContainsRune("aeiouy", r):
			vowels++
		case r >= '0' && r <= '9':
			digits++
		}
	}
	ratio := float64(vowels) / float64(len(label))
	return entropy(label) >= 3.5 && (ratio < 0.2 || (digits >= 3 && ratio < 0.3))
}

// dnsShape reports whether a name looks like data packed into DNS labels: a
// very long label, a very long name, or several long random-looking labels.
func dnsShape(name string) bool {
	name = NormHost(name)
	if len(name) >= 160 {
		return true
	}
	long := 0
	for _, l := range strings.Split(name, ".") {
		if len(l) >= 52 {
			return true
		}
		if len(l) >= 24 && entropy(l) >= 4.0 {
			long++
		}
	}
	return long >= 2
}

// executable names what kind of program a body starts with, if it is one.
func executable(head []byte, contentType string) (string, bool) {
	switch {
	case len(head) >= 4 && string(head[:4]) == "\x7fELF":
		return "Linux program (ELF)", true
	case len(head) >= 2 && string(head[:2]) == "MZ":
		return "Windows program (PE)", true
	case len(head) >= 4 && (string(head[:4]) == "\xcf\xfa\xed\xfe" || string(head[:4]) == "\xce\xfa\xed\xfe" || string(head[:4]) == "\xca\xfe\xba\xbe"):
		return "macOS program (Mach-O)", true
	case len(head) >= 2 && string(head[:2]) == "#!" && !strings.HasPrefix(contentType, "text/html"):
		return "script", true
	}
	return "", false
}
