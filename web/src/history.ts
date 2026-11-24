import type { RouterHistory } from "@tanstack/react-router";

// Where the router keeps the address: the browser's own (undefined). The demo
// build (vite.demo.config.ts) swaps this module for one that keeps it in
// memory, so the console can sit in a frame on another page.
export const history: RouterHistory | undefined = undefined;
