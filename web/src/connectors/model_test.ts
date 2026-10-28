import { eq } from "../testing.ts";
import { categoriesOf, categoryOf, filterCatalog, groupByCategory, isCustom, nextCopyName } from "./model.ts";

const lib = (name: string, category: string, extra: object = {}) => ({ id: name.toLowerCase(), name, description: `${name} tools`, category, transport: "http", kind: "library", sourceId: "", ...extra }) as any;
const catalog = [lib("GitHub", "Developer"), lib("Linear", "Developer"), lib("Notion", ""), lib("Slack", "Communication", { transport: "stdio" })];

// Copies of one preset get the next free number, ignoring case and spacing.
eq(nextCopyName("GitHub", []), "GitHub", "free name");
eq(nextCopyName("GitHub", ["github "]), "GitHub 2", "taken, case and space insensitive");
eq(nextCopyName("GitHub", ["GitHub", "GitHub 2", "github 3"]), "GitHub 4", "skips used numbers");

eq(isCustom(lib("Mine", "", { kind: "custom" })), true, "custom connector");
eq(isCustom(lib("Mine", "", { kind: "custom", sourceId: "x" })), false, "copy of a preset is not custom");

eq(categoryOf(lib("A", "  ")), "General", "blank category");
eq(categoriesOf(catalog), ["Communication", "Developer", "General"], "sorted, blank is General");

eq(filterCatalog(catalog, "", "all").length, 4, "no filter");
eq(filterCatalog(catalog, "", "developer").map((c) => c.name), ["GitHub", "Linear"], "category is case-insensitive");
eq(filterCatalog(catalog, "STDIO", "all").map((c) => c.name), ["Slack"], "search matches transport");
eq(filterCatalog(catalog, "noti", "General").map((c) => c.name), ["Notion"], "search within a category");
eq(filterCatalog(catalog, "zzz", "all"), [], "no match");

eq([...groupByCategory(catalog).entries()].map(([k, v]) => [k, v.length]), [["Developer", 2], ["General", 1], ["Communication", 1]], "groups keep first-seen order");

