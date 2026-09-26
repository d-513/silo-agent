import { describe, parse, toCron } from "./schedule.ts";

function eq(got: unknown, want: unknown, what: string) {
  if (JSON.stringify(got) !== JSON.stringify(want)) throw new Error(`${what}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`);
}

// Every friendly shape round-trips through cron unchanged.
for (const cron of ["", "*/30 * * * *", "15 * * * *", "0 */6 * * *", "0 9 * * *", "30 8 * * 1-5", "0 10 * * 0,6", "0 7 * * 1,3", "0 9 1 * *"]) {
  eq(toCron(parse(cron)), cron, `round-trip ${cron}`);
}

eq(parse("0 9 * * 1-5").mode, "weekly", "weekdays mode");
eq(parse("0 9 * * 0-6").mode, "daily", "all days is daily");
eq(parse("*/7 * * * *").mode, "custom", "odd step is custom");
eq(parse("@daily").mode, "custom", "descriptor is custom");
eq(parse("0 9 * 1 *").mode, "custom", "month field is custom");

eq(describe(""), "No schedule", "none");
eq(describe("*/30 * * * *"), "Every 30 minutes", "minutes");
eq(describe("0 * * * *"), "Every hour at :00", "hourly");
eq(describe("30 8 * * 1-5"), "Weekdays at 08:30", "weekdays");
eq(describe("0 7 * * 1,3"), "Mon, Wed at 07:00", "days");
eq(describe("0 9 2 * *"), "Monthly on the 2nd at 09:00", "monthly");
eq(describe("0 9 * 1 *"), "Custom: 0 9 * 1 *", "custom");

console.log("schedule ok");
