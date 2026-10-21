import { completion, matchCommands, needsArgs, slashQuery, type SlashCommand } from "./slash.ts";

const cmd = (name: string, args = ""): SlashCommand => ({ name, args, summary: "", confirm: false });
const cmds = [cmd("clear"), cmd("compact"), cmd("model", "[model]"), cmd("rename", "<title>")];

function eq(got: unknown, want: unknown, what: string) {
  if (JSON.stringify(got) !== JSON.stringify(want)) throw new Error(`${what}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`);
}

eq(slashQuery("/"), "", "bare slash");
eq(slashQuery("/Cl"), "cl", "case");
eq(slashQuery("/clear now"), null, "with args");
eq(slashQuery("hi /c"), null, "mid text");
eq(slashQuery("/usr/bin"), null, "path");
eq(
  matchCommands(cmds, "c").map((c) => c.name),
  ["clear", "compact"],
  "prefix",
);
eq(matchCommands(cmds, "x"), [], "none");
eq(needsArgs(cmds[3]), true, "required arg");
eq(needsArgs(cmds[2]), false, "optional arg");
eq(completion(cmds[0]), "/clear", "no args");
eq(completion(cmds[2]), "/model ", "args");
console.log("slash ok");
