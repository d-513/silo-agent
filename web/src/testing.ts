// The one assertion the web unit tests (`*_test.ts`, run with node) share.
export function eq(got: unknown, want: unknown, what: string) {
  if (JSON.stringify(got) !== JSON.stringify(want)) throw new Error(`${what}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`);
}
