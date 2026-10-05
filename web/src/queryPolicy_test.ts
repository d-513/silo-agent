import { Code, ConnectError } from "@connectrpc/connect";
import { botPollMs, putRow, retryable, subagentPollMs } from "./queryPolicy.ts";
import { eq } from "./testing.ts";

// Retry: only a call that never reached a working CP is worth repeating.
eq(retryable(0, new ConnectError("", Code.Unavailable)), true, "unavailable retries");
eq(retryable(1, new TypeError("Failed to fetch")), true, "network failure retries");
eq(retryable(2, new ConnectError("", Code.Unavailable)), false, "gives up after two retries");
eq(retryable(0, new ConnectError("", Code.Unauthenticated)), false, "signed out never retries");
eq(retryable(0, new ConnectError("", Code.NotFound)), false, "gone never retries");
eq(retryable(0, new ConnectError("bot name taken", Code.AlreadyExists)), false, "a real answer never retries");

// The Bot row is polled fast until the box settles, slowly after.
eq(botPollMs(undefined), 500, "no row yet");
eq(botPollMs({ workerConnected: false, status: "starting" }), 500, "starting");
eq(botPollMs({ workerConnected: true, status: "working" }), 3000, "online");
eq(botPollMs({ workerConnected: false, status: "stopped" }), 3000, "stopped");

// Subagents: the tray moves while one runs; a lead with none still checks in.
eq(subagentPollMs([{ running: true }, { running: false }], true), 2500, "one running");
eq(subagentPollMs([{ running: false }], true), false, "an idle lead waits for pings");
eq(subagentPollMs([], false), 5000, "a subagent's own log polls the board");

// putRow replaces the row with that id, or leaves the list alone.
const a = { id: "a", name: "A" };
const b = { id: "b", name: "B" };
eq(putRow([a, b], { id: "b", name: "B2" }), [a, { id: "b", name: "B2" }], "replaces in place");
eq(putRow([a], { id: "z", name: "Z" }), [a], "an unknown row is not added");
eq(putRow(undefined, a), undefined, "no list yet");

console.log("queryPolicy ok");
