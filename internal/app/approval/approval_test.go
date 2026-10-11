package approval

import (
	"encoding/json"
	"strings"
	"testing"

	"silo.agent/internal/security"
)

func TestVerdict(t *testing.T) {
	for _, tc := range []struct {
		allowed, forbidden float64
		want               string
	}{
		{0.97, 0.02, security.Allow},
		{0.9, 0.1, security.Allow},
		{0.89, 0.02, security.Ask},  // not sure enough that it is allowed
		{0.97, 0.4, security.Ask},   // allowed, but maybe forbidden too
		{0.5, 0.5, security.Ask},    // the policy is silent
		{0.02, 0.95, security.Deny}, // clearly forbidden
		{0.97, 0.95, security.Deny}, // a contradiction never allows
		{0.02, 0.6, security.Ask},   // probably forbidden: a human says
	} {
		if got := verdict(tc.allowed, tc.forbidden); got != tc.want {
			t.Errorf("verdict(%v, %v) = %s, want %s", tc.allowed, tc.forbidden, got, tc.want)
		}
	}
}

func TestParseVerdict(t *testing.T) {
	for in, want := range map[string]string{
		"approve": security.Allow, "Approve.": security.Allow, " allow": security.Allow,
		"deny": security.Deny, "Denied: no clicks": security.Deny,
		"yes, it is safe": security.Allow,
		"ask":             security.Ask, "human review": security.Ask, "whatever": security.Ask,
		"": security.Ask, "not approve": security.Ask, "I think approve": security.Ask,
	} {
		if got := parseVerdict(in); got != want {
			t.Errorf("parseVerdict(%q) = %s, want %s", in, got, want)
		}
	}
}

func action() security.Prompt {
	return security.Describe("terminal", "run", `{"command":"ls"}`)
}

// The gate is shown the human's message only when a human wrote one. An
// automation or a background process says so instead; nothing stands in.
func TestRequestNeverInventsAHumanMessage(t *testing.T) {
	chat := Request{Policy: "POLICY_TEXT", Origin: OriginChat, Name: "Tidy up", Human: "list my files", Key: "terminal.run", Action: action()}
	text := chat.text()
	for _, want := range []string{`the chat "Tidy up"`, "<human_message>\nlist my files\n</human_message>", "terminal.run", "<action>"} {
		if !strings.Contains(text, want) {
			t.Errorf("chat request missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "POLICY_TEXT") {
		t.Errorf("the policy belongs in the system prompt, not the request:\n%s", text)
	}

	for origin, want := range map[string]string{OriginAutomation: "Automations", OriginUnknown: "Unknown"} {
		r := Request{Policy: "P", Origin: origin, Name: "Nightly", Key: "terminal.run", Action: action()}
		text := r.text()
		if !strings.Contains(text, "Origin: "+want) || !strings.Contains(text, "No human message asked for this") {
			t.Errorf("%s request:\n%s", origin, text)
		}
		if strings.Contains(text, "human_message") {
			t.Errorf("%s request carries a human message:\n%s", origin, text)
		}
		raw, err := r.state()
		if err != nil {
			t.Fatal(err)
		}
		var st map[string]any
		_ = json.Unmarshal(raw, &st)
		if _, has := st["human_message"]; has {
			t.Errorf("%s state carries a human message: %s", origin, raw)
		}
		if o, _ := st["origin"].(string); !strings.HasPrefix(o, want) {
			t.Errorf("%s state origin %q", origin, o)
		}
	}

	raw, _ := chat.state()
	var st struct {
		Policy string `json:"policy"`
		Human  string `json:"human_message"`
		Action struct {
			Name    string            `json:"name"`
			Details map[string]string `json:"details"`
		} `json:"action"`
	}
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatal(err)
	}
	if st.Policy != "POLICY_TEXT" || st.Human != "list my files" || st.Action.Name != "terminal.run" {
		t.Fatalf("state %s", raw)
	}

	// A conversation the human has not written in says that, too.
	quiet := Request{Origin: OriginChat, Name: "Mail: hello", Key: "terminal.run", Action: action()}
	if text := quiet.text(); !strings.Contains(text, "written no message") || strings.Contains(text, "human_message") {
		t.Errorf("quiet chat request:\n%s", text)
	}
}

func TestQuestionsAreValid(t *testing.T) {
	for name, q := range questions {
		raw, err := json.Marshal(q)
		if err != nil || !strings.Contains(string(raw), `"noul"`) {
			t.Errorf("%s: %s %v", name, raw, err)
		}
	}
}
