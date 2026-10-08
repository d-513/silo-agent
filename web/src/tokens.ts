// Hex copies of the DESIGN.md palette for renderers that paint to a canvas
// (xterm) or sit outside the page (the browser's theme-color) and cannot
// resolve CSS custom properties. theme_test.ts keeps them in step with
// index.css.
export const palette = {
  hatch: "#141413",
  matte: "#0e0e0d",
  matteDark: "#080808",
  cobalt: "#2b4fc7",
  white: "#ffffff",
  canvas: "#faf9f7",
  canvasDark: "#171615",
} as const;
