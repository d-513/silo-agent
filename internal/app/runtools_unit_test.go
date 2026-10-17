package app

import (
	"testing"

	"silo.agent/internal/app/knowledge"
	"silo.agent/internal/app/mailbox"
	"silo.agent/internal/app/models"
	"silo.agent/internal/app/tunnels"
	"silo.agent/internal/app/voice"
	"silo.agent/internal/config"
)

func offered(a *App, name string) bool {
	for _, tl := range a.toolsFor("bot", nil) {
		if tl.Name == name {
			return true
		}
	}
	return false
}

// allChatTools is the operator asking for every built-in as a chat tool.
const allChatTools = "tools:\n  expose_completion: [all]\n"

// The model's own tool list is the loop and what must be a chat tool; the
// rest is reached from Python, and the operator can bring any of it back.
func TestToolListIsLeanUnlessTheOperatorAsks(t *testing.T) {
	a := newToolApp()
	a.Store, _ = config.FromYAML([]byte("content:\n  host: t.example.com\n"))
	for _, name := range []string{"exec_python", "read", "remember", "recall", "open_tunnel", "create_automation", "spawn_agent"} {
		if !offered(a, name) {
			t.Errorf("%s is not offered by default", name)
		}
	}
	for _, name := range []string{"web_search", "feed", "task_add", "list_tunnels", "list_automations", "chats", "forget"} {
		if offered(a, name) {
			t.Errorf("%s is offered by default", name)
		}
		if a.toolDef("bot", name) != nil {
			t.Errorf("%s resolves as a chat tool by default", name)
		}
	}
	if n := len(a.toolsFor("bot", nil)); n > 28 {
		t.Errorf("%d tools offered by default; the list was meant to stay small", n)
	}

	a.Store, _ = config.FromYAML([]byte("tools:\n  expose_completion: [web_search, task_add]\n"))
	if !offered(a, "web_search") || !offered(a, "task_add") || offered(a, "feed") {
		t.Error("tools.expose_completion did not offer exactly the named tools")
	}
	if a.toolDef("bot", "web_search") == nil {
		t.Error("an exposed tool does not resolve")
	}
	a.Store, _ = config.FromYAML([]byte(allChatTools))
	if !offered(a, "feed") || !offered(a, "chats") {
		t.Error("`all` did not offer every built-in")
	}
}

func newToolApp() *App {
	a := &App{}
	a.Models = models.New(nil, a.cfg)
	a.Voice = voice.New(nil, a.cfg, a.Models, nil)
	a.Tunnels = tunnels.New(nil, nil, a.cfg, nil)
	a.Mail = mailbox.New(nil, a.cfg, nil, nil, nil)
	a.Knowledge = knowledge.New(nil, nil, a.cfg, a.Models, nil)
	a.Tools = a.newTools()
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
		"explicit host": "content:\n  host: t.example.com\n",
		"local dev":     "public_url: http://localhost:5173\n",
	} {
		a := newToolApp()
		a.Store, _ = config.FromYAML([]byte(yaml + allChatTools))
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
	a.Store, _ = config.FromYAML([]byte("mail:\n  domain: bots.example.com\n" + allChatTools))
	for _, n := range tools {
		if !offered(a, n) {
			t.Errorf("%s not offered with a mail domain", n)
		}
	}
}
