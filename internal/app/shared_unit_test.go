package app

import (
	"os"
	"strings"
	"testing"

	"silo.agent/internal/security"
)

// Every chat tool shared with Python must be a real chat tool, a known rule,
// and have a silo_runtime helper of the same name, so the two paths cannot drift.
func TestSharedToolsHavePythonHelpers(t *testing.T) {
	src, err := os.ReadFile("../../botimage/silo_runtime.py")
	if err != nil {
		t.Fatal(err)
	}
	py := string(src)
	defined := map[string]bool{}
	for _, d := range toolDefs {
		defined[d.Name] = true
	}
	for name, key := range sharedTools {
		if !defined[name] {
			t.Errorf("%s is shared but not a chat tool", name)
		}
		if !security.Reserved(key[0]) {
			t.Errorf("%s: %s is not a built-in connector", name, key[0])
		}
		if !strings.Contains(py, "\ndef "+name+"(") {
			t.Errorf("silo_runtime has no %s()", name)
		}
		if !strings.Contains(py, `call("`+key[0]+`", "`+key[1]+`"`) && !strings.Contains(py, "\""+key[0]+"\",\n        \""+key[1]+"\"") {
			t.Errorf("silo_runtime.%s does not call %s.%s", name, key[0], key[1])
		}
		if got, ok := sharedToolName(key[0], key[1]); !ok || got != name {
			t.Errorf("sharedToolName(%s, %s) = %q", key[0], key[1], got)
		}
	}
}
