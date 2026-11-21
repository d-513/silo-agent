package app_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/apptest"
	"silo.agent/internal/llm"
	"silo.agent/internal/llm/dummy"
)

func imageYAML(t *testing.T, extra string) string {
	t.Helper()
	return apptest.DefaultYAML(t.TempDir()) + "image_models:\n  - dummy/paint\n  - dummy/sketch\nmodel_image: dummy/paint\n" + extra
}

type imageAnswer struct {
	Model  string `json:"model"`
	Images []struct {
		Path  string `json:"path"`
		Mime  string `json:"mime"`
		Bytes int    `json:"bytes"`
	} `json:"images"`
	Cost float64 `json:"cost"`
}

// generate_image asks the operator's image model and writes what it made into
// the Bot's workspace, where the Bot named or under images/; pictures from the
// workspace go along as inputs; the prompt and list_models tell the Bot of it;
// and it is there only while the operator picked an image model.
func TestGenerateImageTool(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t, apptest.WithYAML(imageYAML(t, "")))
	bot := h.CreateBot("Painter")
	w := h.StartWorker(bot.GetId())
	call := func(h *apptest.H, botID, args string) *v1.ToolRes {
		t.Helper()
		res, err := h.WorkerClient(botID).CallTool(h.Ctx(), connect.NewRequest(&v1.ToolReq{Connector: "model", Action: "image", ArgsJson: args}))
		if err != nil {
			t.Fatal(err)
		}
		return res.Msg
	}
	answer := func(res *v1.ToolRes) imageAnswer {
		t.Helper()
		if res.GetError() != "" {
			t.Fatalf("generate_image: %s", res.GetError())
		}
		var out imageAnswer
		if err := json.Unmarshal([]byte(res.GetResultJson()), &out); err != nil {
			t.Fatalf("result %q: %v", res.GetResultJson(), err)
		}
		return out
	}

	// No path: under images/, named after the prompt, in the model's format.
	out := answer(call(h, bot.GetId(), `{"prompt":"A red fox in snow"}`))
	if out.Model != "dummy/paint" || len(out.Images) != 1 || out.Cost == 0 {
		t.Fatalf("answer %+v", out)
	}
	p := out.Images[0].Path
	if !strings.HasPrefix(p, "images/a-red-fox-in-snow-") || !strings.HasSuffix(p, ".png") || out.Images[0].Mime != "image/png" {
		t.Fatalf("default path %q", p)
	}
	saved, err := os.ReadFile(filepath.Join(w.Workspace, p))
	if err != nil || !bytes.Equal(saved, dummy.Swatch("A red fox in snow", 0)) {
		t.Fatalf("the image on disk: %d bytes, %v", len(saved), err)
	}

	// A path, several images, an input from the workspace, another model. The
	// extension is the format that came, whatever the Bot wrote.
	out = answer(call(h, bot.GetId(), `{"prompt":"Make it night","path":"site/hero.jpg","count":2,"aspect":"16:9","images":["`+p+`"],"model":"dummy/sketch"}`))
	if out.Model != "dummy/sketch" || len(out.Images) != 2 || out.Images[0].Path != "site/hero-1.png" || out.Images[1].Path != "site/hero-2.png" {
		t.Fatalf("answer %+v", out)
	}
	if _, err := os.Stat(filepath.Join(w.Workspace, "site/hero-2.png")); err != nil {
		t.Fatal(err)
	}
	asked := dummy.Painted()
	last := asked[len(asked)-1]
	if last.Aspect != "16:9" || last.Count != 2 || len(last.Inputs) != 1 || !bytes.Equal(last.Inputs[0].Data, saved) || last.Inputs[0].Mime != "image/png" {
		t.Fatalf("the model was asked %+v", last)
	}

	// What cannot work says why, and writes nothing.
	for name, args := range map[string]string{
		"a model not allowed":   `{"prompt":"x","model":"dummy/echo"}`,
		"no prompt":             `{"prompt":" "}`,
		"too many":              `{"prompt":"x","count":9}`,
		"an unknown aspect":     `{"prompt":"x","aspect":"7:5"}`,
		"a path out of the box": `{"prompt":"x","path":"../x.png"}`,
		"an input that is text": `{"prompt":"x","images":["notes.txt"]}`,
		"the model's refusal":   `{"prompt":"Test_Image_Error"}`,
	} {
		if name == "an input that is text" {
			if err := os.WriteFile(filepath.Join(w.Workspace, "notes.txt"), []byte("hello"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if res := call(h, bot.GetId(), args); res.GetError() == "" {
			t.Errorf("%s: %s", name, res.GetResultJson())
		}
	}

	// The prompt and list_models say what there is.
	prompt := func(h *apptest.H, botID string) string {
		t.Helper()
		run, _ := h.Send(botID, h.FirstChat(botID), "Test_Image_Prompt")
		h.WaitRun(run)
		sent := dummy.Streamed()
		var b strings.Builder
		for _, blk := range sent[len(sent)-1].System {
			b.WriteString(blk.Text)
		}
		return b.String()
	}
	if p := prompt(h, bot.GetId()); !strings.Contains(p, "## Images") || !strings.Contains(p, "sr.generate_image(") || !strings.Contains(p, "`dummy/paint`") || !strings.Contains(p, "dummy/sketch") {
		t.Fatal("the prompt does not tell the Bot about generate_image")
	}
	res, err := h.WorkerClient(bot.GetId()).CallTool(h.Ctx(), connect.NewRequest(&v1.ToolReq{Connector: "model", Action: "list", ArgsJson: "{}"}))
	if err != nil || !strings.Contains(res.Msg.GetResultJson(), `"default_image":"dummy/paint"`) || !strings.Contains(res.Msg.GetResultJson(), `"dummy/sketch"`) {
		t.Fatalf("list_models: %v %s", err, res.Msg.GetResultJson())
	}

	// The owner can take the tool away with a rule.
	if _, err := h.Client.SetRule(h.Ctx(), connect.NewRequest(&v1.SetRuleRequest{BotId: bot.GetId(), Connector: "model", Action: "image", Decision: "deny"})); err != nil {
		t.Fatal(err)
	}
	if res := call(h, bot.GetId(), `{"prompt":"x"}`); res.GetError() != "denied" {
		t.Fatalf("denied by rule: %+v", res)
	}

	// No image model picked: no tool, and no word of it in the prompt.
	dummy.Reset()
	plain := apptest.New(t)
	bot = plain.CreateBot("Plain")
	if res := call(plain, bot.GetId(), `{"prompt":"x"}`); res.GetError() == "" {
		t.Fatalf("generate_image with no image model: %s", res.GetResultJson())
	}
	if p := prompt(plain, bot.GetId()); strings.Contains(p, "## Images") || strings.Contains(p, "generate_image") {
		t.Fatal("the prompt offers generate_image with no image model")
	}
}

// Image models are a kind of their own: a list, a role that picks from it, and
// providers that serve it.
func TestImageModelsAreAKind(t *testing.T) {
	h := voiceHarness(t)
	set := func(models ...string) error {
		_, err := h.Client.SetModels(h.Ctx(), connect.NewRequest(&v1.SetModelsRequest{Kind: llm.KindImage, Models: models}))
		return err
	}
	if err := set("dummy/paint", "fal/fal-ai/flux-2-pro", "bfl/flux-3-image", "openrouter/google/gemini-3-pro-image"); err != nil {
		t.Fatal(err)
	}
	if err := set("anthropic/claude-sonnet-5"); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("a provider with no image models: %v", err)
	}
	st, err := h.Client.GetSettings(h.Ctx(), connect.NewRequest(&v1.GetSettingsRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	var listed []string
	for _, k := range st.Msg.GetAllowed() {
		if k.GetKind() == llm.KindImage {
			listed = k.GetListed()
		}
	}
	if len(listed) != 4 {
		t.Fatalf("image models listed: %v", listed)
	}
	for _, p := range st.Msg.GetProviders() {
		if p.GetId() == llm.Fal && strings.Join(p.GetKinds(), ",") != llm.KindImage {
			t.Fatalf("fal serves %v", p.GetKinds())
		}
	}
}
