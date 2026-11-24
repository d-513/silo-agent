import { createMemoryHistory } from "@tanstack/react-router";

// The demo keeps its address in memory: it sits in a frame on the website, and
// its page changes must not fill the visitor's Back button. `?at=/bots/…`
// opens it on a page (the docs embed it that way).
const at = new URLSearchParams(location.search).get("at");

export const history = createMemoryHistory({ initialEntries: [at?.startsWith("/") ? at : "/"] });
