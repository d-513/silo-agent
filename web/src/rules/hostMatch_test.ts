import { covers, listedAs } from "./hostMatch.ts";
import { eq } from "../testing.ts";

eq(covers("example.com", "example.com"), true, "the host itself");
eq(covers("example.com", "api.example.com"), true, "a name below");
eq(covers("example.com", "badexample.com"), false, "not a suffix of a label");
eq(covers("*.example.com", "example.com"), false, "a wildcard skips the host");
eq(covers("*.example.com", "a.b.example.com"), true, "a wildcard covers below");
eq(covers("10.0.0.0/8", "10.2.3.4"), true, "a CIDR");
eq(covers("10.0.0.0/8", "11.2.3.4"), false, "outside a CIDR");
eq(covers("1.1", "1.1.1.1"), false, "an address is matched whole");
eq(covers("Example.COM.", "example.com:443"), true, "case, dot and port");

const list = [
  { kind: "block", pattern: "example.com", fromOperator: true },
  { kind: "block", pattern: "api.example.com", fromOperator: false },
  { kind: "trust", pattern: "*.cdn.net", fromOperator: false },
];
eq(listedAs(list, "block", "api.example.com")?.pattern, "api.example.com", "the Bot's own exact entry first");
eq(listedAs(list, "block", "www.example.com")?.fromOperator, true, "else what covers it");
eq(listedAs(list, "trust", "x.cdn.net")?.pattern, "*.cdn.net", "trust");
eq(listedAs(list, "trust", "example.com"), undefined, "not listed");
