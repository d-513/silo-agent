package app

import (
	"os"
	"sort"
	"strings"
	"testing"

	"silo.agent/internal/security"
)

// Every tool exposed to Python must have a silo_runtime helper that calls its
// connector.action, so adding ExposePython to a definition cannot leave Python
// without a way in.
func TestPythonToolsHaveHelpers(t *testing.T) {
	src, err := os.ReadFile("../../botimage/silo_runtime.py")
	if err != nil {
		t.Fatal(err)
	}
	py := string(src)
	for _, d := range (&App{}).toolDefs() {
		if !d.ExposePython {
			continue
		}
		if !security.Reserved(d.Connector) {
			t.Errorf("%s: %s is not a built-in connector", d.Name, d.Connector)
		}
		if !strings.Contains(py, "\ndef "+d.PyHelper()+"(") {
			t.Errorf("silo_runtime has no %s() for the %s tool", d.PyHelper(), d.Name)
		}
		if !strings.Contains(py, `call("`+d.Connector+`", "`+d.Action+`"`) && !strings.Contains(py, "\""+d.Connector+"\",\n        \""+d.Action+"\"") {
			t.Errorf("silo_runtime.%s does not call %s.%s", d.PyHelper(), d.Connector, d.Action)
		}
	}
}

// A tool the engine gates needs a default in the rule catalog; one that gates
// itself still names the rule family it belongs to.
func TestToolRulesAreInTheCatalog(t *testing.T) {
	for _, d := range (&App{}).toolDefs() {
		if d.Connector == "" {
			if d.Name != "sleep" {
				t.Errorf("%s has no rule", d.Name)
			}
			continue
		}
		if _, ok := security.Default(d.Connector, d.Action); !ok && !d.SelfGated {
			t.Errorf("%s: %s.%s has no default in the rule catalog", d.Name, d.Connector, d.Action)
		}
	}
}

// The exposure of the built-in tools, as shipped. Changing a tool's
// ExposeCompletion / ExposePython is deliberate: update this list with it.
func TestBuiltinToolExposure(t *testing.T) {
	var chat, python, both []string
	for _, d := range (&App{}).toolDefs() {
		switch {
		case d.ExposeCompletion && d.ExposePython:
			both = append(both, d.Name)
		case d.ExposeCompletion:
			chat = append(chat, d.Name)
		default:
			python = append(python, d.Name)
		}
	}
	for _, l := range [][]string{chat, python, both} {
		sort.Strings(l)
	}
	want := map[string]string{
		"chat": "agent_status delete exec_python grep message_agent patch present read skill sleep soul core_memory spawn_agent stop_agent switch_model terminal write",
		"python": "channel chats close_tunnel delete_automation feed forget list_automations list_mail list_models list_tunnels read_mail search_docs " +
			"task_add task_done task_list task_reset transcribe web_search",
		"both": "artifact click create_automation key look open_tunnel recall remember scroll type update_automation",
	}
	for name, got := range map[string][]string{"chat": chat, "python": python, "both": both} {
		w := strings.Fields(want[name])
		sort.Strings(w)
		if strings.Join(got, " ") != strings.Join(w, " ") {
			t.Errorf("%s-exposed tools:\n got %s\nwant %s", name, strings.Join(got, " "), strings.Join(w, " "))
		}
	}
}

// Lead-only tools are the ones a subagent's list leaves out.
func TestLeadOnlyTools(t *testing.T) {
	var got []string
	for _, d := range (&App{}).toolDefs() {
		if d.LeadOnly {
			got = append(got, d.Name)
		}
	}
	sort.Strings(got)
	want := "agent_status channel create_automation delete_automation list_automations message_agent spawn_agent stop_agent switch_model task_reset update_automation"
	if strings.Join(got, " ") != want {
		t.Fatalf("lead-only tools: %s", strings.Join(got, " "))
	}
}

// Writes is what takes a change snapshot and clears the repeat guard.
func TestWritingTools(t *testing.T) {
	var got []string
	for _, d := range (&App{}).toolDefs() {
		if d.Writes {
			got = append(got, d.Name)
		}
	}
	sort.Strings(got)
	if strings.Join(got, " ") != "click delete exec_python key patch terminal type write" {
		t.Fatalf("writing tools: %s", strings.Join(got, " "))
	}
}

func TestPythonResultIsAlwaysJSON(t *testing.T) {
	for in, want := range map[string]string{
		`{"a":1}`:    `{"a":1}`,
		`[1,2]`:      `[1,2]`,
		"remembered": `{"result":"remembered"}`,
		"":           `{"result":""}`,
	} {
		if got := pythonResult(in); got != want {
			t.Errorf("pythonResult(%q) = %s, want %s", in, got, want)
		}
	}
}
