import { defineConfig, devices } from "@playwright/test";

// Integration tests run against the developer's already-running stack
// (`make dev`): Vite on :5173 proxying to the control plane on :8080. The
// harness never starts or stops those servers — see AGENTS.md. Point it
// elsewhere with SILO_E2E_URL.
export default defineConfig({
  testDir: "./e2e",
  timeout: 5 * 60_000,
  expect: { timeout: 20_000 },
  fullyParallel: false,
  workers: 1,
  retries: 0,
  reporter: [["list"]],
  use: {
    baseURL: process.env.SILO_E2E_URL ?? "http://localhost:5173",
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
    video: "off",
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
});
