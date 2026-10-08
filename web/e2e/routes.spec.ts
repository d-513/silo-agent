import { expect, test, type Page } from "@playwright/test";

// Every documented URL keeps working: each one loads, stays where it is, and
// lights the right places. No message is sent, so this costs no tokens.

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

// Whatever is lit carries data-tab-on: a sidebar row and a section tab by
// their label, a machine rail button by its title.
const lit = (page: Page, name: string) => page.locator(`[data-tab-on][title="${name}"], [data-tab-on]:has-text("${name}")`).first();

const pages: [path: string, lit: string[]][] = [
  // A machine pane as a page of its own lights its rail button.
  ["desktop", ["Desktop"]],
  ["console", ["Console"]],
  ["files", ["Files"]],
  // Customize and Settings light their sidebar row and the open tab.
  ["connectors", ["Customize", "Connectors"]],
  ["skills", ["Customize", "Skills"]],
  ["drives", ["Customize", "Drives"]],
  ["channels", ["Customize", "Channels"]],
  ["settings", ["Settings", "General"]],
  ["rules", ["Settings", "Rules"]],
  ["secrets", ["Settings", "Secrets"]],
  ["tunnels", ["Settings", "Tunnels"]],
  ["container", ["Settings", "Containers"]],
  // The pages beside the chat light their own row.
  ["automations", ["Automations"]],
  ["memories", ["Memories"]],
  ["knowledge", ["Knowledge"]],
  ["feed", ["Feed"]],
];

