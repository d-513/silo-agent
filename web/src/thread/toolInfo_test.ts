import { eq } from "../testing.ts";
import { connectorTool, toolAction, toolMeta } from "./toolInfo.ts";

// A connector's tool offered to the model directly is named <slug>__<action>;
// the row reads as the connector and what it did, not the raw name.
eq(connectorTool("github__create_issue"), { slug: "github", action: "create_issue" }, "slug and action");
eq(connectorTool("fal_ai__flux__dev"), { slug: "fal_ai", action: "flux__dev" }, "only the first __ splits");
eq([connectorTool("task_add"), connectorTool("__x"), connectorTool("x__")], [undefined, undefined, undefined], "built-in names are not connector tools");
eq(toolMeta("github__create_issue").app, "github", "the app is the connector");
eq(toolAction("github__create_issue", "{}"), "create issue", "the action reads as words");
eq([toolMeta("web_search").app, toolAction("web_search", '{"query":"go"}')], ["Web search", "go"], "built-ins are unchanged");
eq(
  [toolAction("web_search", '{"queries":["go","rust","zig"]}'), toolAction("web_search", '{"queries":["go"]}'), toolAction("web_search", "{}")],
  ["go +2 more", "go", ""],
  "a batch of searches names the first and counts the rest",
);
eq(toolMeta("mystery").app, "mystery", "an unknown name is shown as it is");

// A decision is named by what was asked: the questions' own names.
const asked = (names: string[]) => JSON.stringify({ state: { mail: "hi" }, questions: Object.fromEntries(names.map((n) => [n, { type: "noul", instructions: "?" }])) });
eq(toolMeta("decide").app, "Decision", "decide has its own app name");
eq(toolAction("decide", asked(["is_spam"])), "is_spam", "one question");
eq(toolAction("decide", asked(["is_spam", "team", "urgency"])), "is_spam, team, urgency", "a few questions are all named");
eq(toolAction("decide", asked(["a", "b", "c", "d", "e"])), "a, b, c +2 more", "many are counted");
eq(toolAction("decide", '{"state":"x","questions":"{\\"q\\": {\\"type\\": \\"noul\\"}}"}'), "q", "questions written as a JSON string are read too");
eq(toolAction("decide", '{"state":{"mail":"hi"},"questions":{"is_sp'), "", "nothing to name while the arguments still stream");
