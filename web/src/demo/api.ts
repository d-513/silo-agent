import { createClient, createRouterTransport } from "@connectrpc/connect";
import { UI } from "../gen/silo/v1/ui_pb";
import { serve } from "./backend";

// The demo's stand-in for ../api.ts (vite.demo.config.ts swaps the two): the
// same client, answered by a backend that runs in this page. Nothing leaves
// the browser.
export const transport = createRouterTransport(({ service }) => service(UI, serve()));

export const ui = createClient(UI, transport);

export function useReachable(): boolean {
  return true;
}

export function onSignedOut(_l: () => void): () => void {
  return () => {};
}
