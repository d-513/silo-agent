package app

import (
	"testing"

	"silo.agent/internal/app/mailbox"
	"silo.agent/internal/app/models"
	"silo.agent/internal/app/tunnels"
	"silo.agent/internal/app/voice"
	"silo.agent/internal/config"
)

func offered(a *App, name string) bool {
	for _, tl := range a.runTools() {
		if tl.Name == name {
			return true
		}
	}
	return false
}

func newToolApp() *App {
	a := &App{}
	a.Models = models.New(nil, a.cfg)
	a.Voice = voice.New(nil, a.cfg, a.Models, nil)
	a.Tunnels = tunnels.New(nil, nil, a.cfg, nil)
	a.Mail = mailbox.New(nil, a.cfg, nil, nil, nil)
	return a
}

func TestRunToolsDropsTranscribeWhenVoiceOff(t *testing.T) {
	a := newToolApp()
	if offered(a, "transcribe") {
		t.Fatal("no config: transcribe must not be offered")
	}
}

func TestRunToolsOffersTunnelToolsOnlyWithADomain(t *testing.T) {
	tools := []string{"open_tunnel", "list_tunnels", "close_tunnel"}
	for name, yaml := range map[string]string{
		"no config":   "",
		"disabled":    "tunnels:\n  enabled: false\n  host: t.example.com\n",
		"no domain":   "public_url: https://silo.example.com\n",
		"no host yet": "tunnels:\n  enabled: true\n",
	} {
		a := newToolApp()
		if yaml != "" {
			a.Store, _ = config.FromYAML([]byte(yaml))
		}
		for _, n := range tools {
			if offered(a, n) {
				t.Errorf("%s: %s offered", name, n)
			}
		}
	}
	for name, yaml := range map[string]string{
		"explicit host": "tunnels:\n  host: t.example.com\n",
		"local dev":     "public_url: http://localhost:5173\n",
	} {
		a := newToolApp()
		a.Store, _ = config.FromYAML([]byte(yaml))
		for _, n := range tools {
			if !offered(a, n) {
				t.Errorf("%s: %s not offered", name, n)
			}
		}
	}
}

func TestRunToolsOffersMailToolsOnlyWithADomain(t *testing.T) {
	tools := []string{"list_mail", "read_mail"}
	for name, yaml := range map[string]string{
		"no config":  "",
		"disabled":   "mail:\n  enabled: false\n  domain: bots.example.com\n",
		"no domain":  "public_url: http://localhost:5173\n",
		"bad domain": "mail:\n  domain: \"bots.example.com:25\"\n",
	} {
		a := newToolApp()
		if yaml != "" {
			a.Store, _ = config.FromYAML([]byte(yaml))
		}
		for _, n := range tools {
			if offered(a, n) {
				t.Errorf("%s: %s offered", name, n)
			}
		}
	}
	a := newToolApp()
	a.Store, _ = config.FromYAML([]byte("mail:\n  domain: bots.example.com\n"))
	for _, n := range tools {
		if !offered(a, n) {
			t.Errorf("%s not offered with a mail domain", n)
		}
	}
}
