import { eq } from "../testing.ts";
import { filterSkills, sourceLabel } from "./model.ts";

const sk = (name: string, description = "", source = "") => ({ name, description, source }) as any;
const rows = [
  sk("pdf", "Read and make PDF files", "catalog"),
  sk("deploy", "Ship the site", "https://github.com/acme/skills/tree/main/deploy"),
  sk("Notes", "Quick capture"),
];

eq(filterSkills(rows, "").length, 3, "no search keeps all");
eq(filterSkills(rows, "   ").length, 3, "blank search keeps all");
eq(filterSkills(rows, "PDF").map((r) => r.name), ["pdf"], "name, case-insensitive");
eq(filterSkills(rows, "ship").map((r) => r.name), ["deploy"], "description");
eq(filterSkills(rows, "acme").map((r) => r.name), ["deploy"], "source");
eq(filterSkills(rows, "zzz"), [], "no match");

eq(sourceLabel("https://github.com/acme/skills/tree/main/deploy"), "acme/skills", "github url shows owner/repo");
eq(sourceLabel("github.com/acme/skills"), "acme/skills", "bare github host");
eq(sourceLabel("https://example.com/x/skill.zip"), "example.com/x/skill.zip", "other hosts lose the scheme only");
eq(sourceLabel("upload: mine.zip"), "upload: mine.zip", "non-urls pass through");
eq(sourceLabel(""), "", "empty");
