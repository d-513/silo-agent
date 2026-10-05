package app_test

import (
	"encoding/json"
	"strings"
	"testing"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/apptest"
	"silo.agent/internal/db"
	"silo.agent/internal/llm"
	"silo.agent/internal/llm/dummy"
)

// toolNames is the tool list of the last request the model was sent.
func toolNames(string) []string {
	var names []string
	reqs := dummy.Streamed()
	if len(reqs) == 0 {
		return nil
	}
	for _, t := range reqs[len(reqs)-1].Tools {
		names = append(names, t.Name)
	}
	return names
}

func systemText(r llm.Request) string {
	var b strings.Builder
	for _, blk := range r.System {
		b.WriteString(blk.Text)
	}
	return b.String()
}

func has(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

func TestTunnelToolsOpenListClose(t *testing.T) {
	h := tunnelHarness(t, "")
	dummy.Script("Test_tun1",
		memCall("open_tunnel", `{"port":8000}`),
		memCall("open_tunnel", `{"port":8000}`), // idempotent
		memCall("open_tunnel", `{"port":5900}`), // reserved
		memCall("list_tunnels", `{}`),
		memCall("close_tunnel", `{"port":8000}`),
		memCall("close_tunnel", `{"port":8000}`), // already gone
		memCall("list_tunnels", `{}`),
		dummy.Turn{Text: "done"},
	)
	bot := h.CreateBot("Opener")
	run, _ := h.Send(bot.GetId(), h.FirstChat(bot.GetId()), "Test_tun1_Input")
	res := memResults(h.WaitRun(run))
	if len(res) != 7 {
		t.Fatalf("%d results\n%s", len(res), h.RunBody(run))
	}

	// The row is gone by now; recover the name from the first result.
	first := res[0]
	if !strings.Contains(first, "http://") || !strings.Contains(first, "."+tunnelTestHost) || !strings.Contains(first, "8000") || !strings.Contains(strings.ToLower(first), "private") {
		t.Fatalf("open result = %q", first)
	}
	name := strings.TrimSuffix(strings.TrimPrefix(first[strings.Index(first, "http://"):], "http://"), "")
	name = name[:strings.Index(name, "."+tunnelTestHost)]
	if res[1] != first {
		t.Fatalf("opening the same port twice differs:\n%q\n%q", res[0], res[1])
	}
	if !strings.Contains(res[2], "reserved") {
		t.Fatalf("port 5900 = %q", res[2])
	}
	if !strings.Contains(res[3], name) || !strings.Contains(res[3], "8000") {
		t.Fatalf("list = %q", res[3])
	}
	if !strings.Contains(strings.ToLower(res[4]), "closed") {
		t.Fatalf("close = %q", res[4])
	}
	if !strings.Contains(strings.ToLower(res[5]), "no tunnel") {
		t.Fatalf("second close = %q", res[5])
	}
	if strings.Contains(res[6], name) {
		t.Fatalf("list after close still shows it: %q", res[6])
	}
	var left int64
	h.DB.Model(&db.Tunnel{}).Where("bot_id = ?", bot.GetId()).Count(&left)
	if left != 0 {
		t.Fatalf("rows left: %d", left)
	}
}

func TestTunnelToolCreatesBotOwnedPrivateTunnel(t *testing.T) {
	h := tunnelHarness(t, "")
	dummy.Script("Test_tun2", memCall("open_tunnel", `{"port":3000}`), dummy.Turn{Text: "done"})
	bot := h.CreateBot("Maker")
	run, _ := h.Send(bot.GetId(), h.FirstChat(bot.GetId()), "Test_tun2_Input")
	h.WaitRun(run)
	var row db.Tunnel
	if err := h.DB.First(&row, "bot_id = ?", bot.GetId()).Error; err != nil {
		t.Fatal(err)
	}
	if row.Public || row.CreatedBy != "bot" || row.Port != 3000 {
		t.Fatalf("row = %+v", row)
	}
	// The owner sees it in the Tunnels tab like any other.
	if got := tunnelList(t, h, bot.GetId()).GetTunnels(); len(got) != 1 || got[0].GetCreatedBy() != "bot" {
		t.Fatalf("list = %v", got)
	}
}

// Anything public needs the human: anyone with the link can reach the Bot's
// service, not just the owner.
func TestTunnelPublicAsksTheOwner(t *testing.T) {
	h := tunnelHarness(t, "")
	dummy.Script("Test_tun3", memCall("open_tunnel", `{"port":8000,"public":true}`), dummy.Turn{Text: "done"})
	bot := h.CreateBot("Publisher")
	id := bot.GetId()
	run, _ := h.Send(id, h.FirstChat(id), "Test_tun3_Input")

	ap := h.WaitApproval(id)
	if ap.GetConnector() != "tunnels" || ap.GetAction() != "public" {
		t.Fatalf("approval = %s.%s", ap.GetConnector(), ap.GetAction())
	}
	if !strings.Contains(strings.ToLower(ap.GetSummary()), "public") || !strings.Contains(ap.GetSummary()+ap.GetArgsJson(), "8000") {
		t.Fatalf("slip does not say what is being exposed: %+v", ap)
	}
	if n := len(tunnelList(t, h, id).GetTunnels()); n != 0 {
		t.Fatal("the tunnel existed before the owner decided")
	}
	if _, err := h.Client.DecideApproval(h.Ctx(), connect.NewRequest(&v1.DecideApprovalRequest{Id: ap.GetId(), Decision: "allow"})); err != nil {
		t.Fatal(err)
	}
	res := memResults(h.WaitRun(run))
	if len(res) != 1 || !strings.Contains(strings.ToLower(res[0]), "public") {
		t.Fatalf("result %q", res)
	}
	if got := tunnelList(t, h, id).GetTunnels(); len(got) != 1 || !got[0].GetPublic() {
		t.Fatalf("list = %v", got)
	}
}

func TestTunnelPublicDenied(t *testing.T) {
	h := tunnelHarness(t, "")
	dummy.Script("Test_tun4", memCall("open_tunnel", `{"port":8000,"public":true}`), dummy.Turn{Text: "done"})
	bot := h.CreateBot("Refused")
	id := bot.GetId()
	run, _ := h.Send(id, h.FirstChat(id), "Test_tun4_Input")
	ap := h.WaitApproval(id)
	h.Client.DecideApproval(h.Ctx(), connect.NewRequest(&v1.DecideApprovalRequest{Id: ap.GetId(), Decision: "deny"}))
	res := memResults(h.WaitRun(run))
	if len(res) != 1 || !strings.Contains(res[0], "denied") {
		t.Fatalf("result %q", res)
	}
	// Denying the public one does not quietly make a private one either.
	if n := len(tunnelList(t, h, id).GetTunnels()); n != 0 {
		t.Fatalf("%d tunnels after a denied public request", n)
	}
}

func TestTunnelFlippingAnExistingOneToPublicAsksToo(t *testing.T) {
	h := tunnelHarness(t, "")
	bot := h.CreateBot("Flipper")
	id := bot.GetId()
	tun := createTunnel(t, h, id, 8000, false)
	dummy.Script("Test_tun5", memCall("open_tunnel", `{"port":8000,"public":true}`), dummy.Turn{Text: "done"})
	run, _ := h.Send(id, h.FirstChat(id), "Test_tun5_Input")
	ap := h.WaitApproval(id)
	if ap.GetAction() != "public" {
		t.Fatalf("approval action %q", ap.GetAction())
	}
	h.Client.DecideApproval(h.Ctx(), connect.NewRequest(&v1.DecideApprovalRequest{Id: ap.GetId(), Decision: "allow"}))
	h.WaitRun(run)
	var row db.Tunnel
	h.DB.First(&row, "id = ?", tun.GetId())
	if !row.Public || row.Name != tun.GetName() {
		t.Fatalf("row = %+v", row)
	}
}

// Asking again for a public tunnel never turns it private behind the owner's
// back, and a plain open of a public one leaves it alone.
func TestTunnelOpenNeverDowngrades(t *testing.T) {
	h := tunnelHarness(t, "")
	bot := h.CreateBot("Keeper")
	id := bot.GetId()
	tun := createTunnel(t, h, id, 8000, true)
	dummy.Script("Test_tun6", memCall("open_tunnel", `{"port":8000}`), dummy.Turn{Text: "done"})
	run, _ := h.Send(id, h.FirstChat(id), "Test_tun6_Input")
	res := memResults(h.WaitRun(run))
	var row db.Tunnel
	h.DB.First(&row, "id = ?", tun.GetId())
	if !row.Public || len(res) != 1 || !strings.Contains(strings.ToLower(res[0]), "public") {
		t.Fatalf("row %+v result %q", row, res)
	}
}

func TestTunnelRulesGateTheTools(t *testing.T) {
	h := tunnelHarness(t, "")
	bot := h.CreateBot("Ruled")
	id := bot.GetId()
	h.Client.SetRule(h.Ctx(), connect.NewRequest(&v1.SetRuleRequest{BotId: id, Connector: "tunnels", Action: "open", Decision: "deny"}))
	h.Client.SetRule(h.Ctx(), connect.NewRequest(&v1.SetRuleRequest{BotId: id, Connector: "tunnels", Action: "public", Decision: "allow"}))
	dummy.Script("Test_tun7", memCall("open_tunnel", `{"port":8000}`), memCall("open_tunnel", `{"port":8001,"public":true}`), dummy.Turn{Text: "done"})
	run, _ := h.Send(id, h.FirstChat(id), "Test_tun7_Input")
	res := memResults(h.WaitRun(run))
	if len(res) != 2 || !strings.Contains(res[0], "denied") {
		t.Fatalf("results %q", res)
	}
	// Public allowed by rule, but open is denied: nothing was created.
	if !strings.Contains(res[1], "denied") || len(tunnelList(t, h, id).GetTunnels()) != 0 {
		t.Fatalf("results %q", res)
	}
}

func TestPythonTunnelPaths(t *testing.T) {
	h := tunnelHarness(t, "")
	bot := h.CreateBot("PyTunnels")
	id := bot.GetId()
	h.Client.SetRule(h.Ctx(), connect.NewRequest(&v1.SetRuleRequest{BotId: id, Connector: "tunnels", Action: "public", Decision: "allow"}))

	res := pyCall(t, h, id, "", "tunnels", "open", `{"port":8000,"public":true}`)
	if res.GetError() != "" {
		t.Fatalf("open: %+v", res)
	}
	var opened struct {
		Name    string `json:"name"`
		URL     string `json:"url"`
		Port    int    `json:"port"`
		Public  bool   `json:"public"`
		Created bool   `json:"created"`
	}
	if err := json.Unmarshal([]byte(res.GetResultJson()), &opened); err != nil {
		t.Fatalf("open json %q: %v", res.GetResultJson(), err)
	}
	if opened.Port != 8000 || !opened.Public || !opened.Created || opened.URL != "http://"+opened.Name+"."+tunnelTestHost {
		t.Fatalf("opened = %+v", opened)
	}
	again := pyCall(t, h, id, "", "tunnels", "open", `{"port":8000}`)
	var second struct {
		Created bool   `json:"created"`
		Name    string `json:"name"`
	}
	json.Unmarshal([]byte(again.GetResultJson()), &second)
	if second.Created || second.Name != opened.Name {
		t.Fatalf("second open = %s", again.GetResultJson())
	}

	list := pyCall(t, h, id, "", "tunnels", "list", `{}`)
	var listed struct {
		Tunnels []struct {
			Name string `json:"name"`
			Port int    `json:"port"`
		} `json:"tunnels"`
	}
	if err := json.Unmarshal([]byte(list.GetResultJson()), &listed); err != nil || len(listed.Tunnels) != 1 || listed.Tunnels[0].Name != opened.Name {
		t.Fatalf("list = %s, %v", list.GetResultJson(), err)
	}

	closed := pyCall(t, h, id, "", "tunnels", "close", `{"name":"`+opened.Name+`"}`)
	if closed.GetError() != "" || !strings.Contains(closed.GetResultJson(), opened.Name) {
		t.Fatalf("close = %+v", closed)
	}
	if n := len(tunnelList(t, h, id).GetTunnels()); n != 0 {
		t.Fatalf("%d left", n)
	}
	if bad := pyCall(t, h, id, "", "tunnels", "open", `{"port":9222}`); !strings.Contains(bad.GetError(), "reserved") {
		t.Fatalf("reserved port: %+v", bad)
	}
}

// close_tunnel takes the name or the port, and only ever touches this Bot's.
func TestTunnelCloseIsScopedToTheBot(t *testing.T) {
	h := tunnelHarness(t, "")
	mine := h.CreateBot("Mine").GetId()
	theirs := h.CreateBot("Theirs").GetId()
	other := createTunnel(t, h, theirs, 8000, false)
	createTunnel(t, h, mine, 8000, false)

	if res := pyCall(t, h, mine, "", "tunnels", "close", `{"name":"`+other.GetName()+`"}`); res.GetError() == "" {
		t.Fatalf("closed another Bot's tunnel: %+v", res)
	}
	if n := len(tunnelList(t, h, theirs).GetTunnels()); n != 1 {
		t.Fatalf("other Bot's tunnel gone (%d)", n)
	}
	if res := pyCall(t, h, mine, "", "tunnels", "close", `{"port":8000}`); res.GetError() != "" {
		t.Fatalf("close by port: %+v", res)
	}
	if n := len(tunnelList(t, h, mine).GetTunnels()); n != 0 {
		t.Fatalf("own tunnel not closed (%d)", n)
	}
}

func TestTunnelToolsOnlyOfferedWhenUsable(t *testing.T) {
	// On: all three are in the tool list.
	h := tunnelHarness(t, "")
	dummy.Script("Test_tun8", dummy.Turn{Text: "hi"})
	bot := h.CreateBot("On")
	run, _ := h.Send(bot.GetId(), h.FirstChat(bot.GetId()), "Test_tun8_Input")
	h.WaitRun(run)
	on := toolNames("Test_tun8")
	for _, n := range []string{"open_tunnel", "list_tunnels", "close_tunnel"} {
		if !has(on, n) {
			t.Errorf("%s missing while tunnels are on: %v", n, on)
		}
	}

	// No domain, and disabled: none of them, and the prompt does not mention them.
	for name, mk := range map[string]func() *apptest.H{
		"no domain": func() *apptest.H { dummy.Reset(); return apptest.New(t) },
		"disabled":  func() *apptest.H { return tunnelHarness(t, "  enabled: false\n") },
	} {
		h := mk()
		dummy.Script("Test_tun9", dummy.Turn{Text: "hi"})
		bot := h.CreateBot("Off")
		run, _ := h.Send(bot.GetId(), h.FirstChat(bot.GetId()), "Test_tun9_Input")
		h.WaitRun(run)
		off := toolNames("Test_tun9")
		for _, n := range []string{"open_tunnel", "list_tunnels", "close_tunnel"} {
			if has(off, n) {
				t.Errorf("%s: %s offered while tunnels are unusable", name, n)
			}
		}
		for _, r := range dummy.Streamed() {
			if strings.Contains(systemText(r), "open_tunnel") {
				t.Errorf("%s: system prompt mentions tunnels", name)
			}
		}
		// And the Python path refuses with a reason.
		if res := pyCall(t, h, bot.GetId(), "", "tunnels", "open", `{"port":8000}`); res.GetError() == "" {
			t.Errorf("%s: python open succeeded", name)
		}
	}
}

func TestTunnelSystemPromptSection(t *testing.T) {
	h := tunnelHarness(t, "")
	dummy.Script("Test_tun10", dummy.Turn{Text: "hi"})
	bot := h.CreateBot("Prompted")
	run, _ := h.Send(bot.GetId(), h.FirstChat(bot.GetId()), "Test_tun10_Input")
	h.WaitRun(run)
	var sys string
	for _, r := range dummy.Streamed() {
		sys = systemText(r)
	}
	for _, want := range []string{"open_tunnel", "private", "127.0.0.1"} {
		if !strings.Contains(sys, want) {
			t.Errorf("system prompt lacks %q", want)
		}
	}
}
