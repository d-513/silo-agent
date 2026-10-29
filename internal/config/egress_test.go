package config

import (
	"strings"
	"testing"
)

func TestValidateYAMLChecksEgressDNS(t *testing.T) {
	for yaml, want := range map[string]string{
		"egress:\n  dns: \"1.1.1.1, 9.9.9.9\"\n":                "",
		"egress:\n  dns: \"10.0.0.53:5353,[2001:db8::1]:53\"\n": "",
		"egress:\n  dns: \"2001:db8::53\"\n":                    "",
		"egress:\n  dns: \"\"\n":                                "",
		"egress:\n  dns: dns.google\n":                          "egress.dns",
		"egress:\n  dns: \"0.0.0.0\"\n":                         "egress.dns",
		"egress:\n  dns: \"1.1.1.1:0\"\n":                       "egress.dns",
		"egress:\n  dns: [1.1.1.1]\n":                           "egress.dns",
	} {
		err := validateYAML([]byte(yaml))
		switch {
		case want == "" && err != nil:
			t.Errorf("%q: %v", yaml, err)
		case want != "" && (err == nil || !strings.Contains(err.Error(), want)):
			t.Errorf("%q: error %v, want one naming %q", yaml, err, want)
		}
	}
	if !KnownKey("egress.dns") {
		t.Error("egress.dns is not a known setting: the admin form could not save it")
	}
	got := Config{Egress: Egress{DNS: "1.1.1.1, nonsense, [2001:db8::1]:5353"}}.EgressDNS()
	if strings.Join(got, ",") != "1.1.1.1:53,[2001:db8::1]:5353" {
		t.Errorf("EgressDNS = %v", got)
	}
}
