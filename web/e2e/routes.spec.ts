import { expect, test, type Page } from "@playwright/test";

// Every documented URL keeps working: each one loads, stays where it is, and
// lights the right tab. No message is sent, so this costs no tokens.

const email = process.env.SILO_BOOTSTRAP_EMAIL ?? "admin@local";
const password = process.env.SILO_BOOTSTRAP_PASSWORD ?? "silo-dev-pass";

async function signIn(page: Page) {
  await page.fill("#silo-email", email);
  await page.fill('input[type="password"]', password);
  await page.getByRole("button", { name: /^sign in$/i }).click();
}

// The tab strip marks its active tab with data-tab-on.
const litTab = (page: Page) => page.locator("[data-tab-on]").first();

const tabs: [path: string, label: string][] = [
  ["desktop", "Desktop"],
  ["console", "Console"],
  ["files", "Files"],
  ["drives", "Drives"],
  ["connectors", "Connectors"],
  ["channels", "Channels"],
  ["tunnels", "Tunnels"],
  ["skills", "Skills"],
  ["secrets", "Secrets"],
  ["rules", "Rules"],
  ["container", "Containers"],
  ["settings", "Settings"],
  // The conversation-side pages keep the Chat tab lit.
  ["automations", "Chat"],
  ["memories", "Chat"],
  ["knowledge", "Chat"],
  ["feed", "Chat"],
];

test("every route loads, stays put, and lights its tab", async ({ page }) => {
  await page.goto("/");
  await signIn(page);

  await page.getByRole("link", { name: "New Bot" }).first().click();
  await expect(page).toHaveURL(/\/new$/);
  await page.fill("#bot-name", `Routes ${Date.now()}`);
  await page.getByRole("button", { name: /^create bot$/i }).click();

  // A new Bot opens on its first chat.
  await expect(page).toHaveURL(/\/bots\/[^/]+\/run\/[^/]+$/, { timeout: 60_000 });
  const botId = page.url().match(/\/bots\/([^/]+)/)![1];
  const bot = `/bots/${botId}`;
  const chatUrl = page.url();
  await expect(litTab(page)).toContainText("Chat");

  try {
    for (const [path, label] of tabs) {
      await page.goto(`${bot}/${path}`);
      await expect(litTab(page), path).toContainText(label);
      await expect(page, path).toHaveURL(new RegExp(`${bot}/${path}$`));
    }

    // Redirects.
    await page.goto(bot);
    await expect(page).toHaveURL(new RegExp(`${bot}/run`));
    await page.goto(`${bot}/nonsense`);
    await expect(page).toHaveURL(new RegExp(`${bot}/run`));
    await page.goto(`${bot}/run`);
    await expect(page).toHaveURL(chatUrl);
    await page.goto("/whatever");
    await expect(page).toHaveURL(/\/$/);
    await page.goto("/admin");
    await expect(page).toHaveURL(/\/admin\/settings$/);

    // Sub-routes are real history entries: Back returns to the list.
    for (const [list, sub, label] of [
      ["channels", "channels/new", "Channels"],
      ["drives", "drives/new", "Drives"],
      ["automations", "automations/new", "Chat"],
    ]) {
      await page.goto(`${bot}/${list}`);
      await expect(litTab(page)).toContainText(label);
      await page.goto(`${bot}/${sub}`);
      await expect(page, sub).toHaveURL(new RegExp(`${bot}/${sub}$`));
      await expect(litTab(page), sub).toContainText(label);
      await page.goBack();
      await expect(page, sub).toHaveURL(new RegExp(`${bot}/${list}$`));
    }
    await page.goto(`${bot}/channels/new/telegram`);
    await expect(page.getByText(/telegram/i).first()).toBeVisible();
    await expect(page).toHaveURL(new RegExp(`${bot}/channels/new/telegram$`));
    await page.goto("/admin/connectors/new");
    await expect(page).toHaveURL(/\/admin\/connectors\/new$/);
    await page.goto("/skills");
    await expect(page).toHaveURL(/\/skills$/);
    await page.goto("/account");
    await expect(page).toHaveURL(/\/account$/);

    // A query string survives.
    await page.goto(`${bot}/files?open=bot`);
    await expect(litTab(page)).toContainText("Files");
    await expect(page).toHaveURL(new RegExp(`${bot}/files\\?open=bot$`));

  } finally {
    await page.goto(`${bot}/settings`);
    await page.getByRole("button", { name: /^delete$/i }).click();
    await page.getByRole("button", { name: /^click again to delete$/i }).click();
    await expect(page).toHaveURL(/\/$/, { timeout: 30_000 });
  }
});

// Signing in returns to the page the session ended on. The old router lost
// that to its own /signin -> / redirect; fixed with the typed route tree.
test.fixme("signing in comes back to the page you were on", async ({ page }) => {
  await page.goto("/skills");
  await signIn(page);
  await expect(page).toHaveURL(/\/skills$/);
  await page.getByTitle("Sign out").click();
  await expect(page).toHaveURL(/\/signin/);
  await signIn(page);
  await expect(page).toHaveURL(/\/skills$/);
});