test("every route loads, stays put, and lights its place", async ({ page }) => {
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
  await expect(page.getByPlaceholder(/Ask /)).toBeVisible();

  try {
    for (const [path, names] of pages) {
      await page.goto(`${bot}/${path}`);
      for (const name of names) await expect(lit(page, name), `${path}: ${name}`).toBeVisible();
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
      ["automations", "automations/new", "Automations"],
    ]) {
      await page.goto(`${bot}/${list}`);
      await expect(lit(page, label)).toBeVisible();
      await page.goto(`${bot}/${sub}`);
      await expect(page, sub).toHaveURL(new RegExp(`${bot}/${sub}$`));
      await expect(lit(page, label), sub).toBeVisible();
      await page.goBack();
      await expect(page, sub).toHaveURL(new RegExp(`${bot}/${list}$`));
    }
    await page.goto(`${bot}/channels/new/telegram`);
    await expect(page.getByText(/telegram/i).first()).toBeVisible();
    await expect(page).toHaveURL(new RegExp(`${bot}/channels/new/telegram$`));
    // Each sub-page is its own route with its own content.
    await page.goto(`${bot}/automations/new`);
    await expect(page.getByRole("heading", { name: "New automation" })).toBeVisible();
    await page.goto(`${bot}/automations/nope`);
    await expect(page.getByText("This automation is gone.")).toBeVisible();
    await expect(lit(page, "Automations")).toBeVisible();
    await page.goto(`${bot}/drives/new/sftp`);
    await expect(page.getByText(/sftp/i).first()).toBeVisible();
    await expect(lit(page, "Drives")).toBeVisible();
    await page.goto(`${bot}/drives/nope`);
    await expect(page.getByText("That drive is gone.")).toBeVisible();
    await page.goto(`${bot}/channels/nope/setup`);
    await expect(page.getByText("Opening…")).toBeVisible();
    await expect(lit(page, "Channels")).toBeVisible();
    // The pinned Heartbeat opens its log, and Back returns to the list.
    await page.goto(`${bot}/automations`);
    await page.getByRole("link", { name: /heartbeat/i }).first().click();
    await expect(page).toHaveURL(new RegExp(`${bot}/automations/[^/]+$`));
    await expect(page.getByText(/No runs yet/)).toBeVisible();
    await page.goBack();
    await expect(page.getByRole("heading", { name: "Automations" })).toBeVisible();

    await page.goto("/admin/connectors/new");
    await expect(page).toHaveURL(/\/admin\/connectors\/new$/);
    await page.goto("/skills");
    await expect(page).toHaveURL(/\/skills$/);
    await page.goto("/account");
    await expect(page).toHaveURL(/\/account$/);

    // A query string survives.
    await page.goto(`${bot}/files?open=bot`);
    await expect(lit(page, "Files")).toBeVisible();
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

    // Settings, one of its tabs, and back: the chats stay in the sidebar.
    await page.getByRole("link", { name: "Settings", exact: true }).click();
    await expect(page).toHaveURL(new RegExp(`${bot}/settings$`));
    await page.getByRole("link", { name: "Rules" }).click();
    await expect(page).toHaveURL(new RegExp(`${bot}/rules$`));
    await expect(lit(page, "Rules")).toBeVisible();
    await page.getByRole("link", { name: "New chat" }).first().click();
    await expect(page).toHaveURL(second);

    // A machine pane docks beside the chat without leaving it, and opens as
    // a page of its own.
    await page.getByRole("button", { name: "Files", exact: true }).click();
    await expect(page).toHaveURL(second);
    await expect(lit(page, "Files")).toBeVisible();
    await page.getByRole("button", { name: "Open as a page" }).click();
    await expect(page).toHaveURL(new RegExp(`${bot}/files$`));
    await page.getByRole("button", { name: "Dock beside the chat" }).click();
    await expect(page).toHaveURL(second);
    await page.getByRole("button", { name: "Close" }).click();
    await expect(lit(page, "Files")).toHaveCount(0);

    // A page beside the chat lights its own row; a chat in the list opens it.
    await page.getByRole("link", { name: "Memories" }).first().click();
    await expect(page).toHaveURL(new RegExp(`${bot}/memories$`));
    await expect(lit(page, "Memories")).toBeVisible();
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

// The admin form edits on top of what the server holds: typing counts a change,
// it is still there after a look at another category, and Discard goes back to
// the saved value. Nothing is saved here.
test("admin settings form tracks and discards edits", async ({ page }) => {
  await page.goto("/admin/settings");
  await signIn(page);
  // Settings opens on its first category.
  await expect(page).toHaveURL(/\/admin\/settings\/models$/);
  const categories = page.getByRole("navigation", { name: "Settings" });
  await categories.getByRole("link", { name: "Server" }).click();
  await expect(page).toHaveURL(/\/admin\/settings\/server$/);
  const field = page.locator('main input[type="text"]:not([disabled])').first();
  await expect(field).toBeVisible();
  const saved = await field.inputValue();
  await field.fill(`${saved}x`);
  await expect(page.getByText("1 unsaved change")).toBeVisible();
  await categories.getByRole("link", { name: "Providers" }).click();
  await expect(page.getByRole("button", { name: "List models" }).first()).toBeVisible();
  await expect(page.getByText("1 unsaved change")).toBeVisible();
  await categories.getByRole("link", { name: "Server" }).click();
  await expect(field).toHaveValue(`${saved}x`);
  await page.getByRole("button", { name: /discard/i }).click();
  await expect(field).toHaveValue(saved);
  await expect(page.getByText("1 unsaved change")).toHaveCount(0);
  // Every category is a page of its own, and an unknown one falls back to the first.
  for (const s of ["search", "memory", "runs", "connectors", "tunnels", "yaml", "audit"]) {
    await page.goto(`/admin/settings/${s}`);
    await expect(page).toHaveURL(new RegExp(`/admin/settings/${s}$`));
    await expect(categories.locator("[data-tab-on]")).toHaveCount(1);
    await expect(page.getByText("This page hit a snag")).toHaveCount(0);
  }
  await page.goto("/admin/settings/nope");
  await expect(page).toHaveURL(/\/admin\/settings\/models$/);
  // Search & Extract was a tab; its old address lands on the category.
  await page.goto("/admin/search-extract");
  await expect(page).toHaveURL(/\/admin\/settings\/search$/);
  // The other admin pages load their lists.
  for (const p of ["connectors", "skills", "drives"]) {
    await page.goto(`/admin/${p}`);
    await expect(page).toHaveURL(new RegExp(`/admin/${p}$`));
    await expect(page.locator(".skeleton")).toHaveCount(0);
    await expect(page.getByText("This page hit a snag")).toHaveCount(0);
  }
});
