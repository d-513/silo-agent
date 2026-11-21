import { eq } from "../testing.ts";
import { asDesktopAction, connectorTool, toolAction, toolMeta } from "./toolInfo.ts";

// A connector's tool offered to the model directly is named <slug>__<action>;
// the row reads as the connector and what it did, not the raw name.
eq(connectorTool("github__create_issue"), { slug: "github", action: "create_issue" }, "slug and action");
eq(connectorTool("fal_ai__flux__dev"), { slug: "fal_ai", action: "flux__dev" }, "only the first __ splits");
eq([connectorTool("task_add"), connectorTool("__x"), connectorTool("x__")], [undefined, undefined, undefined], "built-in names are not connector tools");
eq(toolMeta("github__create_issue").app, "github", "the app is the connector");
eq(toolAction("github__create_issue", "{}"), "create issue", "the action reads as words");
eq([toolMeta("web_search").app, toolAction("web_search", '{"query":"go"}')], ["Web search", "go"], "built-ins are unchanged");
eq(
  [toolMeta("web_read").app, toolAction("web_read", '{"url":"https://go.dev/blog"}'), toolAction("web_read", '{"urls":["https://a.example","https://b.example"]}')],
  ["Web page", "https://go.dev/blog", "https://a.example +1 more"],
  "a read is named by its address",
);
eq(
  [toolAction("web_search", '{"queries":["go","rust","zig"]}'), toolAction("web_search", '{"queries":["go"]}'), toolAction("web_search", "{}")],
  ["go +2 more", "go", ""],
  "a batch of searches names the first and counts the rest",
);
eq(toolMeta("mystery").app, "mystery", "an unknown name is shown as it is");

// A decision is named by what was asked: the questions' own names.
const asked = (names: string[]) => JSON.stringify({ state: { mail: "hi" }, questions: Object.fromEntries(names.map((n) => [n, { type: "noul", instructions: "?" }])) });
eq(toolMeta("decide").app, "Decision", "decide has its own app name");
eq(toolMeta("generate_image").app, "Image", "generate_image has its own app name");
eq(toolAction("generate_image", '{"prompt":"A red fox in snow\\nlong exposure","aspect":"16:9"}'), "A red fox in snow", "an image is named by its prompt");
eq(toolAction("decide", asked(["is_spam"])), "is_spam", "one question");
eq(toolAction("decide", asked(["is_spam", "team", "urgency"])), "is_spam, team, urgency", "a few questions are all named");
eq(toolAction("decide", asked(["a", "b", "c", "d", "e"])), "a, b, c +2 more", "many are counted");
eq(toolAction("decide", '{"state":"x","questions":"{\\"q\\": {\\"type\\": \\"noul\\"}}"}'), "q", "questions written as a JSON string are read too");
eq(toolAction("decide", '{"state":{"mail":"hi"},"questions":{"is_sp'), "", "nothing to name while the arguments still stream");

// The desktop is one tool to the model; its row reads as the action it took,
// like the calls older runs made by name.
const desk = (args: string) => asDesktopAction({ name: "desktop", args });
eq(desk('{"action":"click","x":10,"y":20}'), { name: "click", args: '{"x":10,"y":20}' }, "a click is a click row");
eq(desk('{"action":"key","text":"ctrl+l"}'), { name: "key", args: '{"name":"ctrl+l"}' }, "a key's shortcut reads as the older call's name");
eq(toolAction(desk('{"action":"key","text":"Return"}').name, desk('{"action":"key","text":"Return"}').args), "key Return", "the key row names the key");
eq(desk('{"action":"type","text":"hel'), { name: "type", args: '{"text":"hel"}' }, "a call still streaming is already its action");
eq([desk('{"action":"chrome"}').name, desk("").name], ["desktop", "desktop"], "an action it does not have stays the desktop");
eq(toolMeta("desktop").app, "Desktop", "a live step on the desktop is the Desktop");
const older = { name: "look", args: "{}" };
eq(asDesktopAction(older) === older, true, "an older call is left as it was");
