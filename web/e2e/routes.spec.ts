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

// No page may throw, or log a React error, while it is walked.
let thrown: string[] = [];
test.beforeEach(({ page }) => {
  thrown = [];
  page.on("pageerror", (e) => thrown.push(e.message));
  page.on("console", (m) => {
    if (m.type() === "error" && /react|router|query|hook|render/i.test(m.text())) thrown.push(m.text());
  });
});
test.afterEach(() => expect(thrown).toEqual([]));

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
      await expect(page.getByText("This page hit a snag"), path).toHaveCount(0);
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

// Signing in returns to the page the session ended on (the /signin route's
// `from`), not the Bots list.
test("signing in comes back to the page you were on", async ({ page }) => {
  await page.goto("/skills");
  await signIn(page);
  await expect(page).toHaveURL(/\/skills$/);
  await page.getByTitle("Sign out").click();
  await expect(page).toHaveURL(/\/signin/);
  await signIn(page);
  await expect(page).toHaveURL(/\/skills$/);
});

// A private tunnel sends the browser to /signin?next=<its handoff>: after
// signing in the SPA leaves for that server route instead of opening a page.
test("sign-in carries on to a tunnel handoff", async ({ page }) => {
  await page.goto(`/signin?next=${encodeURIComponent("/tunnels/auth?name=nope&rd=/")}`);
  const handoff = page.waitForRequest((r) => new URL(r.url()).pathname === "/tunnels/auth");
  await signIn(page);
  expect(new URL((await handoff).url()).searchParams.get("name")).toBe("nope");
});

// Moving around by clicking: tabs, the chats list, and back to the open chat.
test("tabs and chats navigate by click", async ({ page }) => {
  await page.goto("/new");
  await signIn(page);
  await expect(page).toHaveURL(/\/new$/);
  await page.fill("#bot-name", `Clicks ${Date.now()}`);
  await page.getByRole("button", { name: /^create bot$/i }).click();
  await expect(page).toHaveURL(/\/run\/[^/]+$/, { timeout: 60_000 });
  const first = page.url();
  const bot = first.match(/\/bots\/[^/]+/)![0];
  try {
    await page.getByRole("button", { name: "New chat" }).first().click();
    await expect(page).not.toHaveURL(first);
    await expect(page).toHaveURL(/\/run\/[^/]+$/);
    const second = page.url();

    // Another tab and back: the Chat tab returns to the chat that was open.
    await page.getByRole("link", { name: "Rules" }).click();
    await expect(page).toHaveURL(new RegExp(`${bot}/rules$`));
    await expect(litTab(page)).toContainText("Rules");
    await page.getByRole("link", { name: "Chat", exact: true }).click();
    await expect(page).toHaveURL(new RegExp(`${bot}/run/[^/]+$`));

    // A side page keeps Chat lit; a chat in the list opens it.
    await page.getByRole("link", { name: "Memories" }).first().click();
    await expect(page).toHaveURL(new RegExp(`${bot}/memories$`));
    await expect(litTab(page)).toContainText("Chat");
    await page.goto(second);
    await expect(page.getByPlaceholder(/Ask /)).toBeVisible();

    // A list that is written to shows the change without a reload.
    await page.goto(`${bot}/secrets`);
    await expect(page.getByText("No secrets on this Bot yet.")).toBeVisible();
    await page.getByPlaceholder("vendor_password").fill("e2e_secret");
    await page.locator('input[type="password"]').fill("hunter2");
    await page.getByRole("button", { name: /^add$/i }).click();
    await expect(page.getByText("e2e_secret")).toBeVisible();
    await page.getByRole("button", { name: /^delete$/i }).click();
    await page.getByRole("button", { name: /again/i }).click();
    await expect(page.getByText("No secrets on this Bot yet.")).toBeVisible();

    // Renaming the Bot reaches the header and the rail at once.
    await page.goto(`${bot}/settings`);
    const renamed = `Renamed ${Date.now()}`;
    await page.locator("input").first().fill(renamed);
    await page.getByRole("button", { name: /^save/i }).first().click();
    await expect(page.getByRole("heading", { name: renamed })).toBeAttached();
    await expect(page.getByTitle(renamed)).toBeVisible();
    await page.goto(second);

    // The rail goes home and back to the Bot.
    await page.getByTitle("Bots", { exact: true }).click();
    await expect(page).toHaveURL(/\/$/);
    await page.goBack();
    await expect(page).toHaveURL(second);
  } finally {
    await page.goto(`${bot}/settings`);
    await page.getByRole("button", { name: /^delete$/i }).click();
    await page.getByRole("button", { name: /^click again to delete$/i }).click();
    await expect(page).toHaveURL(/\/$/, { timeout: 30_000 });
  }
});
