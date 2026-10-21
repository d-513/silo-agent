// The composer's slash commands: what the menu offers while a command's name
// is being typed. The server runs them (Send); this is only the menu.

export type SlashCommand = { name: string; args: string; summary: string; confirm: boolean };

// slashQuery is the command name being typed ("cl" for "/cl"), or null when
// the text is not a bare "/name" being typed.
export function slashQuery(text: string): string | null {
  const m = /^\/([a-z]*)$/i.exec(text);
  return m ? m[1].toLowerCase() : null;
}

// matchCommands is the commands whose name starts with q, in order.
export function matchCommands(cmds: SlashCommand[], q: string): SlashCommand[] {
  return cmds.filter((c) => c.name.startsWith(q));
}

// needsArgs: a command whose argument is required ("<title>") is completed
// with a space to type it; one with none or an optional one ("[model]") runs.
export function needsArgs(c: SlashCommand): boolean {
  return c.args.startsWith("<");
}

// completion is the text a picked command leaves in the box.
export function completion(c: SlashCommand): string {
  return c.args ? `/${c.name} ` : `/${c.name}`;
}
