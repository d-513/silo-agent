import { eq } from "../../testing.ts";
import { clock, editable, formOf, priorityTone, requestOf, secondsLeft, uptime, used, words } from "./siloosView.ts";
import type { NetConnection } from "../../gen/silo/v1/siloos_pb";

eq([uptime(40), uptime(125), uptime(3 * 3600 + 120), uptime(2 * 86400 + 5 * 3600)], ["40 s", "2 min", "3 h 2 min", "2 d 5 h"], "uptime");
eq([used(100, 25), used(0, 0)], [75, 0], "used");
eq(secondsLeft("2026-11-21T10:00:30Z", Date.parse("2026-11-21T10:00:00Z")), 30, "seconds left");
eq(secondsLeft("2026-11-21T10:00:00Z", Date.parse("2026-11-21T10:05:00Z")), 0, "never below zero");
eq(words("1.1.1.1, 8.8.8.8\n9.9.9.9;;"), ["1.1.1.1", "8.8.8.8", "9.9.9.9"], "words");

const conn = (o: Partial<NetConnection>) => ({ uuid: "u", name: "Wired", method: "auto", addresses: [], gateway: "", dns: [], ignoreAutoDns: false, ...o }) as NetConnection;
const f = formOf(conn({ method: "manual", addresses: ["10.0.0.5/24"], gateway: "10.0.0.1", dns: ["1.1.1.1", "9.9.9.9"] }));
eq(f, { method: "manual", addresses: "10.0.0.5/24", gateway: "10.0.0.1", dns: "1.1.1.1, 9.9.9.9", ignoreAutoDns: false }, "the form starts from the connection");
eq(requestOf("u", f), { uuid: "u", method: "manual", addresses: ["10.0.0.5/24"], gateway: "10.0.0.1", dns: ["1.1.1.1", "9.9.9.9"], ignoreAutoDns: false }, "manual");
eq(requestOf("u", { ...f, method: "auto", ignoreAutoDns: true }), { uuid: "u", method: "auto", addresses: [], gateway: "", dns: ["1.1.1.1", "9.9.9.9"], ignoreAutoDns: true }, "DHCP drops the addresses and the gateway");
eq([editable(conn({})), editable(conn({ method: "other:auto" })), editable(undefined)], [true, false, false], "only wired profiles are edited");
eq([priorityTone(2), priorityTone(4), priorityTone(6)], ["text-vermilion", "text-ink", "text-ink-2"], "priority");
const now = new Date(2026, 10, 21, 12, 0, 0);
eq(clock(new Date(2026, 10, 21, 9, 5, 7).toISOString(), now), "09:05:07", "today: the time");
eq(clock(new Date(2026, 10, 20, 9, 5, 7).toISOString(), now), "2026-11-20 09:05:07", "another day: the date too");
console.log("siloosView ok");

import { channelChoices } from "./siloosView.ts";
eq(channelChoices("latest", "latest", "v0.4.2"), { choices: [{ value: "latest", label: "latest" }, { value: "master", label: "master" }, { value: "v0.4.2", label: "v0.4.2" }], current: "latest" }, "on latest, staying on the running release is offered");
eq(channelChoices("pinned", "v0.4.1", "v0.4.1").current, "v0.4.1", "pinned");
eq(channelChoices("master", "master", "master-abc1234").choices.length, 2, "a commit build offers no version to stay on");
eq(channelChoices("local", "dev", "dev").current, "", "a local build follows nothing");
