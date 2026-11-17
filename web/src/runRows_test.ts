import { callWord, kindsOf, kindWord, spent, took, tokens, triggerWord } from "./runRows.ts";
import { eq } from "./testing.ts";

eq([took(0), took(420), took(12_300), took(185_000), took(3_840_000)], ["", "0.4 s", "12 s", "3 min 05 s", "1 h 04 min"], "how long");
eq([tokens(0), tokens(950), tokens(12_340), tokens(250_000), tokens(1_240_000)], ["0", "950", "12.3k", "250k", "1.2M"], "token counts");

// A row is named by its kind; a job by what it keeps.
eq([kindWord("chat", ""), kindWord("job", "memory"), kindWord("job", "other"), kindWord("wake", ""), kindWord("new_kind", "")], ["Chat", "Memory keeper", "Housekeeping", "Subagent report", "new_kind"], "kind words");

// A typed message is what a chat run is; only the unusual start is named.
eq([triggerWord("message"), triggerWord("spoken"), triggerWord("mail"), triggerWord("")], ["", "spoken", "woken by a mail", ""], "triggers");

eq([callWord("approval"), callWord("voice-talk"), callWord("something")], ["Auto-approval", "Call talker", "something"], "call labels");

// A run's cost counts the calls made beside its turns.
eq(spent({ input: 1000n, output: 200n, sideInput: 50n, sideOutput: 5n }), { read: 1050, wrote: 205 }, "spent");
eq(spent(undefined), { read: 0, wrote: 0 }, "a live run has no summary yet");

// Chips show for the kinds listed, in their own order.
eq(kindsOf([{ kind: "job" }, { kind: "chat" }, { kind: "chat" }]), ["chat", "job"], "kinds present");

console.log("ok");
