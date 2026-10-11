import { eq } from "./testing.ts";
import { deviceZone, zoneLabel, zoneNames, zoneOffset } from "./zones.ts";

const summer = new Date("2026-07-01T12:00:00Z");
const winter = new Date("2026-12-01T12:00:00Z");

eq(zoneOffset("Europe/Warsaw", summer), "+02:00", "Warsaw in summer");
eq(zoneOffset("Europe/Warsaw", winter), "+01:00", "Warsaw in winter");
eq(zoneOffset("Asia/Kolkata", summer), "+05:30", "a half-hour zone");
eq(zoneOffset("America/New_York", winter), "-05:00", "west of Greenwich");
eq(zoneOffset("UTC", summer), "+00:00", "UTC");
eq(zoneOffset("Mars/Olympus", summer), "", "an unknown zone has no offset");
eq(zoneOffset("", summer), "", "no zone");

eq(zoneLabel("Asia/Tokyo", summer), "Asia/Tokyo · +09:00", "label");
eq(zoneLabel("Mars/Olympus", summer), "Mars/Olympus", "label of an unknown zone");

const names = zoneNames("Mars/Olympus");
for (const want of ["UTC", "Europe/Warsaw", "Asia/Tokyo", "Mars/Olympus"]) {
  if (!names.includes(want)) throw new Error(`zoneNames lacks ${want}`);
}
eq(names, [...names].sort((a, b) => a.localeCompare(b)), "sorted");
eq(new Set(names).size, names.length, "no duplicates");
if (zoneNames().includes("")) throw new Error("zoneNames holds an empty name");

// Whatever this machine is set to, it is a name the picker offers.
const here = deviceZone();
if (here && zoneOffset(here) === "") throw new Error(`the device zone ${here} has no offset`);

console.log("zones ok");
