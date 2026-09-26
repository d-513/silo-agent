import { expect, test } from "@playwright/test";

// The canonical full-integration path: sign in with the developer's bootstrap
// credentials, create a Bot, send one message, and see a real model reply.
// No mocks and no DummyLLM — this costs tokens and needs a running stack.

const email = process.env.SILO_BOOTSTRAP_EMAIL ?? "admin@local";
const password = process.env.SILO_BOOTSTRAP_PASSWORD ?? "silo-dev-pass";

test("create bot, send a message, see the reply", async ({ page }) => {
  await page.goto("/");

  // Sign in.
  await page.fill("#silo-email", email);
  await page.fill('input[type="password"]', password);
  await page.getByRole("button", { name: /^sign in$/i }).click();

  // Create a Bot.
  await page.getByRole("link", { name: "New Bot" }).first().click();
  const name = `E2E ${Date.now()}`;
  await page.fill("#bot-name", name);
  await page.getByRole("button", { name: /^create bot$/i }).click();

  // The Bot page redirects to its first chat; the composer becomes enabled.
  const composer = page.getByPlaceholder(/Ask /);
  await expect(composer).toBeEnabled({ timeout: 60_000 });

  await composer.fill("Reply with the single word PONG and nothing else.");
  await composer.press("Enter");

  // An assistant block (rendered markdown) must show the reply.
  await expect(page.locator(".silo-md", { hasText: "PONG" })).toBeVisible({ timeout: 3 * 60_000 });

  // Clean up: Settings → Delete (armed: first click arms, second deletes).
  const botId = page.url().match(/\/bots\/([^/]+)/)?.[1];
  if (botId) {
    await page.goto(`/bots/${botId}/settings`);
    await page.getByRole("button", { name: /^delete$/i }).click();
    await page.getByRole("button", { name: /^click again to delete$/i }).click();
    await expect(page).toHaveURL(/\/$/, { timeout: 30_000 });
  }
});
