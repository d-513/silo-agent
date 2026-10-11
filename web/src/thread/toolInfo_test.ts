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
eq(toolMeta("mystery").app, "mystery", "an unknown name is shown as it is");
