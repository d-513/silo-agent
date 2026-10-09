package mailin

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"blitiri.com.ar/go/spf"
	"github.com/emersion/go-msgauth/dkim"
	"golang.org/x/net/publicsuffix"
)

// Resolver is the DNS the checks ask; *net.Resolver is one.
type Resolver = spf.DNSResolver

// Verdict says whether the From header can be believed.
type Verdict struct {
	// Verified is true when the From address's organisation vouched for the
	// message: it signed it (DKIM), or it lists the sending server and is the
	// envelope sender too (SPF). This is DMARC's alignment test without the
	// policy lookup, since nothing here is ever rejected over it.
	Verified bool
	// Detail is the evidence in the words of an Authentication-Results line.
	Detail string
}

const authWithin = 10 * time.Second

// Authenticate checks raw against the DNS records of fromAddr's domain.
// fromAddr is the message's single From address ("" when it has none or
// several, which never verifies). dns nil is the system resolver.
func Authenticate(ctx context.Context, dns Resolver, env Envelope, raw []byte, fromAddr string) Verdict {
	if dns == nil {
		dns = net.DefaultResolver
	}
	ctx, cancel := context.WithTimeout(ctx, authWithin)
	defer cancel()
	fromDomain := domainOf(fromAddr)
	dkimOK, dkimDetail := checkDKIM(ctx, dns, raw, fromDomain)
	spfOK, spfDetail := checkSPF(ctx, dns, env, fromDomain)
	v := Verdict{Verified: fromDomain != "" && (dkimOK || spfOK), Detail: dkimDetail + "; " + spfDetail}
	if fromDomain == "" {
		v.Detail += "; no single From address"
	}
	return v
}

func checkDKIM(ctx context.Context, dns Resolver, raw []byte, fromDomain string) (bool, string) {
	sigs, err := dkim.VerifyWithOptions(bytes.NewReader(raw), &dkim.VerifyOptions{
		LookupTXT:        func(domain string) ([]string, error) { return dns.LookupTXT(ctx, domain) },
		MaxVerifications: 5,
	})
	if len(sigs) == 0 {
		if err != nil {
			return false, "dkim=fail (" + short(err.Error()) + ")"
		}
		return false, "dkim=none"
	}
	var other, failed string
	for _, s := range sigs {
		d := strings.ToLower(s.Domain)
		switch {
		case s.Err != nil:
			failed = fmt.Sprintf("dkim=fail (%s: %s)", d, short(s.Err.Error()))
		case !signsFrom(s):
			failed = fmt.Sprintf("dkim=fail (%s: From is not signed)", d)
		case aligned(d, fromDomain):
			return true, "dkim=pass (" + d + ")"
		default:
			other = "dkim=pass (" + d + ", not the From domain)"
		}
	}
	if other != "" {
		return false, other
	}
	return false, failed
}

func signsFrom(v *dkim.Verification) bool {
	for _, k := range v.HeaderKeys {
		if strings.EqualFold(k, "From") {
			return true
		}
	}
	return false
}

func checkSPF(ctx context.Context, dns Resolver, env Envelope, fromDomain string) (bool, string) {
	if env.Remote == nil {
		return false, "spf=none"
	}
	// A bounce has no envelope sender; SPF then judges the HELO name.
	sender := env.From
	if sender == "" {
		sender = "postmaster@" + env.Helo
	}
	res, _ := spf.CheckHostWithSender(env.Remote, env.Helo, sender, spf.WithResolver(dns), spf.WithContext(ctx))
	d := domainOf(sender)
	if res != spf.Pass {
		return false, "spf=" + string(res)
	}
	if !aligned(d, fromDomain) {
		return false, "spf=pass (" + d + ", not the From domain)"
	}
	return true, "spf=pass (" + d + ")"
}

func domainOf(addr string) string {
	i := strings.LastIndex(addr, "@")
	if i < 0 {
		return ""
	}
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(addr[i+1:]), "."))
}

// aligned reports whether two domains belong to one organisation: the same
// registrable domain (example.com for news.example.com), never just a shared
// public suffix (co.uk).
func aligned(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return org(a) == org(b)
}

func org(domain string) string {
	if o, err := publicsuffix.EffectiveTLDPlusOne(domain); err == nil {
		return o
	}
	return domain
}

func short(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 120 {
		s = s[:120] + "…"
	}
	return s
}
