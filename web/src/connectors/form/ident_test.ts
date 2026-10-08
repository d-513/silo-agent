import { eq } from "../../testing.ts";
import { identOf, identTyped } from "./ident.ts";

eq(identOf("fal.ai"), "fal_ai", "a dot is a separator");
eq(identOf("  Google  Drive "), "google_drive", "spaces collapse and the ends are trimmed");
eq(identOf("Monday.com, Inc."), "monday_com_inc", "commas and dots");
eq(identOf("__Git--Hub__"), "git_hub", "underscores and dashes are separators too");
eq(identOf("Zażółć 9"), "za_9", "only ASCII letters and digits are kept");
eq(identOf("..."), "", "nothing to keep");

// While typing, the separator just typed has to survive until the next letter.
eq(identTyped("Fal."), "fal_", "a trailing separator stays");
eq(identTyped("fal_ "), "fal_", "and does not double");
eq(identTyped(" .fal"), "fal", "a leading one never does");
eq(identOf(identTyped("Fal.") + "ai"), "fal_ai", "the next word follows it");
