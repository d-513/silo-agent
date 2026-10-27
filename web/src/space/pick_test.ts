import { eq } from "../testing.ts";
import { markOf, pickSpace } from "./pick.ts";

eq(pickSpace(["a", "b"], "b", "a"), "b", "the page's Workspace wins");
eq(pickSpace(["a", "b"], undefined, "b"), "b", "else the last opened");
eq(pickSpace(["a", "b"], undefined, "gone"), "a", "a last one no longer listed falls to the first");
eq(pickSpace(["a", "b"], undefined, null), "a", "nothing remembered is the first");
eq(pickSpace([], undefined, "a"), "", "nothing listed yet");

// The same cases as space.TestMark on the server.
eq(markOf("Acme labs"), "AL", "two words");
eq(markOf("garden"), "GA", "one word");
eq(markOf("Żółw ninja"), "ŻN", "letters beyond ASCII");
eq(markOf("x"), "X", "one letter");
eq(markOf("!!!"), "W", "no letters at all");
eq(markOf("Acme", "q"), "Q", "a given mark");
eq(markOf("Acme", "  -- "), "AC", "a given mark with no letters");
